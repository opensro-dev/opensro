"""
===========================================================================

test_data_release.py - isolated packaging and uploads through the operator CLI

The upload limit scales with the archive, and a timed-out upload is a
transport failure that is retried, never a crash that loses the run.
Combined server and client staging keeps each packager's directory ownership.

===========================================================================
"""

import hashlib
import json
import os
import subprocess
import sys
import tarfile
import tempfile
from pathlib import Path
import unittest
from unittest import mock

import data_release
from plan import git
from test_client_data import release
from test_release_state import production


# ================
# UploadTests
# ================
class UploadTests(unittest.TestCase):
	# ================
	# test_the_limit_grows_with_the_archive
	# ================
	def test_the_limit_grows_with_the_archive(self):
		self.assertEqual(data_release.upload_timeout(1 << 20), data_release.UPLOAD_TIMEOUT_SECONDS)
		eighty = 80 << 20
		self.assertEqual(data_release.upload_timeout(eighty), eighty // data_release.MIN_UPLOAD_BYTES_PER_SECOND)
		self.assertGreater(data_release.upload_timeout(eighty), data_release.UPLOAD_TIMEOUT_SECONDS)

	# ================
	# test_a_timed_out_upload_is_retried
	# ================
	def test_a_timed_out_upload_is_retried(self):
		with tempfile.TemporaryDirectory() as directory:
			archive = Path(directory) / "payload-000.tar"
			archive.write_bytes(b"x" * 1024)
			calls = []

			# ================
			# run
			# ================
			def run(command, stdin, capture_output, timeout):
				calls.append(timeout)
				if len(calls) == 1:
					raise subprocess.TimeoutExpired(command, timeout)
				return subprocess.CompletedProcess(command, 0, stdout=b'{"stored": 1}\n', stderr=b"")

			known = Path(directory) / "known_hosts"
			known.write_text("pinned host\n")
			with mock.patch.object(data_release.time, "sleep"), mock.patch.dict(os.environ, {"RELEASE_KNOWN_HOSTS_FILE": str(known)}):
				self.assertEqual(data_release.send(archive, "stage@host", Path("key"), run), {"stored": 1})
		self.assertEqual(calls, [data_release.UPLOAD_TIMEOUT_SECONDS] * 2)


# ================
# PinnedUploadTests
#
# The upload uses ci.py's isolated, pinned connection and refuses a target
# that ssh could read as an option.
# ================
class PinnedUploadTests(unittest.TestCase):
	# ================
	# test_the_upload_is_pinned_and_isolated
	# ================
	def test_the_upload_is_pinned_and_isolated(self):
		with tempfile.TemporaryDirectory() as directory:
			archive = Path(directory) / "candidate.tar"
			archive.write_bytes(b"x")
			known = Path(directory) / "known_hosts"
			known.write_text("pinned host\n")
			seen = []

			def run(command, stdin, capture_output, timeout):
				seen.append(command)
				return subprocess.CompletedProcess(command, 0, stdout=b'{"candidate": "x"}\n', stderr=b"")

			with mock.patch.dict(os.environ, {"RELEASE_KNOWN_HOSTS_FILE": str(known), "RELEASE_SSH_PROXY_COMMAND": "relay %h %p"}):
				data_release.send(archive, "sro-stage@release.example", Path("key"), run)
			command = seen[0]
			self.assertEqual(command[:3], ["ssh", "-F", "none"])
			for option in ('UserKnownHostsFile="' + str(known) + '"', "GlobalKnownHostsFile=none", "StrictHostKeyChecking=yes",
				"IdentityAgent=none", "IdentitiesOnly=yes", "ProxyCommand=relay %h %p"):
				self.assertIn(option, command)
			self.assertEqual(command[-1], "sro-stage@release.example")
			for target, known_file in (("-oProxyCommand=evil@host", str(known)), ("stage@host", ""), ("stage@host", str(Path(directory) / "absent"))):
				with self.subTest(target=target, known=known_file), mock.patch.dict(os.environ, {"RELEASE_KNOWN_HOSTS_FILE": known_file}):
					with self.assertRaises(ValueError):
						data_release.send(archive, target, Path("key"), lambda *a, **k: self.fail("ssh ran"))


# ================
# InventoryTests
# ================
class InventoryTests(unittest.TestCase):
	# ================
	# test_the_inventory_is_asked_in_bounded_requests
	# ================
	def test_the_inventory_is_asked_in_bounded_requests(self):
		limit = data_release.client_data.MAX_INVENTORY_FILES
		needed = [{"sha256": f"{index:064x}", "length": 1} for index in range(limit + 3)]
		requests = []

		# ================
		# send
		# ================
		def send(request, target, identity):
			body = json.loads(Path(request).read_text(encoding="utf-8"))
			requests.append(len(body["files"]))
			self.assertEqual(body["operation"], "payload-inventory")
			return {"present": [row["sha256"] for row in body["files"][:2]]}

		present = data_release.stored_payloads(needed, "stage@host", Path("key"), send)
		self.assertEqual(requests, [limit, 3])
		self.assertEqual(len(present), 4)

	# ================
	# test_a_host_without_the_inventory_stops_the_release
	#
	# Older controls refuse the operation; the run stops naming the reinstall
	# rather than uploading everything. Any other failure passes through.
	# ================
	def test_a_host_without_the_inventory_stops_the_release(self):
		needed = [{"sha256": "a" * 64, "length": 1}]

		# ================
		# refused
		# ================
		def refused(request, target, identity):
			raise RuntimeError(f"host refused inventory-request.json: ValueError: {data_release.OPERATION_REFUSED}")

		with self.assertRaisesRegex(RuntimeError, "predate it.*Reinstall the controls"):
			data_release.stored_payloads(needed, "stage@host", Path("key"), refused)

		# ================
		# lost
		# ================
		def lost(request, target, identity):
			raise RuntimeError("inventory-request.json was not delivered after 5 attempts")

		with self.assertRaisesRegex(RuntimeError, "not delivered"):
			data_release.stored_payloads(needed, "stage@host", Path("key"), lost)


# ================
# StagingCLITests
#
# Real plans, inventory and packaging, with only origin reads and uploads
# replaced. Every artifact and the simulated payload store is private.
# ================
class StagingCLITests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		directory = tempfile.TemporaryDirectory()
		self.addCleanup(directory.cleanup)
		self.root = Path(directory.name)
		contracts = json.loads((Path(__file__).parent / "compatibility.json").read_bytes())
		self.base = release(self.root / "base", b"old", b"shared pack", b"old publication", contracts["client"])
		self.next = release(self.root / "next", b"new", b"shared pack", b"new publication", contracts["client"])
		self.state = production()
		commit = git(["rev-parse", "HEAD"])
		for component in ("client", "server"):
			self.state[component].update(commit=commit, compatibility=contracts[component])
		self.state["client"]["release"] = self.base["releaseId"]
		self.archive = self.root / "server.srogz"
		self.archive.write_bytes(b"server game data")
		self.output = self.root / "out"
		self.config = {"payload_store": str(self.root / "store")}
		self.sent = []
		self.fetched = []
		self.fail_server = False

	# ================
	# fetch
	# ================
	def fetch(self, origin, path):
		self.assertEqual(origin, "https://example.test")
		self.fetched.append(path)
		return {"/releases/production.json": self.state, "/releases/client.json": self.base}[path]

	# ================
	# send
	#
	# Validate the bytes at the upload boundary before acknowledging them.
	# ================
	def send(self, archive, target, identity):
		archive = Path(archive)
		self.assertEqual((target, identity), ("stage@example.test", Path("key")))
		self.sent.append(archive)
		if archive.name == "inventory-request.json":
			return data_release.client_data.payload_inventory(self.config, json.loads(archive.read_bytes()))
		if archive.name == "server-data.tar":
			self.assertEqual(archive.parent.parent, self.archive.parent)
			with tarfile.open(archive) as package:
				self.assertEqual(package.getnames(),
					[data_release.server_data.DECLARATION, data_release.server_data.ARCHIVE])
				declaration = json.load(package.extractfile(data_release.server_data.DECLARATION))
				data = package.extractfile(data_release.server_data.ARCHIVE).read()
			self.assertEqual(data, self.archive.read_bytes())
			self.assertEqual(declaration, {"format": data_release.server_data.FORMAT,
				"sha256": hashlib.sha256(data).hexdigest(), "length": len(data)})
			if self.fail_server:
				raise RuntimeError("server upload refused")
			return {"staged": declaration["sha256"]}
		if archive.name.startswith("payload-"):
			return data_release.client_data.store_payload(self.config, archive)
		candidate, manifest, _, payloads = data_release.client_data.unpack(archive)
		self.assertEqual(manifest, self.next)
		self.assertEqual(candidate["plan"]["baseRelease"], self.base["releaseId"])
		self.assertEqual(payloads["application/index.html"], b"new")
		for row in data_release.client_data.needed_payloads(self.root / "next", self.base):
			self.assertEqual((Path(self.config["payload_store"]) / row["sha256"]).stat().st_size, row["length"])
		return {"candidate": hashlib.sha256(archive.read_bytes()).hexdigest()}

	# ================
	# run_cli
	#
	# stored_payloads captures its default sender at import; route that real
	# inventory helper through the same injected upload boundary as main.
	# ================
	def run_cli(self, client=True, server=True):
		arguments = ["data_release.py"]
		if client:
			arguments += [str(self.root / "next"), str(self.output)]
		if server:
			arguments += ["--server-data", str(self.archive)]
		arguments += ["--origin", "https://example.test", "--ssh-target", "stage@example.test", "--identity", "key"]
		inventory = data_release.stored_payloads
		with mock.patch.object(sys, "argv", arguments), \
			mock.patch.object(data_release, "fetch_json", side_effect=self.fetch), \
			mock.patch.object(data_release, "send", side_effect=self.send), \
			mock.patch.object(data_release, "stored_payloads",
				side_effect=lambda needed, target, identity: inventory(needed, target, identity, self.send)):
			data_release.main()

	# ================
	# test_combined_cli_sends_both_packages_and_cleans_server_staging
	# ================
	def test_combined_cli_sends_both_packages_and_cleans_server_staging(self):
		self.run_cli()
		self.assertEqual([path.name for path in self.sent],
			["server-data.tar", "inventory-request.json", "payload-000.tar", "candidate.tar"])
		self.assertNotEqual(self.sent[0].parent, self.output)
		self.assertFalse(self.sent[0].parent.exists())
		self.assertEqual(sorted(path.name for path in self.output.iterdir()), ["candidate.tar", "payload-000.tar"])
		self.assertEqual(self.archive.read_bytes(), b"server game data")

	# ================
	# test_client_only_cli_still_sends_payload_and_candidate
	# ================
	def test_client_only_cli_still_sends_payload_and_candidate(self):
		self.run_cli(server=False)
		self.assertEqual([path.name for path in self.sent],
			["inventory-request.json", "payload-000.tar", "candidate.tar"])

	# ================
	# test_server_only_cli_cleans_staging_and_keeps_adjacent_files
	# ================
	def test_server_only_cli_cleans_staging_and_keeps_adjacent_files(self):
		adjacent = self.root / "server-data.tar"
		adjacent.write_bytes(b"keep")
		self.run_cli(client=False)
		self.assertEqual([path.name for path in self.sent], ["server-data.tar"])
		self.assertFalse(self.sent[0].parent.exists())
		self.assertFalse(self.output.exists())
		self.assertEqual(adjacent.read_bytes(), b"keep")

	# ================
	# test_combined_cli_keeps_the_client_fresh_output_guard
	# ================
	def test_combined_cli_keeps_the_client_fresh_output_guard(self):
		self.output.mkdir()
		sentinel = self.output / "keep.txt"
		sentinel.write_bytes(b"keep")
		for server in (True, False):
			with self.subTest(server=server):
				with self.assertRaises(SystemExit) as error:
					self.run_cli(server=server)
				self.assertEqual(error.exception.code, 2)
		self.assertEqual(self.sent, [])
		self.assertEqual(self.fetched, [])
		self.assertEqual(list(self.output.iterdir()), [sentinel])
		self.assertEqual(sentinel.read_bytes(), b"keep")

	# ================
	# test_server_upload_failure_cleans_staging_and_stops_before_client
	# ================
	def test_server_upload_failure_cleans_staging_and_stops_before_client(self):
		adjacent = self.root / "server-data.tar"
		adjacent.write_bytes(b"keep")
		self.fail_server = True
		with self.assertRaisesRegex(RuntimeError, "server upload refused"):
			self.run_cli()
		self.assertEqual(self.fetched, [])
		self.assertEqual([path.name for path in self.sent], ["server-data.tar"])
		self.assertFalse(self.sent[0].parent.exists())
		self.assertFalse(self.output.exists())
		self.assertEqual(adjacent.read_bytes(), b"keep")

	# ================
	# test_server_packaging_failure_cleans_private_staging
	# ================
	def test_server_packaging_failure_cleans_private_staging(self):
		self.archive.unlink()
		before = sorted(self.root.iterdir())
		with self.assertRaises(FileNotFoundError):
			self.run_cli()
		self.assertEqual(self.sent, [])
		self.assertEqual(self.fetched, [])
		self.assertFalse(self.output.exists())
		self.assertEqual(sorted(self.root.iterdir()), before)


if __name__ == "__main__":
	unittest.main()
