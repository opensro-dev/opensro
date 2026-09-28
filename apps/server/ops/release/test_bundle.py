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


# ================
# BundleTests
# ================
class BundleTests(unittest.TestCase):
	# ================
	# setUp
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
		bundle(self.module, self.archive, "a" * 40)

	# ================
	# rewrite
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
	# ================
	def test_tampering_writes_nothing(self):
		# ================
		# change
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
	# ================
	def test_path_escape(self):
		# ================
		# change
		# ================
		def change(entries):
			entries[0][0].name = "../outside"
		self.rewrite(change)
		with self.assertRaisesRegex(ValueError, "members"):
			unpack(self.archive, self.root / "unpacked")
		self.assertFalse((self.root / "outside").exists())

	# ================
	# test_symlink
	# ================
	def test_symlink(self):
		# ================
		# change
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
	# ================
	def test_duplicate(self):
		self.rewrite(lambda entries: entries.append(entries[0]))
		with self.assertRaisesRegex(ValueError, "duplicate"):
			unpack(self.archive, self.root / "unpacked")

	# ================
	# test_missing_manifest_file
	# ================
	def test_missing_manifest_file(self):
		# ================
		# change
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


if __name__ == "__main__":
	unittest.main()
