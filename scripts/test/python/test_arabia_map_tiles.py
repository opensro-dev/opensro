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


if __name__ == "__main__":
	unittest.main()
