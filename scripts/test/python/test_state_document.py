"""
===========================================================================

test_state_document.py - the shared reader and writer of build state files

A foreign, stale or broken file reads as nothing, and an unchanged body is
not rewritten (its mtime stays, so the trees' stat fingerprints hold).

===========================================================================
"""
import os
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import state_document  # noqa: E402

FORMAT = "sro-test-state"


class StateDocumentTest(unittest.TestCase):
	def setUp(self):
		self.folder = tempfile.TemporaryDirectory()
		self.path = Path(self.folder.name) / "state.json"

	def tearDown(self):
		self.folder.cleanup()

	def test_a_written_body_reads_back(self):
		self.assertTrue(state_document.write_document(self.path, FORMAT, "entries", {"a": [1, 2]}, 3))
		self.assertEqual(state_document.read_document(self.path, FORMAT, "entries", 3), {"a": [1, 2]})

	def test_a_missing_foreign_or_broken_file_reads_as_none(self):
		self.assertIsNone(state_document.read_document(self.path, FORMAT, "entries"))
		state_document.write_document(self.path, "other-format", "entries", {"a": 1})
		self.assertIsNone(state_document.read_document(self.path, FORMAT, "entries"))
		state_document.write_document(self.path, FORMAT, "entries", {"a": 1}, 1)
		self.assertIsNone(state_document.read_document(self.path, FORMAT, "entries", 2))
		self.path.write_text("{not json", encoding="utf-8")
		self.assertIsNone(state_document.read_document(self.path, FORMAT, "entries"))
		self.path.write_text('{"format": "sro-test-state", "entries": [1]}', encoding="utf-8")
		self.assertIsNone(state_document.read_document(self.path, FORMAT, "entries"))

	def test_an_unchanged_body_is_not_rewritten(self):
		state_document.write_document(self.path, FORMAT, "entries", {"b": 2, "a": 1})
		old = 1_000_000_000
		os.utime(self.path, (old, old))
		self.assertFalse(state_document.write_document(self.path, FORMAT, "entries", {"a": 1, "b": 2}))
		self.assertEqual(int(self.path.stat().st_mtime), old)
		self.assertTrue(state_document.write_document(self.path, FORMAT, "entries", {"a": 2}))
		self.assertNotEqual(int(self.path.stat().st_mtime), old)


if __name__ == "__main__":
	unittest.main()
