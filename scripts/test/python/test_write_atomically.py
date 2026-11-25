"""
===========================================================================

test_write_atomically.py - prepare_client_resources.write_atomically

The asset build keys the extracted tree by size and mtime, so a writer
that rewrites an unchanged file invalidates the next build (#4337: the
optional-teleport import reran all three asset lanes on every build).

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ on sys.path, #3410)
import os
import tempfile
import unittest
from pathlib import Path

import prepare_client_resources


class WriteAtomically(unittest.TestCase):
	def setUp(self):
		self.temp = tempfile.TemporaryDirectory()
		self.target = Path(self.temp.name) / "deep" / "out.json"

	def tearDown(self):
		self.temp.cleanup()

	# ================
	# test_the_same_bytes_keep_the_file_and_its_mtime
	# ================
	def test_the_same_bytes_keep_the_file_and_its_mtime(self):
		prepare_client_resources.write_atomically(self.target, b"{}\n")
		os.utime(self.target, ns=(1_000_000_000, 1_000_000_000))
		prepare_client_resources.write_atomically(self.target, b"{}\n")
		self.assertEqual(self.target.stat().st_mtime_ns, 1_000_000_000)

	# ================
	# test_other_bytes_replace_the_file
	#
	# Same size, other content: still written.
	# ================
	def test_other_bytes_replace_the_file(self):
		prepare_client_resources.write_atomically(self.target, b"{1}\n")
		os.utime(self.target, ns=(1_000_000_000, 1_000_000_000))
		prepare_client_resources.write_atomically(self.target, b"{2}\n")
		self.assertEqual(self.target.read_bytes(), b"{2}\n")
		self.assertNotEqual(self.target.stat().st_mtime_ns, 1_000_000_000)
		self.assertFalse(self.target.with_name("out.json.partial").exists())


if __name__ == "__main__":
	unittest.main()
