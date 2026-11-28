"""
===========================================================================
test_magic_pop_import.py - the v1.696 Magic POP pool import's contract.

On by default (SRO_MAGIC_POP_V1696 unset), the import writes the v1.696
pool's set 1 and 2 rows with each reward rewritten to this tree's id by
codename, the machine map and the category tree verbatim, and stages the
event gear the tree lacks in its own shard with the socket parameter
reset, and the lose set's expendables with their parameters verbatim.
Degree 11 rows and the WITHHELD prizes leave the pool and are not
imported; a prize with no import lane, or an unknown reward id, refuses.
Off (or no v1.696 client) reaps every output and leaves the tree as the
earlier steps wrote it.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import cap110_item_import as shared  # noqa: E402
import cap110_items as items  # noqa: E402
import jupiter_import as jupiter  # noqa: E402
import magic_pop_import as pop  # noqa: E402
import mall_avatar_import as avatars  # noqa: E402
import sro_pk2  # noqa: E402
from test_nasrun_import import archive_bytes, utf16  # noqa: E402

# Native ids the output tree holds (v1.150 rows) and the later ids of the
# fixture's v1.696 rows; the import must never keep a later id.
NATIVE_BOX = 10129
NATIVE_GEAR = 4140
LATER_EVENT_SWORD = 25833
LATER_EVENT_BOW = 25834
LATER_DEGREE11 = 25859
LATER_SP_SCROLL = 33239
LATER_FLOWER = 41182
LATER_PET2 = 41130
LATER_POTION = 70001


# ================
# gear_row
#
# One v1.696 event Seal row: a weapon of the given class, the socket
# parameter authored in its twentieth pair as the later client does.
# ================
def gear_row(code, identity, item_class, sockets="3"):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY], row[items.DESCRIPTION_KEY] = "SN_" + code.replace("ITEM_EVENT_", "ITEM_"), "xxx"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = ["3", "1", "6", "2"]
	row[items.RACE] = "0"
	row[items.CLASS] = str(item_class)
	row[items.MODEL] = "item\\china\\weapon\\sword_01.bsr"
	row[items.DROP_MODEL] = "item\\etc\\drop_ch_equip_rare.bsr"
	row[items.ICON] = "item\\china\\weapon\\sword_01.ddj"
	for column in range(items.PARAM_FIRST, items.PARAM_END, items.PARAM_STRIDE):
		row[column], row[column + 1] = "-1", items.UNUSED
	row[pop.SOCKET_PARAM], row[pop.SOCKET_PARAM + 1] = sockets, pop.SOCKET_LABEL
	return row


# ================
# plain_row
# ================
def plain_row(code, identity, kind=("3", "3", "1", "1")):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY], row[items.DESCRIPTION_KEY] = "SN_" + code, "xxx"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = list(kind)
	return row


# ================
# flower_row
#
# The v1.696 summon flower: an expendable whose first parameter names the
# structure it places.
# ================
def flower_row():
	row = plain_row("ITEM_ETC_SUMMON_FLOWER_01", LATER_FLOWER, ("3", "3", "3", "20"))
	row[items.PARAM_FIRST], row[items.PARAM_FIRST + 1] = "0", "STRUCTURE_SUMMON_FLOWER_01"
	return row


# ================
# pool_row
#
# One v1.696 gachaitemset row: twenty columns, the level cap at 16.
# ================
def pool_row(set_id, reward, weight, entry, min_level=1, enabled="1"):
	return [enabled, str(set_id), str(reward), str(weight), "1", str(entry), "0", "1",
		"[BIIV]<M:str,1,3><M:int,1,3><O:3>", "0", "xxx", "0", "xxx", "0", "xxx",
		str(min_level), str(weight), str(weight), str(weight), str(weight)]


# ================
# text_row
# ================
def text_row(key, english):
	row = ["1", key] + ["0"] * 13
	row[8] = english
	return row


# ================
# tree_row
# ================
def tree_row(group, sub):
	return ["1", "3", "1", "6", "2", "100", group, "101", sub, "15"]


# ================
# MagicPopImportTest
# ================
class MagicPopImportTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 Media.pk2 with the pool, its items and
	# tables, and an output tree holding the native rows, the machine NPC
	# and the text tables. pool_rows and later_items override the defaults.
	# ================
	def fixture(self, base, pool_rows=None, later_items=None, machine=pop.MACHINE_CODENAME):
		later, native, output = base / "later", base / "native", base / "output"
		text_root = output / pop.TEXT_ROOT
		text_root.mkdir(parents=True)
		later.mkdir()
		(native / pop.TEXT_ROOT).mkdir(parents=True)
		if later_items is None:
			later_items = [
				gear_row("ITEM_EVENT_CH_SWORD_01_A_RARE", LATER_EVENT_SWORD, 1),
				gear_row("ITEM_EVENT_CH_BOW_10_A_RARE", LATER_EVENT_BOW, 28, sockets="0"),
				gear_row("ITEM_EVENT_CH_SWORD_11_A_RARE", LATER_DEGREE11, 31),
				plain_row("ITEM_EVENT_SKILLPOINT_SCROLL", LATER_SP_SCROLL, ("3", "3", "3", "12")),
				flower_row(),
				plain_row("ITEM_EVENT_PET2_SCROLL_ASS_DINOSAUR_A", LATER_PET2, ("3", "2", "1", "3")),
				# Native in this tree under another id: the pool takes the native id.
				plain_row("ITEM_ETC_E060517_MON_GENERATION_BOX", 99999, ("3", "3", "15", "1")),
				plain_row("ITEM_CH_TBLADE_07_A_RARE", 88888, ("3", "1", "6", "4")),
			]
		if pool_rows is None:
			pool_rows = [
				pool_row(1, LATER_EVENT_SWORD, 3000, 1),
				pool_row(1, LATER_EVENT_BOW, 8, 2, min_level=91),
				pool_row(1, LATER_DEGREE11, 5, 3, min_level=101),
				pool_row(1, LATER_PET2, 300, 4),
				pool_row(1, 88888, 30, 5),
				pool_row(2, LATER_SP_SCROLL, 100, 6),
				pool_row(2, LATER_FLOWER, 1300, 7),
				pool_row(2, 99999, 50, 8),
				pool_row(3, LATER_EVENT_SWORD, 3000, 9),
				pool_row(1, LATER_EVENT_SWORD, 3000, 10, enabled="0"),
			]
		names = [text_row("SN_ITEM_CH_BOW_10_A_RARE", "Bow ten"),
			text_row("SN_ITEM_EVENT_SKILLPOINT_SCROLL", "Skill point scroll"),
			text_row("SN_ITEM_ETC_SUMMON_FLOWER_01", "Summon flower")]
		self.media = archive_bytes({
			"itemdata.txt": utf16([["itemdata_25000.txt"]]),
			"itemdata_25000.txt": utf16(later_items),
			"textdata_equip&skill.txt": utf16(names),
			"gachaitemset.txt": utf16(pool_rows),
			"gachanpcmap.txt": utf16([["1", "9251", "1", "2", "3", "4"]]),
			"refgachatreeforclientuidata.txt": utf16([
				tree_row("UIIT_CTL_WK_CH_WEAPON", "UIIT_CTL_WK_CH_WEAPON_SWORD"),
				tree_row("UIIT_CTL_WK_CH_WEAPON", "UIIT_CTL_WK_CH_WEAPON_BOW"),
			]),
			"textuisystem.txt": utf16([["textuisystem_100.txt"]]),
			# The v1.696 UI layout: id, key at 2, English at 9.
			"textuisystem_100.txt": utf16([
				["1", "77", "UIIT_CTL_WK_CH_WEAPON_BOW", "0", "0", "0", "0", "0", "0", "Bow"],
			]),
		})
		native_items = [plain_row("ITEM_CH_TBLADE_07_A_RARE", NATIVE_GEAR, ("3", "1", "6", "4")),
			plain_row("ITEM_ETC_E060517_MON_GENERATION_BOX", NATIVE_BOX, ("3", "3", "15", "1"))]
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16(native_items))
		machine_row = ["1", str(pop.MACHINE_ID), machine]
		(text_root / "characterdata.txt").write_bytes(utf16([["characterdata_9000.txt"]]))
		(text_root / "characterdata_9000.txt").write_bytes(utf16([machine_row]))
		native_names = utf16([text_row("SN_ITEM_CH_SWORD_01_A_RARE", "Sword one")])
		(text_root / pop.NAME_TABLE).write_bytes(native_names)
		(native / pop.TEXT_ROOT / pop.NAME_TABLE).write_bytes(native_names)
		ui = utf16([["1", "UIIT_CTL_WK_CH_WEAPON", "Weapon", "", "", "Weapon", "Weapon", "Weapon", "Weapon", "Weapon"],
			["1", "UIIT_CTL_WK_CH_WEAPON_SWORD", "Sword", "", "", "Sword", "Sword", "Sword", "Sword", "Sword"]])
		(text_root / pop.UI_TABLE).write_bytes(ui)
		self.before = {path.name: path.read_bytes() for path in text_root.iterdir()}
		return later, native, output, text_root

	# ================
	# run_import
	# ================
	def run_import(self, later, native, output, env):
		data = self.media
		index = {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)}
		self.closure_rows = []

		def open_archives(stack, root, source):
			self.assertIs(source, avatars.V1696_SOURCE)
			return {"Media": (data, index)}, {"Media": "fixture"}

		self.closure_worn = {}

		def copy_asset_closure(rows, native_root, output_root, archives, worn=True):
			self.closure_rows += [row[items.CODENAME] for row in rows]
			self.closure_worn.update({row[items.CODENAME]: worn for row in rows})
			path = "icon/item/china/weapon/event_fixture.ddj"
			target = output_root / shared.FOLDERS["Media"] / path
			target.parent.mkdir(parents=True, exist_ok=True)
			target.write_bytes(b"icon")
			return [{"archive": "Media", "path": path, "source": "later", "sha256": "x"}]

		with mock.patch.object(avatars, "open_archives", open_archives), \
				mock.patch.object(jupiter, "require_v1696_pin", lambda root, names: None), \
				mock.patch.object(shared, "copy_asset_closure", copy_asset_closure):
			return pop.add_magic_pop(native, output, later, env=env)

	# ================
	# pool
	#
	# The written pool as (set, reward id, entry) rows.
	# ================
	def pool(self, text_root):
		rows = [line.split("\t") for line in
			(text_root / pop.POOL_TABLE).read_bytes().decode("utf-16").splitlines() if line.strip()]
		for row in rows:
			self.assertEqual(len(row), pop.POOL_COLUMNS)
		return rows

	# ================
	# test_default_is_on
	# ================
	def test_default_is_on(self):
		self.assertTrue(pop.flag_enabled({}))
		self.assertTrue(pop.flag_enabled({pop.FLAG: "on"}))
		for value in ("0", "off", "false", "no", " OFF "):
			self.assertFalse(pop.flag_enabled({pop.FLAG: value}))

	# ================
	# test_pool_keeps_normal_sets_and_remaps_by_codename
	# ================
	def test_pool_keeps_normal_sets_and_remaps_by_codename(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, {})
			self.assertTrue(summary["enabled"])
			rows = self.pool(text_root)
			by_entry = {row[pop.POOL_ENTRY]: row for row in rows}
			# Sets 3/4, disabled rows, degree 11 and withheld prizes leave.
			self.assertEqual(sorted(by_entry), ["1", "2", "5", "6", "7", "8"])
			self.assertEqual({row[pop.POOL_SET] for row in rows}, {"1", "2"})
			# Native items take the tree's id, never the later one.
			self.assertEqual(by_entry["5"][pop.POOL_REWARD], str(NATIVE_GEAR))
			self.assertEqual(by_entry["8"][pop.POOL_REWARD], str(NATIVE_BOX))
			# Imported items take their fresh ids.
			self.assertEqual(by_entry["1"][pop.POOL_REWARD], str(summary["items"]["ITEM_EVENT_CH_SWORD_01_A_RARE"]))
			self.assertEqual(by_entry["2"][pop.POOL_REWARD], str(summary["items"]["ITEM_EVENT_CH_BOW_10_A_RARE"]))
			# Every other column is the later row's.
			self.assertEqual(by_entry["2"][pop.POOL_MIN_LEVEL], "91")
			self.assertEqual(by_entry["1"][8], "[BIIV]<M:str,1,3><M:int,1,3><O:3>")
			self.assertEqual(summary["pool"]["perSet"], {"1": 3, "2": 3})
			# The lose set keeps every native prize (owner, 2026-11-28).
			self.assertEqual(by_entry["6"][pop.POOL_REWARD], str(summary["items"]["ITEM_EVENT_SKILLPOINT_SCROLL"]))
			self.assertEqual(by_entry["7"][pop.POOL_REWARD], str(summary["items"]["ITEM_ETC_SUMMON_FLOWER_01"]))
			self.assertEqual(summary["pool"]["dropped"], {
				"dropped: PET2 fellow pet scroll": 1, "dropped: degree 11": 1,
			})

	# ================
	# test_imports_missing_gear_and_lose_expendables
	#
	# Missing gear is staged with its socket parameter reset and a worn
	# mesh; a missing lose-set expendable keeps every parameter and is
	# carried (icon and drop model only).
	# ================
	def test_imports_missing_gear_and_lose_expendables(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, {})
			gear = ["ITEM_EVENT_CH_BOW_10_A_RARE", "ITEM_EVENT_CH_SWORD_01_A_RARE"]
			carried = ["ITEM_ETC_SUMMON_FLOWER_01", "ITEM_EVENT_SKILLPOINT_SCROLL"]
			self.assertEqual(sorted(summary["items"]), sorted(gear + carried))
			self.assertEqual(sorted(self.closure_rows), sorted(summary["items"]))
			self.assertEqual(self.closure_worn, {**{code: True for code in gear}, **{code: False for code in carried}})
			for identity in summary["items"].values():
				self.assertNotIn(identity, (LATER_EVENT_SWORD, LATER_EVENT_BOW, LATER_SP_SCROLL, LATER_FLOWER, NATIVE_GEAR, NATIVE_BOX))
			shard = {row[items.CODENAME]: row for row in (line.split("\t") for line in
				(text_root / pop.ITEM_SHARD).read_bytes().decode("utf-16").splitlines() if line.strip())}
			self.assertEqual(sorted(shard), sorted(gear + carried))
			for code in gear:
				self.assertEqual(shard[code][pop.SOCKET_PARAM:pop.SOCKET_PARAM + 2], ["-1", items.UNUSED])
			for code in gear + carried:
				self.assertEqual(shard[code][items.PLACEHOLDER_NAME], items.UNUSED)
			# A carried prize keeps the later row's parameters as authored.
			self.assertEqual(shard["ITEM_ETC_SUMMON_FLOWER_01"][items.PARAM_FIRST + 1], "STRUCTURE_SUMMON_FLOWER_01")
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertIn(pop.ITEM_SHARD, manifest)
			names = (text_root / pop.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertIn("Bow ten", names)
			self.assertEqual(summary["texts"], ["SN_ITEM_CH_BOW_10_A_RARE", "SN_ITEM_ETC_SUMMON_FLOWER_01",
				"SN_ITEM_EVENT_SKILLPOINT_SCROLL"])
			again = self.run_import(later, native, output, {})
			self.assertEqual(again["items"], summary["items"])
			self.assertEqual((text_root / pop.NAME_TABLE).read_bytes().decode("utf-16").count("Bow ten"), 1)
			self.assertEqual((text_root / "itemdata.txt").read_bytes().decode("utf-16").count(pop.ITEM_SHARD), 1)

	# ================
	# test_tables_and_labels
	# ================
	def test_tables_and_labels(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, {})
			npc = (text_root / pop.NPC_MAP_TABLE).read_bytes().decode("utf-16")
			self.assertEqual(npc.strip(), "1\t9251\t1\t2\t3\t4")
			tree = [line for line in (text_root / pop.TREE_TABLE).read_bytes().decode("utf-16").splitlines() if line]
			self.assertEqual(len(tree), 2)
			# The one label the tree's UI table lacked, in the later English.
			self.assertEqual(summary["uiTexts"], ["UIIT_CTL_WK_CH_WEAPON_BOW"])
			ui = (text_root / pop.UI_TABLE).read_bytes().decode("utf-16")
			self.assertIn("UIIT_CTL_WK_CH_WEAPON_BOW\tBow", ui)
			self.assertEqual(ui.count("UIIT_CTL_WK_CH_WEAPON\t"), 1)
			# The native pool table is never written.
			self.assertFalse((text_root / "gachaitemset.txt").exists())

	# ================
	# test_off_reaps_every_output
	# ================
	def test_off_reaps_every_output(self):
		for env in ({pop.FLAG: "off"}, {pop.ROOT_ENV: ""}):
			with tempfile.TemporaryDirectory() as root:
				later, native, output, text_root = self.fixture(Path(root))
				summary = self.run_import(later, native, output, {})
				icon = output / shared.FOLDERS["Media"] / summary["assets"][0]["path"]
				self.assertTrue(icon.exists())
				# The second case names no client: the root argument is unset too.
				result = pop.add_magic_pop(native, output, later if pop.FLAG in env else None, env=env)
				self.assertFalse(result["enabled"])
				after = {path.name: path.read_bytes() for path in text_root.iterdir()}
				self.assertEqual(after, self.before)
				self.assertFalse((output / pop.MARKER).exists())
				self.assertFalse(icon.exists())

	# ================
	# test_unknown_reward_refuses
	# ================
	def test_unknown_reward_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, _ = self.fixture(Path(root), pool_rows=[
				pool_row(1, LATER_EVENT_SWORD, 3000, 1), pool_row(2, 99999, 50, 2), pool_row(1, 123456, 5, 3)])
			with self.assertRaisesRegex(ValueError, "unknown item 123456"):
				self.run_import(later, native, output, {})

	# ================
	# test_prize_without_lane_refuses
	#
	# A missing prize that is neither withheld, degree 1-10 equipment, nor
	# a lose-set expendable: an expendable in the win set, and a lose-set
	# prize that is no expendable.
	# ================
	def test_prize_without_lane_refuses(self):
		for set_id, kind, reason in ((1, ("3", "3", "1", "1"), "not degree 1-"), (2, ("3", "2", "1", "3"), "not an expendable")):
			with self.subTest(set_id=set_id), tempfile.TemporaryDirectory() as root:
				later_items = [gear_row("ITEM_EVENT_CH_SWORD_01_A_RARE", LATER_EVENT_SWORD, 1),
					plain_row("ITEM_ETC_UNKNOWN_SCROLL", LATER_POTION, kind),
					plain_row("ITEM_ETC_E060517_MON_GENERATION_BOX", 99999, ("3", "3", "15", "1"))]
				later, native, output, _ = self.fixture(Path(root), later_items=later_items, pool_rows=[
					pool_row(1, LATER_EVENT_SWORD, 3000, 1), pool_row(2, 99999, 50, 2), pool_row(set_id, LATER_POTION, 50, 3)])
				with self.assertRaisesRegex(ValueError, "ITEM_ETC_UNKNOWN_SCROLL has no import lane \\(" + reason):
					self.run_import(later, native, output, {})

	# ================
	# test_unread_parameter_refuses
	# ================
	def test_unread_parameter_refuses(self):
		row = gear_row("ITEM_EVENT_CH_SWORD_01_A_RARE", LATER_EVENT_SWORD, 1)
		row[items.PARAM_FIRST], row[items.PARAM_FIRST + 1] = "1", "ITEM_CH_SWORD_12_A_RARE"
		with self.assertRaisesRegex(ValueError, "authors parameter"):
			pop.convert_gear(row, 400000)
		row = gear_row("ITEM_EVENT_CH_SWORD_01_A_RARE", LATER_EVENT_SWORD, 1)
		row[pop.SOCKET_PARAM + 1] = "SOMETHING_ELSE"
		with self.assertRaisesRegex(ValueError, "authors parameter"):
			pop.convert_gear(row, 400000)

	# ================
	# test_degree_filter
	#
	# Degree 11 gear never converts, even if a row reaches the converter.
	# ================
	def test_degree_filter(self):
		self.assertEqual(pop.gear_degree(gear_row("ITEM_EVENT_CH_BOW_10_A_RARE", 1, 28)), 10)
		with self.assertRaisesRegex(ValueError, "no import lane"):
			pop.convert_gear(gear_row("ITEM_EVENT_CH_SWORD_11_A_RARE", LATER_DEGREE11, 31), 400000)

	# ================
	# test_withheld_table
	# ================
	def test_withheld_table(self):
		cases = {
			"ITEM_EVENT_PET2_SCROLL_ASS_DINOSAUR_A": "dropped",
			"ITEM_PRE_PET2_SCROLL_ENC_CAT_A": "dropped",
			"ITEM_EVENT_AVATAR_M_NASRUN_UNIQUE": "pending: event Spirit",
			"ITEM_EVENT_AVATAR_W_AMALRUN_UNIQUE": "pending: event Spirit",
			"ITEM_EVENT_AVATAR_M_SPARTA_HAT": "pending: event avatar",
			"ITEM_ETC_E090429_ROC_M": "pending: Roc flag",
			"ITEM_ETC_E090429_GACHA_ROC_W": "pending: Roc flag",
			"ITEM_ETC_E060529_GACHA_GOLDDRAGONFLAG_F": "pending: gold dragon flag",
		}
		for code, reason in cases.items():
			self.assertTrue((pop.withheld_reason(code) or "").startswith(reason), code)
		for code in ("ITEM_EVENT_CH_SWORD_01_A_RARE", "ITEM_ETC_E060517_MON_GENERATION_BOX",
				"ITEM_ETC_E060529_GOLDDRAGONFLAG_M", "ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_01",
				# The lose set ships as native (owner, 2026-11-28).
				"ITEM_EVENT_SKILLPOINT_SCROLL", "ITEM_EVENT_ARCHEMY_REINFORCE_RATIO_100",
				"ITEM_ETC_SUMMON_FLOWER_05", "ITEM_ETC_WHAN_LOOK_CHANGE_C_PANDA"):
			self.assertIsNone(pop.withheld_reason(code), code)

	# ================
	# test_wrong_machine_refuses
	# ================
	def test_wrong_machine_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, _ = self.fixture(Path(root), machine="NPC_CH_SOMEONE")
			with self.assertRaisesRegex(ValueError, pop.MACHINE_CODENAME):
				self.run_import(later, native, output, {})

	# ================
	# test_marker
	# ================
	def test_marker(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, _ = self.fixture(Path(root))
			summary = self.run_import(later, native, output, {})
			marker = json.loads((output / pop.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["format"], pop.FORMAT)
			self.assertEqual(marker["items"], summary["items"])


if __name__ == "__main__":
	unittest.main()
