"""
===========================================================================
test_reset_scroll_import.py - the reset scrolls import's contract.

Each reset scroll whose switch is on is its v1.696 item: the later
client's row under a marker-kept identity, its icon copied, the skill
scroll's level window lifted, and its authored English name and
description in the name table. A rebuild keeps the identities and one
copy of each text; a switch off reaps exactly what that scroll added; a
row the later client lacks refuses.
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
import mall_avatar_import as avatars  # noqa: E402
import reset_scroll_import as resets  # noqa: E402
import sro_pk2  # noqa: E402
from test_nasrun_import import archive_bytes, utf16  # noqa: E402

ALL_ON = {flag: "on" for flag, *_ in resets.SCROLLS}
ALL_OFF = {flag: "" for flag, *_ in resets.SCROLLS}
STAT, SKILL = (code for _, code, *_ in resets.SCROLLS)
NATIVE_ID = 5000
SKILL_WINDOW = ["1", "1", "100", "-1", "0", "-1", "0", "-1"]


# ================
# later_row
#
# One v1.696 row as the later client authors the two scrolls: type
# 3/3/13/13, Param1 2 or 5, an icon of its own.
# ================
def later_row(code, identity, param1, window):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY], row[items.DESCRIPTION_KEY] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = ["3", "3", "13", "13"]
	row[items.MODEL] = items.UNUSED
	row[items.DROP_MODEL] = "item\\etc\\drop_ch_bag.bsr"
	row[items.ICON] = "item\\etc\\" + code.lower() + ".ddj"
	row[resets.REQUIREMENTS] = window
	row[resets.PARAM1] = param1
	return row


# ================
# ResetScrollImportTest
# ================
class ResetScrollImportTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 Media.pk2 holding the two scrolls
	# and a decoy, and an output tree with one native item and a name
	# table.
	# ================
	def fixture(self, base, drop=None):
		later, native, output = base / "later", base / "native", base / "output"
		text_root = output / resets.TEXT_ROOT
		text_root.mkdir(parents=True)
		later.mkdir()
		native.mkdir()
		rows = [later_row("ITEM_MALL_DECOY", 40000, "2", ["0", "-1"] * 4)]
		if drop != STAT:
			rows.append(later_row(STAT, 40001, "2", ["0", "-1"] * 4))
		if drop != SKILL:
			rows.append(later_row(SKILL, 40002, "5", SKILL_WINDOW))
		self.media = archive_bytes({
			"itemdata.txt": utf16([["itemdata_40000.txt"]]),
			"itemdata_40000.txt": utf16(rows),
		})
		native_row = ["0"] * items.ITEM_COLUMNS
		native_row[0], native_row[1], native_row[2] = "1", str(NATIVE_ID), "ITEM_NATIVE_THING"
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16([native_row]))
		(text_root / resets.NAME_TABLE).write_bytes(utf16([["1", "SN_NATIVE_A", "", "", "", "", "", "", "Native A"]]))
		return later, native, output, text_root

	# ================
	# run_import
	#
	# One pass on the fixture's archive, the later client present and
	# pinned, the resource copy writing each icon as a later file (its
	# closure walk is the cap-110 import's, tested there).
	# ================
	def run_import(self, later, native, output, env):
		data = self.media
		index = {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)}

		def open_archives(stack, root, source):
			self.assertIs(source, avatars.V1696_SOURCE)
			return {"Media": (data, index)}, {"Media": "fixture"}

		def copy_asset_closure(rows, native_root, output_root, archives, worn=True):
			self.assertFalse(worn)
			records = []
			for row in rows:
				path = "icon/" + row[items.ICON].replace("\\", "/")
				target = output_root / shared.FOLDERS["Media"] / path
				target.parent.mkdir(parents=True, exist_ok=True)
				target.write_bytes(b"icon")
				records.append({"archive": "Media", "path": path, "source": "later", "sha256": "x"})
			return records

		with mock.patch.dict(os.environ, env), \
				mock.patch.object(avatars, "open_archives", open_archives), \
				mock.patch.object(jupiter, "require_v1696_pin", lambda root, names: None), \
				mock.patch.object(shared, "copy_asset_closure", copy_asset_closure):
			return resets.add_reset_scrolls(native, output, later)

	# ================
	# shard_rows
	# ================
	def shard_rows(self, text_root):
		lines = (text_root / resets.ITEM_SHARD).read_bytes().decode("utf-16").splitlines()
		return {row[items.CODENAME]: row for row in (line.split("\t") for line in lines if line)}

	# ================
	# names
	# ================
	def names(self, text_root):
		lines = (text_root / resets.NAME_TABLE).read_bytes().decode("utf-16").splitlines()
		return {row[1]: row for row in (line.split("\t") for line in lines) if len(row) > 8}

	# ================
	# test_all_on_stages_both_items
	# ================
	def test_all_on_stages_both_items(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, ALL_ON)
			self.assertEqual(sorted(summary["items"]), sorted([STAT, SKILL]))
			rows = self.shard_rows(text_root)
			self.assertEqual(sorted(rows), sorted([STAT, SKILL]))
			for code in (STAT, SKILL):
				self.assertEqual(rows[code][items.ICON], "item\\etc\\" + code.lower() + ".ddj")
				self.assertEqual(rows[code][items.TYPE_FIRST:items.TYPE_FOURTH + 1], ["3", "3", "13", "13"])
				self.assertGreater(int(rows[code][items.IDENTITY]), NATIVE_ID)
				self.assertEqual(int(rows[code][items.IDENTITY]), summary["items"][code])
			# The skill scroll's 1-100 window is lifted; its Param1 stays 5.
			self.assertEqual(rows[SKILL][resets.REQUIREMENTS], ["0", "-1"] * 4)
			self.assertEqual(rows[SKILL][resets.PARAM1], "5")
			self.assertEqual(rows[STAT][resets.PARAM1], "2")
			self.assertIn(resets.ITEM_SHARD, (text_root / "itemdata.txt").read_bytes().decode("utf-16"))
			names = self.names(text_root)
			self.assertEqual(names["SN_" + STAT][8], "Stat Recall (All Levels)")
			self.assertEqual(names["SN_" + SKILL][8], "Scroll of Skill Restore")
			self.assertEqual(
				names["SN_" + SKILL + "_TT_DESC"][8],
				"Returns every skill and mastery point you have spent: your skills are forgotten and every "
				"mastery goes back to 0.")
			self.assertEqual(
				names["SN_" + STAT + "_TT_DESC"][8],
				"Returns every stat point you have spent: STR and INT go back to their values for your level.")
			self.assertIn("SN_NATIVE_A", names)
			self.assertTrue((output / "Media_extracted/icon/item/etc/item_mall_skill_point_recall_a.ddj").exists())
			marker = json.loads((output / resets.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["items"], summary["items"])
			# A rebuild keeps the identities and one copy of each text.
			again = self.run_import(later, native, output, ALL_ON)
			self.assertEqual(again["items"], summary["items"])
			table = (text_root / resets.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertEqual(table.count("\tSN_" + SKILL + "\t"), 1)

	# ================
	# test_each_switch_stages_its_own_item
	# ================
	def test_each_switch_stages_its_own_item(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output, {**ALL_OFF, "SRO_SKILL_RESET_SCROLL": "on"})
			self.assertEqual(list(summary["items"]), [SKILL])
			self.assertEqual(list(self.shard_rows(text_root)), [SKILL])
			self.assertNotIn("SN_" + STAT, self.names(text_root))

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			self.run_import(later, native, output, ALL_ON)
			self.assertEqual(self.run_import(later, native, output, ALL_OFF), {"enabled": False})
			self.assertFalse((text_root / resets.ITEM_SHARD).exists())
			self.assertFalse((output / resets.MARKER).exists())
			self.assertFalse((output / "Media_extracted/icon/item/etc/item_mall_skill_point_recall_a.ddj").exists())
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertNotIn(resets.ITEM_SHARD, manifest)
			self.assertIn("itemdata_5000.txt", manifest)
			names = (text_root / resets.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertNotIn("RECALL", names)
			self.assertIn("SN_NATIVE_A", names)

	# ================
	# test_missing_later_row_refuses
	# ================
	def test_missing_later_row_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, _ = self.fixture(Path(root), drop=SKILL)
			with self.assertRaises(ValueError):
				self.run_import(later, native, output, ALL_ON)


if __name__ == "__main__":
	unittest.main()
