"""
===========================================================================

test_import_vsro_dungeon_maps.py - the Dungeonmap rows, signs and tiles

===========================================================================
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))

import import_vsro_dungeon_maps as importer


def map_line(map_id, layer, region, prefix="interface\\worldmap\\dungeon\\qt_a01_", label="UIIT_STT_JANGAN"):
	"""One Dungeonmap row: only the cells the importer reads are filled."""
	cells = [""] * 18
	cells[0] = str(map_id)
	cells[3] = str(layer)
	cells[5] = str(region)
	cells[6] = "2"
	cells[7] = "2"
	cells[8] = "512"
	cells[9] = "512"
	cells[10] = "170"
	cells[11] = "103"
	cells[12] = "171"
	cells[13] = "102"
	cells[15] = prefix
	cells[17] = label
	return "\t".join(cells)


def sign_line(kind, map_id, code, comment, name_code=""):
	"""One worldmap_localinfo row: only the cells the importer reads are filled."""
	cells = [""] * 22
	cells[2] = kind
	cells[3] = code
	cells[6] = comment
	cells[8] = str(map_id)
	cells[11] = "120"
	cells[12] = "34"
	cells[13] = "16"
	cells[14] = "9"
	cells[15] = "255"
	cells[16] = "128"
	cells[17] = "0"
	cells[21] = name_code
	return "\t".join(cells)


class DungeonMapRowTests(unittest.TestCase):
	def test_only_the_ports_floors_are_taken(self):
		lines = [
			"#section\tWorldmap",
			map_line(2001, 1, 32775),
			"#section\tDungeonmap",
			"// a comment",
			"not a row",
			map_line(1999, 1, 32775),
			map_line(2001, 1, 32775),
			map_line(2011, 1, 32775),
		]
		rows = importer.map_rows(lines)
		self.assertEqual([row["id"] for row in rows], [2001])
		row = rows[0]
		self.assertEqual(row["regionId"], 32775)
		self.assertEqual(row["columns"], 2)
		self.assertEqual(row["prefix"], "qt_a01_")
		self.assertEqual(row["floorLabel"], "UIIT_STT_JANGAN")

	def test_floors_of_one_region_number_from_its_first_layer(self):
		lines = [
			"#section\tDungeonmap",
			map_line(2001, 1, 32775),
			map_line(2002, 3, 32774),
			map_line(2003, 1, 32773),
			map_line(2004, 2, 32773),
		]
		rows = importer.map_rows(lines)
		# A lone floor keeps index 0 whatever its layer; two floors in one
		# region number from the first layer, so the client picks its page.
		self.assertEqual([row["floorIndex"] for row in rows], [0, 0, 0, 1])


class DungeonMapSignTests(unittest.TestCase):
	def rows_and_names(self):
		rows = importer.map_rows(["#section\tDungeonmap", map_line(2001, 1, 32775)])
		names = importer.english_text(["x\tSN_ROOM_A\tKorean A\t\t\t\t\t\tEnglish A", "x\tSN_ROOM_B\tKorean B"])
		return rows, names

	def test_a_label_reads_its_codes_name(self):
		rows, names = self.rows_and_names()
		importer.add_signs(rows, [sign_line("1", 2001, "ROOM_A", "the table comment")], names)
		label = rows[0]["labels"][0]
		self.assertEqual(label["text"], "English A")
		self.assertEqual((label["x"], label["y"]), (120, 34))
		self.assertEqual(label["font"], 16)
		self.assertEqual(label["color"], [255, 128, 0])
		self.assertEqual(rows[0]["signs"], [])

	def test_a_missing_english_falls_back_to_korean_then_comment(self):
		rows, names = self.rows_and_names()
		importer.add_signs(rows, [
			sign_line("1", 2001, "ROOM_B", "the table comment"),
			sign_line("1", 2001, "ROOM_C", "the table comment"),
		], names)
		self.assertEqual([label["text"] for label in rows[0]["labels"]], ["Korean B", "the table comment"])

	def test_only_the_gate_icon_becomes_a_sign(self):
		rows, names = self.rows_and_names()
		importer.add_signs(rows, [
			sign_line("2", 2001, "map_world_iconwf", "to Jangan", "ROOM_A"),
			sign_line("2", 2001, "map_world_icon", "not a gate"),
			sign_line("2", 9999, "map_world_iconwf", "no such floor"),
			sign_line("1", 2001, "ROOM_A", "a room"),
		], names)
		sign = rows[0]["signs"][0]
		self.assertEqual(len(rows[0]["signs"]), 1)
		self.assertEqual(sign["text"], "to Jangan")
		self.assertEqual(sign["name"], "English A")
		self.assertEqual((sign["width"], sign["height"]), (16, 9))


class DungeonMapCaptionTests(unittest.TestCase):
	def test_a_page_titles_with_its_regions_zone_name(self):
		rows = importer.map_rows(["#section\tDungeonmap", map_line(2005, 1, 32775), map_line(2010, 6, 32770)])
		zones = importer.english_text([
			"1\t32775\tKorean B1\t\t\t\t\t\tUnderground Level 1 of Tomb of Qin-Shi",
			"1\t32770\tKorean B6\t\t\t\t\t\tUnderground Level 6 of Tomb of Qin-Shi ",
		])
		importer.add_captions(rows, zones)
		# The title is the region's, not the row's layer: B6 is region 32770.
		self.assertEqual(
			[row["caption"] for row in rows],
			["Underground Level 1 of Tomb of Qin-Shi", "Underground Level 6 of Tomb of Qin-Shi"]
		)

	def test_a_region_without_a_zone_name_stops_the_import(self):
		rows = importer.map_rows(["#section\tDungeonmap", map_line(2001, 1, 32769)])
		with self.assertRaises(SystemExit):
			importer.add_captions(rows, {})

	def test_the_monster_note_is_required(self):
		names = importer.english_text(["1\tUIIT_STT_WORLDMAP_LEVEL_MONSTER_LIST\tKorean\t\t\t\t\t\tDisplay 6 lv."])
		self.assertEqual(importer.required_text(names, importer.MONSTER_LEVEL_NOTE, "ui"), "Display 6 lv.")
		with self.assertRaises(SystemExit):
			importer.required_text({}, importer.MONSTER_LEVEL_NOTE, "ui")


class DungeonMapTileTests(unittest.TestCase):
	def test_tiles_name_and_place_each_cell(self):
		row = {"prefix": "qt_a01_", "left": 170, "right": 171, "bottom": 102, "top": 103}
		self.assertEqual(importer.tile_cells(row), [
			("qt_a01_170x102", 0, 1),
			("qt_a01_170x103", 0, 0),
			("qt_a01_171x102", 1, 1),
			("qt_a01_171x103", 1, 0),
		])


if __name__ == "__main__":
	unittest.main()
