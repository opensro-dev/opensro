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
import alexandria_npcs as characters  # noqa: E402
import later_fixtures  # noqa: E402
import json
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
# pk2
#
# A test archive holding files at nested paths ({"a/b.txt": bytes}),
# the shape test_later_world_import.py builds with the same writer.
# ================
def pk2(files):
	from test_client_preparation import ArchiveBuilder
	builder = ArchiveBuilder()
	tree = {}
	for path, raw in files.items():
		node = tree
		*folders, name = path.split("/")
		for folder in folders:
			node = node.setdefault(folder, {})
		node[name] = raw

	def emit(node):
		offset = builder.folder([])
		entries = []
		for name, child in node.items():
			if isinstance(child, dict):
				entries.append((1, name.encode("ascii"), emit(child), 0))
			else:
				entries.append((2, name.encode("ascii"), *builder.add_payload(child)))
		builder.blocks[offset] = entries
		return offset

	emit(tree)
	return builder.build()


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
	# test_no_staged_parameter_is_below_the_none_value
	#
	# The item loaders refuse a parameter below -1; the extraction's
	# two damaged unused slots (URUCHI p5 -257, IVY p18 -65537) stage
	# as -1, and every other slot stays as extracted.
	# ================
	def test_no_staged_parameter_is_below_the_none_value(self):
		raw = {entry["codename"]: entry["row"]
			for entry in json.loads(rigid.ROWS_FILE.read_text(encoding="utf-8"))["rows"]}
		rows = {row[rigid.ITEM_CODE]: row for row in rigid.scroll_rows()}
		self.assertEqual(raw["ITEM_RIGID_URUCHI_L2_SUMMON_SCROLL"][126], "-257")
		self.assertEqual(raw["ITEM_RIGID_IVY_L2_SUMMON_SCROLL"][152], "-65537")
		for code, row in rows.items():
			for k in range(rigid.PARAM_SLOTS):
				self.assertGreaterEqual(int(row[rigid.PARAM_FIRST + 2 * k]), -1, f"{code} parameter {k + 1}")
			changed = [i for i, (a, b) in enumerate(zip(raw[code], row)) if a != b]
			expected = {"ITEM_RIGID_URUCHI_L2_SUMMON_SCROLL": [126], "ITEM_RIGID_IVY_L2_SUMMON_SCROLL": [152]}
			self.assertEqual(changed, expected.get(code, []), code)

	# ================
	# test_an_unused_slot_keeps_a_real_value
	#
	# Retail unused slots hold 0, 1, 50 and 100; only a negative
	# value other than -1 is damage.
	# ================
	def test_an_unused_slot_keeps_a_real_value(self):
		row = ["0"] * rigid.ITEM_COLUMNS
		for k in range(rigid.PARAM_SLOTS):
			row[rigid.PARAM_FIRST + 2 * k], row[rigid.PARAM_FIRST + 2 * k + 1] = "-1", "xxx"
		row[120], row[122], row[128], row[129] = "50", "-9", "-3", "MOB_X"
		fixed = rigid.unused_parameters(row)
		self.assertEqual((fixed[120], fixed[122], fixed[128]), ("50", "-1", "-3"))

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
# RigidKindNamesTest
#
# The three kinds v1.696 names in Korean only take their English from
# this layer's staged rows, never from the retail completion catalog:
# a catalog key the tree lacks failed every build without the v1.696
# client (#4466).
# ================
class RigidKindNamesTest(unittest.TestCase):
	def test_a_placeholder_english_cell_takes_the_layer_name(self):
		texts = [["1", "SN_MOB_AM_IVY_L2", "금강급", "", "", "", "", "", "0", "", ""],
			["1", "SN_MOB_EU_KERBEROS_L2", "태백급", "", "", "", "", "", "Shipped", "", ""],
			["1", "SN_OTHER", "x", "", "", "", "", "", "", "", ""]]
		rigid.later_world_import.fill_layer_english(texts, rigid.KIND_NAMES)
		english = characters.TEXT_ENGLISH_COLUMN
		self.assertEqual(texts[0][english], rigid.KIND_NAMES["SN_MOB_AM_IVY_L2"])
		self.assertEqual(texts[1][english], "Shipped", "a shipped translation stays")
		self.assertEqual(texts[2][english], "", "a symbol the layer does not name is untouched")

	def test_every_name_is_a_staged_kind_and_none_is_in_the_retail_catalog(self):
		self.assertEqual(set(rigid.KIND_NAMES), {"SN_" + code for code in rigid.KINDS} - {"SN_MOB_TQ_BLACKSNAKE_L3"})
		catalog = json.loads((Path(rigid.__file__).parent / "build" / "shared" / "englishCompletions" / "textdataname.json")
			.read_text(encoding="utf-8"))
		self.assertEqual(set(rigid.KIND_NAMES) & set(catalog), set())
		for name in rigid.KIND_NAMES.values():
			self.assertRegex(name, r"^[A-Za-z][A-Za-z -]+$")


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
		later_fixtures.write_sharded(native_text, "itemdata.txt", {"itemdata_5000.txt": []})
		model = native / "Data_extracted" / "res" / "item" / "etc" / "drop_scroll.bsr"
		model.parent.mkdir(parents=True)
		model.write_bytes(b"JMXVRES 0109" + bytes(256))
		icon = native / "Media_extracted" / "icon" / "item" / "etc" / "e050729_summon_scroll.ddj"
		icon.parent.mkdir(parents=True)
		icon.write_bytes(b"JMXVDDJ 0101")
		text_root = out / rigid.later_world_import.TEXT_ROOT
		text_root.mkdir(parents=True)
		(text_root / "itemdata.txt").write_bytes(later_fixtures.index("itemdata_5000.txt"))
		(text_root / "textdataname.txt").write_bytes(utf16([["1", "SN_NATIVE", "x", "", "", "Native", "", "", "", "", ""]]))
		return native, out, text_root

	# ================
	# archives
	#
	# The empty archives add_rigid builds from the layer's declared
	# set: the staging runs against whatever the declaration names,
	# so a declaration that drops an archive fails here (it dropped
	# Data once, and the real build died at the model closure).
	# ================
	def archives(self):
		return {name: (b"", {}) for name in rigid.ARCHIVES}

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
			summary = rigid.add_items(native, out, self.archives(), {})
			shard_rows = rigid.tables.table_rows((text_root / rigid.ITEM_SHARD).read_bytes())
			self.assertEqual(len(shard_rows), 8)
			ids = [int(row[rigid.ITEM_ID]) for row in shard_rows]
			self.assertEqual(len(set(ids)), 8)
			self.assertEqual({row[rigid.ITEM_CODE] for row in shard_rows}, set(rigid.SCROLLS))
			manifest = [row[0] for row in rigid.tables.table_rows((text_root / "itemdata.txt").read_bytes()) if row]
			self.assertEqual(manifest, [later_fixtures.archive_case("itemdata_5000.txt"), rigid.ITEM_SHARD])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			for symbol in summary["itemSymbols"]:
				self.assertIn(symbol, names)
			self.assertIn("SN_NATIVE", names)

			again = rigid.add_items(native, out, self.archives(), summary)
			self.assertEqual(again["items"], summary["items"])
			manifest = [row[0] for row in rigid.tables.table_rows((text_root / "itemdata.txt").read_bytes()) if row]
			self.assertEqual(manifest.count(rigid.ITEM_SHARD), 1)

			rigid.reap_items(out, again)
			self.assertFalse((text_root / rigid.ITEM_SHARD).exists())
			manifest = [row[0] for row in rigid.tables.table_rows((text_root / "itemdata.txt").read_bytes()) if row]
			self.assertEqual(manifest, [later_fixtures.archive_case("itemdata_5000.txt")])
			names = (text_root / "textdataname.txt").read_bytes().decode("utf-16")
			self.assertIn("SN_NATIVE", names)
			for symbol in summary["itemSymbols"]:
				self.assertNotIn(symbol, names)

	# ================
	# test_the_item_closure_resolves_the_drop_model_from_the_data_archive
	#
	# The scrolls' drop model is a Data record, not a Media one: with
	# the model absent from the native tree the closure must take it
	# from the layer's Data archive, enrol it in the marker and reap
	# it (T5145: declared as Media alone, the layer died here).
	# ================
	def test_the_item_closure_resolves_the_drop_model_from_the_data_archive(self):
		with tempfile.TemporaryDirectory() as temp, \
				mock.patch.object(rigid.item_import, "resource_dependencies",
					lambda requests: [[] for _ in requests]):
			native, out, text_root = self.fixture(Path(temp))
			(native / "Data_extracted" / "res" / "item" / "etc" / "drop_scroll.bsr").unlink()
			data = pk2({"res/item/etc/drop_scroll.bsr": b"JMXVRES 0109" + bytes(64)})
			index = {rigid.item_import.normalized_path(entry.path): entry
				for entry in rigid.sro_pk2.read_directory(data)}
			archives = self.archives()
			archives["Data"] = (data, index)
			summary = rigid.add_items(native, out, archives, {})
			model = out / "Data_extracted" / "res" / "item" / "etc" / "drop_scroll.bsr"
			self.assertTrue(model.exists())
			self.assertEqual([record["path"] for record in summary["itemResources"]],
				["res/item/etc/drop_scroll.bsr"])
			rigid.reap_items(out, summary)
			self.assertFalse(model.exists())


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
