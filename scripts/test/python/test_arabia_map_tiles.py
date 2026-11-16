"""
===========================================================================

test_arabia_map_tiles.py - Arabia's M map and minimap tiles

The import owns the tiles the variant lacks and swaps the ones it holds,
keeping the native bytes so the off path restores them instead of
deleting native map art.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import arabia_map_tiles as arabia

# Region 0x5a50: x 0x50 = 80, y 0x5a = 90; 0x8001 is a dungeon id.
OUTDOOR, DUNGEON = (90 << 8) | 80, 0x8001
BOX = (78, 92, 81, 89)
TILE = "interface/worldmap/map/map_world_78x92.ddj"


class MapTiles(unittest.TestCase):

	def test_paths_cover_outdoor_regions_only(self):
		paths = arabia.map_tile_paths({OUTDOOR: "a", DUNGEON: "b"}, BOX)
		self.assertEqual(paths, ["minimap/80x90.ddj", TILE])

	def test_owned_replaced_and_restored(self):
		with tempfile.TemporaryDirectory() as temp:
			output = Path(temp)
			native = output / "Media_extracted" / TILE
			native.parent.mkdir(parents=True)
			native.write_bytes(b"native")
			index = {"minimap/80x90.ddj": b"mini", TILE: b"later"}
			with mock.patch.object(arabia.sro_pk2, "payload", lambda media, entry: entry):
				owned, replaced = arabia.import_map_tiles(None, index, output, list(index))
				# A second on run keeps the first native copy, not the later art.
				arabia.import_map_tiles(None, index, output, list(index))
			self.assertEqual(owned, ["Media_extracted/minimap/80x90.ddj"])
			self.assertEqual(replaced, ["Media_extracted/" + TILE])
			self.assertEqual(native.read_bytes(), b"later")
			arabia.restore_map_tiles(output, {"replacedTiles": replaced})
			self.assertEqual(native.read_bytes(), b"native")

	def test_label_rows_keep_arabia_overview_rows(self):
		def later(key, kind, destination, page, x, y):
			return ["1", key, kind, "SN_ZONE_" + key if kind == "1" else r"interface\worldmap\map\city_bagdad.ddj",
				"", "", "", destination, page, str(x), str(y)] + ["0"] * 13
		rows = [
			later("22010", "1", "0", "0", 80, 90),  # Arabia name on the overview
			later("100000", "2", "18", "0", 80, 90),  # badge to an unpublished page
			later("11001", "1", "0", "1", 80, 90),  # a town page's label
			later("23000", "1", "0", "0", 168, 97),  # not an Arabia region
			later("26000", "1", "0", "0", 80, 90),  # the native table holds it
		]
		pins = arabia.map_label_rows(rows, {OUTDOOR: "a"}, {"26000"}, {"0", "1"})
		self.assertEqual([row[0] for row in pins], ["22010", "100000"])
		self.assertTrue(all(len(row) == 20 for row in pins))
		self.assertEqual(pins[1][6], "0", "no page to open, no click")

	def test_baghdad_page_comes_with_its_pins(self):
		def later(key, kind, destination, page, x, y):
			return ["1", key, kind, "SN_ZONE_" + key, "", "", "", destination, page, str(x), str(y)] + ["0"] * 13
		badge = later("100000", "2", "18", "0", 80, 90)
		badge[3] = r"interface\worldmap\map\city_bagdad.ddj"
		pins = [badge, later("99214", "1", "0", "18", -1, -1), later("99300", "1", "0", "17", -1, -1)]
		page = ["18", "1", "Baghdad", r"interface\worldmap\map\map_bagdad.ddj"] + ["1"] * 15 + ["xxx", "TITLE", "1", "xxx", "1.0"]
		other = ["19", "1", "Dungeon", r"interface\worldmap\map\map_bagdad_dungeon.ddj"] + ["1"] * 15
		rows = arabia.map_page_rows([page, other], pins, {OUTDOOR: "a"}, {"0", "1"})
		self.assertEqual(rows, [page[:19]], "only the page an Arabia badge opens, in the v1.150 width")
		kept = arabia.map_label_rows(pins, {OUTDOOR: "a"}, set(), {"0", "1", "18"}, {"18"})
		self.assertEqual([row[0] for row in kept], ["100000", "99214"])
		self.assertEqual(kept[0][6], "18", "the badge opens the town page")

	def test_remove_drops_an_added_page(self):
		native = "0\t0\tworld\r\n"
		with tempfile.TemporaryDirectory() as temp:
			path = Path(temp) / arabia.MAPINFO
			path.write_bytes(b"\xff\xfe" + native.encode("utf-16-le"))
			raw = path.read_bytes()
			path.write_bytes(arabia.append_rows(raw, [["18", "1", "Baghdad"]]))
			arabia.remove_map_labels(Path(temp), {"mapPages": ["18"]})
			self.assertEqual(path.read_bytes(), raw)

	def test_later_text_reads_utf16_without_bom(self):
		line = ["1", "9669", "SN_ZONE_26000_15", "K", "0", "0", "0", "0", "0", "Expeditionary Force Outpost"]
		raw = ("\t".join(line) + "\n").encode("utf-16-le")
		self.assertEqual(arabia.later_rows(raw)[0][arabia.LATER_TEXT_ENGLISH], "Expeditionary Force Outpost")


	def test_labels_keep_a_line_break_inside_a_description(self):
		# A description cell holds a bare LF; rows end in CR LF. Adding and
		# removing the map texts must leave that row's bytes as they were.
		native = "﻿1\tSN_ITEM\tK\t\tline one\nline two\t\t\t\tItem\t\t\r\n"
		raw = native.encode("utf-16-le")
		with tempfile.TemporaryDirectory() as temp:
			path = Path(temp) / arabia.TEXT_NAMES
			path.write_bytes(raw)
			row = ["1", "SN_ZONE_22010"] + ["Arabia"] * 9
			path.write_bytes(arabia.append_rows(path.read_bytes(), [row]))
			self.assertTrue(path.read_bytes().startswith(raw))
			arabia.remove_map_labels(Path(temp), {"mapTexts": ["SN_ZONE_22010"]})
			self.assertEqual(path.read_bytes(), raw)


if __name__ == "__main__":
	unittest.main()
