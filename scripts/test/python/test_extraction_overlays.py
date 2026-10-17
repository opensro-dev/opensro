"""
===========================================================================
test_extraction_overlays.py - merging an overlay leaves the retail tree alone

A synthetic retail extraction and overlay in a temporary game root: the
merged tree has the overlay's files and appended rows, the retail files keep
their bytes, and a row retail already has stops the merge.
===========================================================================
"""

import json
import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2]))
import merge_extraction_overlays as merger

TABLE = pathlib.Path("Media_extracted/server_dep/silkroad/textdata/textdataname.txt")


# ================
# MergeTests
# ================
class MergeTests(unittest.TestCase):
	# ================
	# setUp
	# ================
	def setUp(self):
		self.directory = tempfile.TemporaryDirectory()
		self.addCleanup(self.directory.cleanup)
		root = pathlib.Path(self.directory.name)
		self.saved = (merger.EXTRACTED, merger.MERGED, merger.OVERLAYS)
		self.addCleanup(self.restore)
		merger.EXTRACTED = root / "extracted"
		merger.MERGED = root / "extracted-merged"
		merger.OVERLAYS = merger.EXTRACTED / "overlays"
		self.retail_table = merger.encode_text(["1\tSN_A\ta", "1\tSN_B\tb"])
		(merger.EXTRACTED / TABLE).parent.mkdir(parents=True)
		(merger.EXTRACTED / TABLE).write_bytes(self.retail_table)
		(merger.EXTRACTED / "Data_extracted").mkdir()
		(merger.EXTRACTED / "Data_extracted/model.bsr").write_bytes(b"retail model")
		self.overlay = merger.OVERLAYS / "cap110" / "set"
		(self.overlay / TABLE).parent.mkdir(parents=True)
		(self.overlay / "Data_extracted").mkdir()
		(self.overlay / "Data_extracted/new.bsr").write_bytes(b"new model")
		(self.overlay / "overlay.json").write_text("{}")

	# ================
	# restore
	# ================
	def restore(self):
		merger.EXTRACTED, merger.MERGED, merger.OVERLAYS = self.saved

	# ================
	# test_adds_files_and_appends_rows_without_touching_retail
	# ================
	def test_adds_files_and_appends_rows_without_touching_retail(self):
		(self.overlay / TABLE).write_bytes(merger.encode_text(["1\tSN_C\tc"]))
		manifest = merger.merge(["cap110"])
		merged_rows = merger.read_rows(merger.MERGED / TABLE)
		self.assertEqual(merged_rows, ["1\tSN_A\ta", "1\tSN_B\tb", "1\tSN_C\tc"])
		self.assertEqual((merger.EXTRACTED / TABLE).read_bytes(), self.retail_table)
		self.assertEqual((merger.MERGED / "Data_extracted/new.bsr").read_bytes(), b"new model")
		self.assertEqual((merger.MERGED / "Data_extracted/model.bsr").read_bytes(), b"retail model")
		self.assertFalse((merger.EXTRACTED / "Data_extracted/new.bsr").exists())
		self.assertEqual(manifest["overlays"], ["cap110"])
		recorded = json.loads((merger.MERGED / merger.MANIFEST).read_text(encoding="utf-8"))
		self.assertEqual(recorded["appendedTables"][TABLE.as_posix()], [{"set": "set", "rows": 1}])

	# ================
	# test_a_row_retail_already_has_stops_the_merge
	# ================
	def test_a_row_retail_already_has_stops_the_merge(self):
		(self.overlay / TABLE).write_bytes(merger.encode_text(["1\tSN_B\tother"]))
		with self.assertRaises(SystemExit):
			merger.merge(["cap110"])

	# ================
	# test_a_different_file_at_a_retail_path_stops_the_merge
	# ================
	def test_a_different_file_at_a_retail_path_stops_the_merge(self):
		(self.overlay / TABLE).write_bytes(merger.encode_text([]))
		(self.overlay / "Data_extracted/model.bsr").write_bytes(b"not retail")
		with self.assertRaises(SystemExit):
			merger.merge(["cap110"])


if __name__ == "__main__":
	unittest.main()
