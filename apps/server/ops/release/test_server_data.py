"""
===========================================================================
test_server_data.py - a server release opens only game data it accepts
===========================================================================
"""

import hashlib
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import server_data


# ================
# upload
#
# A stage upload for these archive bytes, optionally with a wrong digest.
# ================
def upload(directory, data, sha=None):
	declaration = json.dumps({"format": server_data.FORMAT, "sha256": sha or hashlib.sha256(data).hexdigest(),
		"length": len(data)}).encode()
	path = Path(directory) / "upload.tar"
	with tarfile.open(path, "w") as package:
		for name, body in ((server_data.DECLARATION, declaration), (server_data.ARCHIVE, data)):
			entry = tarfile.TarInfo(name)
			entry.size = len(body)
			package.addfile(entry, io.BytesIO(body))
	return path


# ================
# ServerDataTests
# ================
class ServerDataTests(unittest.TestCase):
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		root = Path(self.directory.name)
		self.live = root / "game-data" / "server.srogz"
		self.live.parent.mkdir()
		self.live.write_bytes(b"live archive")
		self.config = {"game_data": str(self.live)}
		self.staging = root / "staging"
		self.staging.mkdir()

	def tearDown(self):
		self.directory.cleanup()

	# ================
	# accept
	#
	# A fake sro-game-data-check verdict: archives whose bytes are listed pass.
	# ================
	def accept(self, *accepted):
		(self.staging / server_data.CHECK_TOOL).write_text("fixture")
		return patch.object(server_data, "check",
			side_effect=lambda staging, archive: Path(archive).read_bytes() in accepted)

	def test_stage_verifies_and_marks_pending(self):
		result = server_data.stage(self.config, upload(self.directory.name, b"new archive"))
		self.assertTrue(result["pending"])
		self.assertEqual(server_data.pending(self.config).read_bytes(), b"new archive")
		with self.assertRaisesRegex(ValueError, "digest"):
			server_data.stage(self.config, upload(self.directory.name, b"tampered", sha="0" * 64))

	def test_staging_the_live_archive_clears_pending(self):
		server_data.stage(self.config, upload(self.directory.name, b"new archive"))
		result = server_data.stage(self.config, upload(self.directory.name, b"live archive"))
		self.assertTrue(result["installed"])
		self.assertIsNone(server_data.pending(self.config))

	def test_a_pending_archive_must_pass(self):
		server_data.stage(self.config, upload(self.directory.name, b"new archive"))
		with self.accept(b"new archive"):
			self.assertEqual(server_data.choose(self.config, self.staging).read_bytes(), b"new archive")
		with self.accept(b"live archive"), self.assertRaisesRegex(RuntimeError, "staged server game data is refused"):
			server_data.choose(self.config, self.staging)

	def test_installed_archive_needs_no_install(self):
		with self.accept(b"live archive"):
			self.assertIsNone(server_data.choose(self.config, self.staging))

	def test_refused_installed_archive_falls_back_to_a_retained_one(self):
		# Install a newer archive, retaining the old: a revert accepts only the old.
		server_data.stage(self.config, upload(self.directory.name, b"new archive"))
		server_data.install(self.config, server_data.pending(self.config))
		self.assertEqual(self.live.read_bytes(), b"new archive")
		self.assertIsNone(server_data.pending(self.config))
		with self.accept(b"live archive"):
			chosen = server_data.choose(self.config, self.staging)
		self.assertEqual(chosen.read_bytes(), b"live archive")
		with self.accept(), self.assertRaisesRegex(RuntimeError, "refuses the installed server game data"):
			server_data.choose(self.config, self.staging)

	def test_an_older_release_is_not_asked(self):
		self.assertTrue(server_data.check(self.staging, self.live))

	def test_upload_must_hold_only_its_declaration_and_archive(self):
		path = Path(self.directory.name) / "odd.tar"
		with tarfile.open(path, "w") as package:
			entry = tarfile.TarInfo("payload.json")
			entry.size = 2
			package.addfile(entry, io.BytesIO(b"{}"))
		with self.assertRaisesRegex(ValueError, "exactly its declaration and archive"):
			server_data.stage(self.config, path)


if __name__ == "__main__":
	unittest.main()
