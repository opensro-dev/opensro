"""
===========================================================================

test_data_release.py - the operator upload survives a slow link

The upload limit scales with the archive, and a timed-out upload is a
transport failure that is retried, never a crash that loses the run.

===========================================================================
"""
import json
import os
import subprocess
import tempfile
from pathlib import Path
import unittest
from unittest import mock

import data_release


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

		def refused(request, target, identity):
			raise RuntimeError(f"host refused inventory-request.json: ValueError: {data_release.OPERATION_REFUSED}")

		with self.assertRaisesRegex(RuntimeError, "predate it.*Reinstall the controls"):
			data_release.stored_payloads(needed, "stage@host", Path("key"), refused)

		def lost(request, target, identity):
			raise RuntimeError("inventory-request.json was not delivered after 5 attempts")

		with self.assertRaisesRegex(RuntimeError, "not delivered"):
			data_release.stored_payloads(needed, "stage@host", Path("key"), lost)


# ================
# CombinedRunTests
#
# PACKAGE OUTPUT --server-data in one run (F8, 2026-10-14): the server data
# was sent, then client_data.bundle refused the OUTPUT that its tar had
# already created, and the client half never staged.
# ================
class CombinedRunTests(unittest.TestCase):
	# ================
	# test_server_data_and_package_stage_in_one_run
	# ================
	def test_server_data_and_package_stage_in_one_run(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			package = root / "package"
			package.mkdir()
			(package / "release.json").write_text(json.dumps({"releaseId": "r" * 64}))
			archive = root / "server.srogz"
			archive.write_bytes(b"server data")
			output = root / "out"
			sent = []

			def send(path, target, identity):
				sent.append(Path(path))
				return {"recorded": True}

			def bundle(package_dir, base, plan, out, batch_bytes, present):
				# client_data.bundle's own contract: it creates OUTPUT.
				out.mkdir(parents=True, exist_ok=False)
				(out / "candidate.tar").write_bytes(b"candidate")
				return []

			state = {"client": {"release": "b" * 64, "compatibility": {}}}
			argv = ["data_release.py", str(package), str(output), "--server-data", str(archive),
				"--origin", "https://release.example", "--ssh-target", "stage@host", "--identity", "key"]
			with (
				mock.patch.object(data_release, "send", send),
				mock.patch.object(data_release, "fetch_json", lambda origin, path: state if "production" in path else {"releaseId": "b" * 64}),
				mock.patch.object(data_release, "compatibility", lambda *a: None),
				mock.patch.object(data_release, "build_plan", lambda *a: {}),
				mock.patch.object(data_release, "stored_payloads", lambda *a: set()),
				mock.patch.object(data_release.client_data, "needed_payloads", lambda *a: []),
				mock.patch.object(data_release.client_data, "bundle", bundle),
				mock.patch("sys.argv", argv),
			):
				data_release.main()
			self.assertEqual([path.name for path in sent], ["server-data.tar", "candidate.tar"])
			self.assertNotEqual(sent[0].parent, output, "the server data is written beside OUTPUT, never inside it")
			self.assertEqual(sent[1], output / "candidate.tar")

	# ================
	# test_an_existing_output_stops_before_any_upload
	#
	# client_data.bundle refuses an OUTPUT that exists. Refuse it first, so a
	# run never sends the server data without its client candidate (#812).
	# ================
	def test_an_existing_output_stops_before_any_upload(self):
		with tempfile.TemporaryDirectory() as directory:
			root = Path(directory)
			archive = root / "server.srogz"
			archive.write_bytes(b"server data")
			output = root / "out"
			output.mkdir()
			sentinel = output / "keep.txt"
			sentinel.write_bytes(b"keep")
			sent, fetched = [], []
			for server in (True, False):
				with self.subTest(server=server):
					argv = ["data_release.py", str(root / "package"), str(output)]
					if server:
						argv += ["--server-data", str(archive)]
					argv += ["--origin", "https://release.example", "--ssh-target", "stage@host", "--identity", "key"]
					with (
						mock.patch.object(data_release, "send", lambda *a: sent.append(a)),
						mock.patch.object(data_release, "fetch_json", lambda *a: fetched.append(a)),
						mock.patch("sys.argv", argv),
						self.assertRaises(SystemExit) as error,
					):
						data_release.main()
					self.assertEqual(error.exception.code, 2)
			self.assertEqual((sent, fetched), ([], []))
			self.assertEqual(list(output.iterdir()), [sentinel])
			self.assertEqual(sentinel.read_bytes(), b"keep")
			# No partial state either: the refusal precedes the server data's
			# own directory beside OUTPUT (#1319).
			self.assertFalse(data_release.server_data_directory(output, archive).exists())


if __name__ == "__main__":
	unittest.main()
