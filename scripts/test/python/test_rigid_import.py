"""
===========================================================================

test_rigid_import.py - the Rigid L2 summon scrolls' staging (#3785)

Pins the committed extraction the import stages (the monster link,
family type, required level and cooltime the server's use path
reads), the authored name rows (the only part of the layer no game
source carries), and the item shard's stage/reap round trip.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import rigid_import as rigid


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + "".join("\t".join(row) + "\r\n" for row in rows).encode("utf-16-le")


# ================
# RigidScrollRowsTest
# ================
class RigidScrollRowsTest(unittest.TestCase):
	# ================
	# test_every_scroll_row_carries_its_use_path_inputs
	#
	# The v1.188 use path reads the monster from parameter 1's
	# description and the 600000 ms family cooltime from parameter
	# 4; the family type and required level ride the common fields.
	# ================
	def test_every_scroll_row_carries_its_use_path_inputs(self):
		rows = {row[rigid.ITEM_CODE]: row for row in rigid.scroll_rows()}
		self.assertEqual(set(rows), set(rigid.SCROLLS))
		for code, row in rows.items():
			monster, _display = rigid.SCROLLS[code]
			self.assertEqual(len(row), 160)
			self.assertEqual(row[9:13], ["3", "3", "3", "23"])
			self.assertEqual(row[rigid.ITEM_REQ_LEVEL], "90")
			self.assertEqual(row[118], "-1")
			self.assertEqual(row[119], monster)
			self.assertEqual(row[124], "600000")
			self.assertEqual(row[125], "COOLTIME:0x000000F1")
			self.assertEqual(row[53], "item\\etc\\drop_scroll.bsr")
			self.assertEqual(row[54], "item\\etc\\e050729_summon_scroll.ddj")

	# ================
	# test_the_uruchi_name_symbol_keeps_the_extraction_spelling
	#
	# The ISRO-R row names SN_..._SUMNON_SCROLL (one M short); the
	# staged texts follow the row, so the name resolves.
	# ================
	def test_the_uruchi_name_symbol_keeps_the_extraction_spelling(self):
		rows = {row[rigid.ITEM_CODE]: row for row in rigid.scroll_rows()}
		self.assertEqual(rows["ITEM_RIGID_URUCHI_L2_SUMMON_SCROLL"][rigid.ITEM_NAME],
			"SN_ITEM_RIGID_URUCHI_L2_SUMNON_SCROLL")


# ================
# RigidAuthoredTextsTest
# ================
class RigidAuthoredTextsTest(unittest.TestCase):
	# ================
	# test_texts_cover_every_symbol_the_rows_name
	# ================
	def test_texts_cover_every_symbol_the_rows_name(self):
		rows = rigid.scroll_rows()
		texts = rigid.authored_texts(rows)
		named = [row[rigid.ITEM_NAME] for row in rows] + [row[rigid.ITEM_DESC] for row in rows]
		self.assertEqual(sorted(named), sorted(row[1] for row in texts))
		by_symbol = {row[1]: row for row in texts}
		for row in rows:
			monster, display = rigid.SCROLLS[row[rigid.ITEM_CODE]]
			self.assertEqual(by_symbol[row[rigid.ITEM_NAME]][5], rigid.ITEM_NAME_TEXT)
			self.assertIn(display, by_symbol[row[rigid.ITEM_DESC]][5])


# ================
# RigidItemStagingTest
# ================
class RigidItemStagingTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A native extraction holding the scrolls' icon and drop model
	# (the shipped party summon scroll's own assets) and an output
	# textdata seeded as the earlier build stages leave it.
	# ================
	def fixture(self, base):
		native, out = base / "native", base / "out"
		native_text = native / rigid.later_world_import.TEXT_ROOT
		native_text.mkdir(parents=True)
		(native_text / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(native_text / "itemdata_5000.txt").write_bytes(utf16([]))
		model = native / "Data_extracted" / "res" / "item" / "etc" / "drop_scroll.bsr"
		model.parent.mkdir(parents=True)
		model.write_bytes(b"JMXVRES 0109" + bytes(256))
		icon = native / "Media_extracted" / "icon" / "item" / "etc" / "e050729_summon_scroll.ddj"
		icon.parent.mkdir(parents=True)
		icon.write_bytes(b"JMXVDDJ 0101")
		text_root = out / rigid.later_world_import.TEXT_ROOT
		text_root.mkdir(parents=True)
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", "SN_NATIVE", "x", "", "", "Native", "", "", "", "", ""]]))
		return native, out, text_root

	# ================
	# test_stage_then_reap_round_trip
	#
	# The asset parser behind the icon/model closure is the cap110
	# import's own tested boundary; here it reports no dependencies,
	# so the staging and reap are what is measured.
	# ================
	def test_stage_then_reap_round_trip(self):
		with tempfile.TemporaryDirectory() as temp, \
				mock.patch.object(rigid.item_import, "resource_dependencies",
					lambda requests: [[] for _ in requests]):
			native, out, text_root = self.fixture(Path(temp))
			summary = rigid.add_items(native, out, {"Data": (b"", {}), "Media": (b"", {})}, {})
			shard_rows = rigid.tables.table_rows((text_root / rigid.ITEM_SHARD).read_bytes())
			self.assertEqual(len(shard_rows), 8)
			ids = [int(row[rigid.ITEM_ID]) for row in shard_rows]
			self.assertEqual(len(set(ids)), 8)
			self.assertEqual({row[rigid.ITEM_CODE] for row in shard_rows}, set(rigid.SCROLLS))
			manifest = [row[0] for row in rigid.tables.table_rows((text_root / "itemdata.txt").read_bytes()) if row]
			self.assertEqual(manifest, ["itemdata_5000.txt", rigid.ITEM_SHARD])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			for symbol in summary["itemSymbols"]:
				self.assertIn(symbol, names)
			self.assertIn("SN_NATIVE", names)

			again = rigid.add_items(native, out, {"Data": (b"", {}), "Media": (b"", {})}, summary)
			self.assertEqual(again["items"], summary["items"])
			manifest = [row[0] for row in rigid.tables.table_rows((text_root / "itemdata.txt").read_bytes()) if row]
			self.assertEqual(manifest.count(rigid.ITEM_SHARD), 1)

			rigid.reap_items(out, again)
			self.assertFalse((text_root / rigid.ITEM_SHARD).exists())
			manifest = [row[0] for row in rigid.tables.table_rows((text_root / "itemdata.txt").read_bytes()) if row]
			self.assertEqual(manifest, ["itemdata_5000.txt"])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("SN_NATIVE", names)
			for symbol in summary["itemSymbols"]:
				self.assertNotIn(symbol, names)


# ================
# RigidOffPathTest
# ================
class RigidOffPathTest(unittest.TestCase):
	# ================
	# test_without_the_later_root_nothing_is_staged
	# ================
	def test_without_the_later_root_nothing_is_staged(self):
		with tempfile.TemporaryDirectory() as temp:
			native, out = Path(temp) / "native", Path(temp) / "out"
			native.mkdir()
			text_root = out / rigid.later_world_import.TEXT_ROOT
			text_root.mkdir(parents=True)
			summary = rigid.add_rigid(native, out, env={})
			self.assertFalse(summary["enabled"])
			self.assertFalse((text_root / rigid.ITEM_SHARD).exists())
			self.assertFalse((text_root / rigid.CHARACTER_SHARD).exists())


if __name__ == "__main__":
	unittest.main()
