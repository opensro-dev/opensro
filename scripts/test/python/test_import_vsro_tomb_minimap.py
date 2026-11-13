"""
===========================================================================

test_import_vsro_tomb_minimap.py - tomb tile selection and its catalogue

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sys
import unittest
from pathlib import Path


import import_vsro_tomb_minimap as importer


def every_floor():
	"""One archive name for each tomb floor."""
	return [f"minimap_d/jinsi/qt_a01_floor0{floor}_126x125.ddj" for floor in range(1, 7)]


class TombMinimapTests(unittest.TestCase):
	def test_only_tomb_tiles_are_taken(self):
		names = every_floor() + ["minimap_d/donwhang/dh_a01_floor01_127x126.ddj", "minimap_d/jinsi/readme.txt"]
		self.assertEqual(len(importer.tomb_tiles(names)), 6)

	def test_each_floor_lands_on_its_region(self):
		catalog = importer.tomb_catalog(importer.tomb_tiles(every_floor()))
		self.assertEqual(
			[dungeon["sectorId"] for dungeon in catalog["dungeons"]],
			[0x8007, 0x8006, 0x8005, 0x8004, 0x8003, 0x8002]
		)
		floor = catalog["dungeons"][0]["floors"][0]
		self.assertEqual(floor["floorIndex"], 0)
		self.assertEqual(floor["prefix"], "qt_a01_floor01")
		self.assertEqual(floor["tiles"], ["126x125"])
		self.assertEqual(
			catalog["tilePaths"][0],
			"/assets/images/Media_extracted/minimap_d/jinsi/qt_a01_floor01_126x125.png"
		)

	def test_a_missing_floor_stops_the_import(self):
		with self.assertRaises(SystemExit):
			importer.tomb_catalog(importer.tomb_tiles(every_floor()[:5]))

	def test_a_non_ddj_tile_is_refused(self):
		with self.assertRaises(ValueError):
			importer.ddj_payload(b"not a texture" * 4)


if __name__ == "__main__":
	unittest.main()
