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


if __name__ == "__main__":
	unittest.main()
