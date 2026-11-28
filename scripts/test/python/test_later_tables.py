"""
===========================================================================

test_later_tables.py - the one reader of a later client's textdata tables

Synthetic tables in each shape the clients ship: a v1.696 shard index
(one cell naming a .txt file per row, mixed-case names, a row the
archive wraps at a shard's end), VSRO's textdataname.txt (an index over
other tables' files), and a whole table. What the reader follows, what
it reads as itself, and what it refuses (#4761).

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import tempfile
import unittest
from pathlib import Path

import alexandria_npcs as characters
import import_vsro_alexandria as stage1
import later_tables
from test_later_world_import import pk2


def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


FILES = {
	# v1.696: an index naming its shards in the archive's own mixed case.
	"textdata_object.txt": utf16([["TextData_Object_100.txt"], ["TextData_Object_200.txt"]]),
	# A row the archive wraps over two lines, at the end of a shard.
	"textdata_object_100.txt": utf16([["1", "SN_A", "Kor"], [" tail", "x"]]),
	"textdata_object_200.txt": utf16([["1", "SN_B", "Kor"]]),
	# VSRO: textdataname.txt indexes other tables' files.
	"textdataname.txt": utf16([["TextData_Equip&Skill.txt"], ["TextData_Object_200.txt"]]),
	"textdata_equip&skill.txt": utf16([["1", "SN_E", "Kor"]]),
	"flat.txt": utf16([["1", "SN_F", "Kor"]]),
	"characterdata.txt": utf16([["CharacterData_100.txt"]]),
	"characterdata_100.txt": utf16([["1", "1900", "MOB_A", "x"], ["1", "1901"]]),
}


# ================
# IndexShapeTests
# ================
class IndexShapeTests(unittest.TestCase):
	# ================
	# test_only_one_cell_txt_rows_make_an_index
	#
	# A comment leads some tables; a table whose first row happens to be
	# one .txt cell but whose later rows carry columns is a table.
	# ================
	def test_only_one_cell_txt_rows_make_an_index(self):
		self.assertTrue(later_tables.is_index(FILES["textdata_object.txt"]))
		self.assertTrue(later_tables.is_index(b"\xff\xfe" + "// shards\r\nA_1.txt\r\n".encode("utf-16-le")))
		self.assertFalse(later_tables.is_index(FILES["flat.txt"]))
		self.assertFalse(later_tables.is_index(utf16([["A_1.txt"], ["1", "SN_X"]])))
		self.assertFalse(later_tables.is_index(utf16([["readme"]])))
		self.assertFalse(later_tables.is_index(b""))

	# ================
	# test_an_index_naming_a_path_or_a_repeat_refuses
	# ================
	def test_an_index_naming_a_path_or_a_repeat_refuses(self):
		for entries in ([["../itemdata_5000.txt"]], [["sub/itemdata_5000.txt"]], [[".itemdata.txt"]],
				[["ItemData_5000.txt"], ["itemdata_5000.txt"]]):
			source = later_tables.Archive.of({"itemdata.txt": utf16(entries)})
			with self.subTest(entries=entries), self.assertRaisesRegex(ValueError, "invalid shard index entries"):
				source.rows("itemdata.txt")


# ================
# ArchiveTests
# ================
class ArchiveTests(unittest.TestCase):
	def setUp(self):
		self.source = later_tables.Archive.of(FILES)

	# ================
	# test_rows_follow_the_index_and_parse_per_shard
	#
	# translation_rows joins a wrapped row; run per file, the join never
	# swallows the next shard's first row.
	# ================
	def test_rows_follow_the_index_and_parse_per_shard(self):
		rows = self.source.rows("TextData_Object.txt", characters.translation_rows)
		self.assertEqual([row[1] for row in rows], ["SN_A", "SN_B"])
		self.assertEqual(rows[0][-1], "x")
		self.assertEqual(self.source.rows("flat.txt"), [["1", "SN_F", "Kor"]])

	# ================
	# test_several_tables_read_each_file_once
	#
	# VSRO's textdataname.txt indexes a shard textdata_object.txt also
	# lists; read together, its rows still come once, in first order.
	# ================
	def test_several_tables_read_each_file_once(self):
		rows = self.source.rows_of(("textdata_object.txt", "textdataname.txt"))
		self.assertEqual([row[1] for row in rows if row[0] == "1"], ["SN_A", "SN_B", "SN_E"])

	# ================
	# test_text_and_files_follow_the_same_shards
	# ================
	def test_text_and_files_follow_the_same_shards(self):
		self.assertEqual(self.source.files("textdata_object.txt"), ["textdata_object_100.txt", "textdata_object_200.txt"])
		self.assertEqual(self.source.files("flat.txt"), ["flat.txt"])
		text = self.source.text("textdata_object.txt")
		self.assertIn("SN_A", text)
		self.assertIn("SN_B", text)
		self.assertNotIn("TextData_Object_100", text)

	# ================
	# test_the_one_file_read_refuses_an_index
	#
	# file (and calling the Archive, as helpers that take a read do) is
	# for a whole table or a shard; an index read that way would be file
	# names as rows.
	# ================
	def test_the_one_file_read_refuses_an_index(self):
		with self.assertRaisesRegex(ValueError, "is a shard index"):
			self.source.file("textdata_object.txt")
		with self.assertRaisesRegex(ValueError, "is a shard index"):
			self.source("textdataname.txt")
		self.assertEqual(self.source("TextData_Object_200.txt"), FILES["textdata_object_200.txt"])

	# ================
	# test_a_v1696_reader_refuses_a_whole_table
	# ================
	def test_a_v1696_reader_refuses_a_whole_table(self):
		with self.assertRaisesRegex(ValueError, "not a v1.696 shard index"):
			self.source.rows("flat.txt", indexed_only=True)
		self.assertEqual(len(self.source.rows("textdata_object.txt", indexed_only=True)), 3)

	# ================
	# test_a_missing_table_is_an_error_unless_optional
	# ================
	def test_a_missing_table_is_an_error_unless_optional(self):
		self.assertFalse(self.source.has("textdata_new.txt"))
		self.assertEqual(self.source.rows("textdata_new.txt", optional=True), [])
		with self.assertRaises(SystemExit):
			self.source.rows("textdata_new.txt")
		missing_shard = later_tables.Archive.of({"itemdata.txt": utf16([["ItemData_5000.txt"]])})
		with self.assertRaises(SystemExit):
			missing_shard.rows("itemdata.txt", optional=True)

	# ================
	# test_a_snapshot_reads_back_through_the_same_index
	# ================
	def test_a_snapshot_reads_back_through_the_same_index(self):
		copied = self.source.snapshot(("textdata_object.txt", "flat.txt"))
		self.assertEqual(sorted(copied), ["flat.txt", "textdata_object.txt", "textdata_object_100.txt", "textdata_object_200.txt"])
		self.assertEqual(later_tables.Archive.of(copied).rows("textdata_object.txt"), self.source.rows("textdata_object.txt"))

	# ================
	# test_characters_by_codename
	# ================
	def test_characters_by_codename(self):
		self.assertEqual(sorted(later_tables.characters(self.source)), ["MOB_A"])


# ================
# SourceTests
#
# A Media archive, an extracted folder and a dict read alike.
# ================
class SourceTests(unittest.TestCase):
	def test_a_media_archive_an_open_one_and_a_folder_read_alike(self):
		with tempfile.TemporaryDirectory() as temp:
			root = Path(temp)
			(root / "Media.pk2").write_bytes(pk2({stage1.TEXTDATA + name: raw for name, raw in FILES.items()}))
			folder = root / "textdata"
			folder.mkdir()
			for name, raw in FILES.items():
				(folder / name).write_bytes(raw)
			expected = later_tables.Archive.of(FILES).rows("textdata_object.txt")
			with later_tables.Archive.open(root) as source:
				self.assertEqual(source.rows("textdata_object.txt"), expected)
				with self.assertRaisesRegex(ValueError, "Media.pk2: textdata_object.txt is a shard index"):
					source.file("textdata_object.txt")
			self.assertEqual(later_tables.Archive.tree(folder).rows("textdata_object.txt"), expected)
			self.assertFalse(later_tables.Archive.tree(root / "absent").has("flat.txt"))
			with self.assertRaises(SystemExit):
				with later_tables.Archive.open(root / "absent"):
					pass


if __name__ == "__main__":
	unittest.main()
