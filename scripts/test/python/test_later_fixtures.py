"""
===========================================================================

test_later_fixtures.py - the fixture builders make the shapes they claim

later_fixtures stands in for the clients' table shapes in every importer
test (#4762), so a builder that drifted from the real shape would let a
whole family of tests pass on a shape no client ships. These pin each
builder against later_tables and the text-row join that read the real
archives.

===========================================================================
"""
import tempfile
import unittest
from pathlib import Path

import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import later_fixtures  # noqa: E402
import later_tables  # noqa: E402
from alexandria_npcs import translation_rows  # noqa: E402


# ================
# LaterFixturesTests
# ================
class LaterFixturesTests(unittest.TestCase):

	# ================
	# test_archive_case_spells_a_shard_as_the_indexes_do
	# ================
	def test_archive_case_spells_a_shard_as_the_indexes_do(self):
		self.assertEqual(later_fixtures.archive_case("itemdata_5000.txt"), "Itemdata_5000.txt")
		self.assertEqual(later_fixtures.archive_case("textquest_speech&name_100.txt"), "Textquest_Speech&Name_100.txt")

	# ================
	# test_a_sharded_tree_reads_through_its_mixed_case_index
	# ================
	def test_a_sharded_tree_reads_through_its_mixed_case_index(self):
		with tempfile.TemporaryDirectory() as temp:
			root = Path(temp)
			later_fixtures.write_sharded(root, "skilldata.txt", {
				"skilldata_5000.txt": [["1", "5000", "SKILL_A"]],
				"skilldata_6000.txt": [["1", "6000", "SKILL_B"]],
			})
			archive = later_tables.Archive.tree(root)
			self.assertTrue(later_tables.is_index((root / "skilldata.txt").read_bytes()))
			self.assertEqual([row[2] for row in archive.rows("skilldata.txt", width=3)], ["SKILL_A", "SKILL_B"])

	# ================
	# test_a_name_index_lists_other_tables_files
	# ================
	def test_a_name_index_lists_other_tables_files(self):
		files = later_fixtures.name_index({"textdata_equip&skill.txt": [["1", "SN_E", "kor"]]})
		self.assertEqual(sorted(files), ["textdata_equip&skill.txt", "textdataname.txt"])
		self.assertTrue(later_tables.is_index(files["textdataname.txt"]))

	# ================
	# test_a_wrapped_last_row_joins_back_whole
	# ================
	def test_a_wrapped_last_row_joins_back_whole(self):
		rows = [["1", "SN_A", "kor", "", "en"], ["1", "SN_B", "kor text", "", "en", "vi"]]
		wrapped = later_fixtures.wrap_last(rows)
		self.assertEqual(len(wrapped), 3)
		self.assertEqual(translation_rows(later_fixtures.utf16(wrapped)), rows)

	# ================
	# test_a_row_with_no_spaced_cell_is_refused
	# ================
	def test_a_row_with_no_spaced_cell_is_refused(self):
		with self.assertRaises(ValueError):
			later_fixtures.wrap_last([["1", "SN_A", "kor", "en"]])


if __name__ == "__main__":
	unittest.main()
