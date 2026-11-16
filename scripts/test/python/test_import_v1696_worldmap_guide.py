"""
===========================================================================

test_import_v1696_worldmap_guide.py - the M map guide importer (#3387)

The tables' columns, the region pairing and the hunting-ground mask
rule, on rows and pixels built here: the archive itself stays behind
its pin.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import struct
import sys
import tempfile
import unittest
from pathlib import Path

from PIL import Image

import import_v1696_worldmap_guide as guide  # noqa: E402
import import_vsro_worldmap_buttons as buttons  # noqa: E402

MONSTER_MAIN = ["1", "k", "FIELD_CHINA", "field", "main", "10100", "10800", "10100", "0", "0", "0", "0", "0", "0",
	"xxx", "1", "0"]


def monster_row(code, page="field", tier="1", continent="0"):
	return ["1", "k", code, page, "sub", "10101", "10801", "10101", "3212", "559", "166", "97", "12", "47",
		"0x80000005,0x80000006", tier, continent]


def ddj(width, height, pixels):
	"""A JMXVDDJ wrapper around an uncompressed A8R8G8B8 DDS."""
	header = bytearray(guide.DDS_HEADER_BYTES)
	header[0:4] = b"DDS "
	struct.pack_into("<II", header, 12, height, width)
	struct.pack_into("<I", header, 88, 32)
	dds = bytes(header) + struct.pack(f"<{len(pixels)}I", *pixels)
	return b"JMXVDDJ 1000" + struct.pack("<II", len(dds), 0) + dds


class ArtOwnerTest(unittest.TestCase):
	def test_guide_art_and_buttons_share_no_path(self):
		# One producer per published image: the v1.188 buttons own the
		# reverse return icons, and a path in both lists would let the
		# step that runs last decide the bytes.
		self.assertEqual(set(guide.ART) & set(buttons.BUTTONS), set())


class TablesTest(unittest.TestCase):
	"""
	================
	TablesTest
	================
	"""

	def test_pages_keep_every_column(self):
		pages = guide.read_pages([["1", "field", "46x113", "17x26", "128x128", "4x4", "0"],
			["0", "off", "1x1", "1x1", "1x1", "1x1", "9"]])
		self.assertEqual(pages, {"field": {"origin": [46, 113], "extent": [17, 26], "tile": [128, 128],
			"perTile": [4, 4], "id": 0}})

	def test_monster_columns_tier_and_continent(self):
		areas = [{"codename": "FIELD_CHINA", "name": "China  Lv.1~20"}]
		monsters = guide.read_monsters([MONSTER_MAIN, monster_row("MOB_A", tier="91", continent="3")],
			{"SN_MOB_A": "Ghost"}, {"MOB_A": 7}, areas)
		group, mob = monsters
		self.assertEqual((group["name"], group["main"], group["level"]), ("China  Lv.1~20", True, None))
		self.assertEqual((mob["name"], mob["level"], mob["tier"], mob["continent"]), ("Ghost", 7, 91, 3))
		self.assertEqual(mob["order"], [10101, 10801, 10101])
		self.assertEqual((mob["world"], mob["sector"], mob["point"]), ([3212, 559], [166, 97], [12, 47]))
		self.assertEqual(mob["colours"], [0x80000005, 0x80000006])

	def test_area_link_instances_and_text(self):
		row = ["1", "k", "FIELD_X", "UIIT_STT_X", "field", "sub", "1101", "1108", "1101", "1", "91001",
			"INS_A,INS_B", "101", "1"]
		area, = guide.read_areas([row, ["0"] + row[1:]], {"UIIT_STT_X": "Field X"})
		self.assertEqual((area["name"], area["linkKind"], area["link"]), ("Field X", 1, 91001))
		self.assertEqual((area["instances"], area["tier"], area["continent"]), (["INS_A", "INS_B"], 101, 1))

	def test_region_lines_pair_with_rows_in_order(self):
		first, second = guide.read_monsters([monster_row("MOB_A", "p1"), monster_row("MOB_A", "p2")], {}, {}, [])
		guide.attach_regions([first, second], [["MOB_A", "1x2,3x4"], ["MOB_A", "5x6"], ["MOB_ORPHAN", "7x8"]])
		self.assertEqual(first["tiles"], [[1, 2], [3, 4]])
		self.assertEqual(second["tiles"], [[5, 6]])

	def test_english_text_skips_absent_translations(self):
		rows = [["1", "5", "KEY_A"] + ["x"] * 6 + ["Alpha"], ["1", "6", "KEY_B"] + ["x"] * 6 + ["0"],
			["0", "7", "KEY_C"] + ["x"] * 6 + ["Gamma"]]
		self.assertEqual(guide.english_text(rows), {"KEY_A": "Alpha"})


class MaskTest(unittest.TestCase):
	"""
	================
	MaskTest

	sub_f46270's test: a pixel in the set is kept; 0, 0xffffff and every
	other colour become transparent.
	================
	"""

	def test_mask_keeps_only_the_set(self):
		pixels = [0x80000005, 0x80000006, 0x80000007, 0x00ffffff, 0x00000000, 0x80000005]
		with tempfile.TemporaryDirectory() as folder:
			target = Path(folder) / "m.png"
			guide.write_mask(ddj(3, 2, pixels), [0x80000005, 0x00ffffff], target)
			mask = Image.open(target)
			self.assertEqual(mask.mode, "RGBA")
			alpha = list(mask.split()[-1].tobytes())
		self.assertEqual(alpha, [255, 0, 0, 0, 0, 255])

	def test_compressed_tile_is_refused(self):
		raw = bytearray(ddj(1, 1, [0]))
		offset = raw.index(b"DDS ")
		struct.pack_into("<I", raw, offset + 88, 0)
		with self.assertRaises(SystemExit):
			guide.tile_pixels(bytes(raw))


if __name__ == "__main__":
	unittest.main()
