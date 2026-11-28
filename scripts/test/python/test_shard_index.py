"""
===========================================================================
test_shard_index.py - a later layer's shard lines in the tree's index

cap110_monsters.list_shard / unlist_shard keep characterdata.txt and
skilldata.txt true to the shards on disk, the view every built-tree read
follows since #4770 (#4843).
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import tempfile
import unittest
from pathlib import Path

import cap110_monsters as monsters


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# ShardIndexTest
#
# A layer's character shard is listed in the tree's index, so the next
# layer's standing read (tree_characters, which follows the index since
# #4770) sees its kinds and never stages them again (#4843).
# ================
class ShardIndexTest(unittest.TestCase):
	def tree(self, root):
		root.mkdir()
		(root / "characterdata.txt").write_bytes(utf16([["characterdata_5000.txt"]]))
		(root / "characterdata_5000.txt").write_bytes(utf16([["1", "1", "MOB_NATIVE"]]))
		(root / "characterdata_120000.txt").write_bytes(utf16([["1", "50150", "MOB_SD_AKERU"]]))
		return root

	def codes(self, root, own):
		return {row[2] for row in monsters.tree_characters(root, own)}

	def test_a_listed_layer_stands_for_the_next_layer(self):
		with tempfile.TemporaryDirectory() as temp:
			root = self.tree(Path(temp) / "textdata")
			self.assertEqual(self.codes(root, "characterdata_180000.txt"), {"MOB_NATIVE"})
			monsters.list_shard(root, "characterdata.txt", "characterdata_120000.txt")
			self.assertEqual(self.codes(root, "characterdata_180000.txt"), {"MOB_NATIVE", "MOB_SD_AKERU"})
			# The layer's own rebuild still leaves its own rows out.
			self.assertEqual(self.codes(root, "characterdata_120000.txt"), {"MOB_NATIVE"})

	def test_listing_twice_keeps_one_line_in_place(self):
		with tempfile.TemporaryDirectory() as temp:
			root = self.tree(Path(temp) / "textdata")
			monsters.list_shard(root, "characterdata.txt", "characterdata_120000.txt")
			first = (root / "characterdata.txt").read_bytes()
			monsters.list_shard(root, "characterdata.txt", "characterdata_120000.txt")
			self.assertEqual((root / "characterdata.txt").read_bytes(), first)
			self.assertEqual(first, utf16([["characterdata_5000.txt"], ["characterdata_120000.txt"]]))

	def test_unlisting_drops_only_that_line(self):
		with tempfile.TemporaryDirectory() as temp:
			root = self.tree(Path(temp) / "textdata")
			monsters.list_shard(root, "characterdata.txt", "characterdata_120000.txt")
			monsters.unlist_shard(root, "characterdata.txt", "characterdata_120000.txt")
			(root / "characterdata_120000.txt").unlink()
			self.assertEqual((root / "characterdata.txt").read_bytes(), utf16([["characterdata_5000.txt"]]))
			self.assertEqual(self.codes(root, "characterdata_180000.txt"), {"MOB_NATIVE"})
			monsters.unlist_shard(Path(temp) / "absent", "characterdata.txt", "characterdata_120000.txt")


# ================
# ShardCaseTest
#
# An index line names a file, and the tree folds file-name case: a shard
# named in another case is the same line, so a rerun never adds a second.
# ================
class ShardCaseTest(unittest.TestCase):
	def test_a_shard_in_another_case_is_the_same_line(self):
		with tempfile.TemporaryDirectory() as temp:
			root = Path(temp)
			(root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"], ["SkillData_340000.txt"]]))
			monsters.list_shard(root, "skilldata.txt", "SKILLDATA_340000.txt")
			self.assertEqual((root / "skilldata.txt").read_bytes(), utf16([["skilldata_5000.txt"], ["SkillData_340000.txt"]]))
			monsters.unlist_shard(root, "skilldata.txt", "skilldata_340000.txt")
			self.assertEqual((root / "skilldata.txt").read_bytes(), utf16([["skilldata_5000.txt"]]))


if __name__ == "__main__":
	unittest.main()
