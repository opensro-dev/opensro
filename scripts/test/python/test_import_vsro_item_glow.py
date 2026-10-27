"""
===========================================================================

test_import_vsro_item_glow.py - the item glow importer's archive pin

No game data: the entry point must refuse a client that is not the pinned
VSRO one before any table is read.

===========================================================================
"""
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import import_vsro_item_glow as importer  # noqa: E402


class MainPinTests(unittest.TestCase):
	def setUp(self):
		importer.stage1._verified_archives.clear()
		self.root = tempfile.TemporaryDirectory()
		self.addCleanup(self.root.cleanup)
		self.vsro = Path(self.root.name)

	def run_main(self):
		argv = ["import_vsro_item_glow.py", "--vsro-root", str(self.vsro)]
		with patch.object(sys, "argv", argv):
			importer.main()

	def test_main_refuses_a_client_that_is_not_the_pinned_one(self):
		(self.vsro / "Media.pk2").write_bytes(b"some other client")
		with self.assertRaisesRegex(SystemExit, "does not match the pinned"):
			self.run_main()

	def test_main_refuses_a_missing_archive(self):
		with self.assertRaisesRegex(SystemExit, "not found"):
			self.run_main()


if __name__ == "__main__":
	unittest.main()
