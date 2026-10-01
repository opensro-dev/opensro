"""
===========================================================================
test_bundle.py - artifact integrity and extraction boundaries
===========================================================================
"""

import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from bundle import FILES, bundle, unpack
from deploy import receive
from test_release_state import candidate


# ================
# BundleTests
#
# Build real tar artifacts and mutate archive entries, never implementation
# text, to prove rejection happens before any extraction side effects.
# ================
class BundleTests(unittest.TestCase):
	# ================
	# setUp
	#
	# Give every case a complete private artifact and a fresh extraction target.
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		self.root = Path(self.directory.name)
		self.module = self.root / "module"
		for name, source in FILES.items():
			path = self.module / source
			path.parent.mkdir(parents=True, exist_ok=True)
			path.write_bytes(("fixture: " + name).encode())
		self.archive = self.root / "server.tar"
		plan = candidate("server")
		plan.update(commit="a" * 40, release="a" * 40)
		bundle(self.module, self.archive, "a" * 40, plan)

	# ================
	# rewrite
	#
	# Repack a modified archive so the production reader must verify its content.
	# ================
	def rewrite(self, change):
		with tarfile.open(self.archive, "r:") as archive:
			entries = [(member, archive.extractfile(member).read()) for member in archive.getmembers()]
		change(entries)
		with tarfile.open(self.archive, "w") as archive:
			for member, data in entries:
				archive.addfile(member, io.BytesIO(data))

	# ================
	# test_round_trip
	#
	# Receive and unpack exactly the bytes the builder produced.
	# ================
	def test_round_trip(self):
		destination = self.root / "unpacked"
		manifest = unpack(self.archive, destination)
		self.assertEqual(manifest["commit"], "a" * 40)
		for name, source in FILES.items():
			self.assertEqual((destination / name).read_bytes(), (self.module / source).read_bytes())
		with self.archive.open("rb") as stream:
			receive(stream, self.root / "received.tar")
		self.assertEqual(self.archive.read_bytes(), (self.root / "received.tar").read_bytes())

	# ================
	# test_tampering_writes_nothing
	#
	# A digest mismatch must leave the extraction destination absent.
	# ================
	def test_tampering_writes_nothing(self):
		# ================
		# change
		# Corrupt payload bytes without changing their recorded digest.
		# ================
		def change(entries):
			member, data = entries[0]
			entries[0] = (member, b"X" + data[1:])
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "digest mismatch"):
			unpack(self.archive, self.root / "unpacked")
		self.assertFalse((self.root / "unpacked").exists())

	# ================
	# test_path_escape
	# Reject archive paths before any directory creation.
	# ================
	def test_path_escape(self):
		# ================
		# change
		# Attempt to replace a declared member with a parent-relative path.
		# ================
		def change(entries):
			entries[0][0].name = "../outside"
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "members"):
			unpack(self.archive, self.root / "unpacked")
		self.assertFalse((self.root / "outside").exists())

	# ================
	# test_symlink
	# Treat every non-regular member as invalid, including empty symlinks.
	# ================
	def test_symlink(self):
		# ================
		# change
		# A link must be rejected even when it names an otherwise expected member.
		# ================
		def change(entries):
			member, _ = entries[0]
			member.type = tarfile.SYMTYPE
			member.linkname = "/etc/passwd"
			member.size = 0
			entries[0] = (member, b"")
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "non-file"):
			unpack(self.archive, self.root / "unpacked")

	# ================
	# test_duplicate
	# Duplicate tar names must not override a previously verified member.
	# ================
	def test_duplicate(self):
		self.rewrite(lambda entries: entries.append(entries[0]))
		with self.assertRaisesRegex(ValueError, "duplicate"):
			unpack(self.archive, self.root / "unpacked")

	# ================
	# test_missing_manifest_file
	# Every payload must have a corresponding manifest digest.
	# ================
	def test_missing_manifest_file(self):
		# ================
		# change
		# Preserve valid JSON while removing one required digest.
		# ================
		def change(entries):
			member, data = entries[-1]
			manifest = json.loads(data)
			manifest["files"].pop("agent")
			data = json.dumps(manifest).encode()
			member.size = len(data)
			entries[-1] = (member, data)
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "file list"):
			unpack(self.archive, self.root / "unpacked")

	# ================
	# test_release_built_before_an_added_file
	#
	# A release retained before a file joined the bundle still unpacks with its
	# own file list; an undeclared member is still refused.
	# ================
	def test_release_built_before_an_added_file(self):
		# ================
		# drop_upgrader
		# Remove the added file from both the members and the manifest.
		# ================
		def drop_upgrader(entries):
			entries[:] = [entry for entry in entries if entry[0].name != "sro-authority-upgrade"]
			member, data = entries[-1]
			manifest = json.loads(data)
			manifest["files"].pop("sro-authority-upgrade")
			data = json.dumps(manifest).encode()
			member.size = len(data)
			entries[-1] = (member, data)
		self.rewrite(drop_upgrader)
		destination = self.root / "unpacked"
		manifest = unpack(self.archive, destination)
		self.assertNotIn("sro-authority-upgrade", manifest["files"])
		self.assertFalse((destination / "sro-authority-upgrade").exists())
		self.assertTrue((destination / "agent").exists())
		# ================
		# stray
		# A member the manifest does not name.
		# ================
		def stray(entries):
			member = tarfile.TarInfo("sro-authority-upgrade")
			member.size = 1
			entries.insert(0, (member, b"x"))
		self.rewrite(stray)
		with self.assertRaisesRegex(ValueError, "members"):
			unpack(self.archive, self.root / "second")


if __name__ == "__main__":
	unittest.main()
