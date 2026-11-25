"""
===========================================================================

test_later_mall_items.py - the later mall items import's contract.

Each admitted v1.696 item and the skills its rows name stage in the
import's own shards under marker-kept identities, with the later
client's English texts and icons; a rebuild keeps the identities and
one copy of each text; a missing or miscast row refuses; no v1.696
root named reaps exactly what a previous pass added.
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
import later_mall_items as mall_items  # noqa: E402
import mall_avatar_import as avatars  # noqa: E402
import sro_pk2  # noqa: E402
from test_nasrun_import import archive_bytes, utf16  # noqa: E402

NATIVE_ID = 5000
ITEM_CODES = [code for code, _ in mall_items.ITEMS]


# ================
# later_item_row
#
# One v1.696 item row as the later client authors it: the family's
# type, a skill name in Desc1 for the skill-carrying rows, an icon
# of its own.
# ================
def later_item_row(code, kind, identity):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY], row[items.DESCRIPTION_KEY] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = kind
	row[items.MODEL] = items.UNUSED
	row[items.DROP_MODEL] = "item\\etc\\drop_ch_bag.bsr"
	row[items.ICON] = "item\\etc\\" + code.lower() + ".ddj"
	if kind is mall_items.SKILL_ITEM_TYPE:
		row[items.PARAM_DESCRIPTION] = mall_items.SKILL_CODES[ITEM_CODES.index(code)]
	return row


# ================
# later_skill_row
#
# One v1.696 skill row: 118 columns, its icon and name keys.
# ================
def later_skill_row(code, identity):
	row = ["0"] * mall_items.SKILL_COLUMNS
	row[mall_items.SKILL_SERVICE], row[mall_items.SKILL_ID] = "1", str(identity)
	row[mall_items.SKILL_CODENAME] = code
	row[mall_items.SKILL_ICON] = "skill\\mall\\" + code.lower() + ".ddj"
	row[mall_items.SKILL_NAME_KEY] = "SN_" + code
	row[mall_items.SKILL_DESCRIPTION_KEY] = "SN_" + code + "_TT_DESC"
	return row


# ================
# later_text_row
#
# One v1.696 text row (17 columns, numeric id in 1, English in 9).
# ================
def later_text_row(key, english):
	row = ["0"] * 17
	row[0], row[1], row[2], row[9] = "1", "777", key, english
	return row


# ================
# LaterMallItemsTest
# ================
class LaterMallItemsTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch world: a fake v1.696 Media.pk2 holding the nineteen
	# items, the nine skills and their texts, and an output tree
	# with one native item, one native skill and a name table.
	# ================
	def fixture(self, base, drop_item=None, drop_skill=None):
		later, native, output = base / "later", base / "native", base / "output"
		text_root = output / mall_items.TEXT_ROOT
		text_root.mkdir(parents=True)
		later.mkdir()
		(native / mall_items.TEXT_ROOT).mkdir(parents=True)
		item_rows = [later_item_row("ITEM_MALL_DECOY", mall_items.EXP_SCROLL_TYPE, 40000)]
		for position, (code, kind) in enumerate(mall_items.ITEMS):
			if code != drop_item:
				item_rows.append(later_item_row(code, kind, 40001 + position))
		skill_rows = [later_skill_row(code, 31000 + position)
			for position, code in enumerate(mall_items.SKILL_CODES) if code != drop_skill]
		symbols = ["SN_" + code for code in ITEM_CODES + list(mall_items.SKILL_CODES)]
		symbols += [symbol + "_TT_DESC" for symbol in symbols]
		self.media = archive_bytes({
			"itemdata.txt": utf16([["itemdata_40000.txt"]]),
			"itemdata_40000.txt": utf16(item_rows),
			"skilldata.txt": utf16([["skilldata_31000.txt"]]),
			"skilldata_31000.txt": utf16(skill_rows),
			"textdata_equip&skill.txt": utf16([later_text_row(symbol, "Text of " + symbol) for symbol in symbols]),
		})
		native_item = ["0"] * items.ITEM_COLUMNS
		native_item[0], native_item[1], native_item[2] = "1", str(NATIVE_ID), "ITEM_NATIVE_THING"
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16([native_item]))
		native_skill = ["0"] * mall_items.SKILL_COLUMNS
		native_skill[0], native_skill[1], native_skill[3] = "1", str(NATIVE_ID), "SKILL_NATIVE"
		(text_root / "skilldata.txt").write_bytes(utf16([["skilldata_5000.txt"]]))
		(text_root / "skilldata_5000.txt").write_bytes(utf16([native_skill]))
		(text_root / mall_items.NAME_TABLE).write_bytes(
			utf16([["1", "SN_NATIVE_A", "", "", "", "", "", "", "Native A"]]))
		return later, native, output, text_root

	# ================
	# run_import
	#
	# One pass on the fixture's archive, the later client present and
	# pinned, the resource copies writing each icon as a later file
	# (their closure walks are the cap-110 import's, tested there).
	# ================
	def run_import(self, later, native, output, root="named"):
		data = self.media
		index = {shared.normalized_path(entry.path): entry for entry in sro_pk2.read_directory(data)}

		def open_archives(stack, root_arg, source):
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

		def copy_resource_closure(icons, native_root, output_root, archives):
			records = []
			for archive, path in sorted(icons):
				target = output_root / shared.FOLDERS[archive] / path
				target.parent.mkdir(parents=True, exist_ok=True)
				target.write_bytes(b"icon")
				records.append({"archive": archive, "path": path, "source": "later", "sha256": "x"})
			return records

		with mock.patch.object(avatars, "open_archives", open_archives), \
				mock.patch.object(jupiter, "require_v1696_pin", lambda root_arg, names: None), \
				mock.patch.object(shared, "copy_asset_closure", copy_asset_closure), \
				mock.patch.object(shared, "copy_resource_closure", copy_resource_closure):
			return mall_items.add_later_mall_items(native, output, later if root == "named" else None,
				env={} if root == "named" else {})

	# ================
	# shard_rows
	# ================
	def shard_rows(self, text_root, shard, codename_column):
		lines = (text_root / shard).read_bytes().decode("utf-16").splitlines()
		return {row[codename_column]: row for row in (line.split("\t") for line in lines if line)}

	# ================
	# names
	# ================
	def names(self, text_root):
		lines = (text_root / mall_items.NAME_TABLE).read_bytes().decode("utf-16").splitlines()
		return {row[1]: row for row in (line.split("\t") for line in lines) if len(row) > 8}

	# ================
	# test_stages_items_skills_and_texts
	# ================
	def test_stages_items_skills_and_texts(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			summary = self.run_import(later, native, output)
			self.assertTrue(summary["enabled"])
			self.assertEqual(sorted(summary["items"]), sorted(ITEM_CODES))
			self.assertEqual(sorted(summary["skills"]), sorted(mall_items.SKILL_CODES))
			staged = self.shard_rows(text_root, mall_items.ITEM_SHARD, items.CODENAME)
			self.assertEqual(sorted(staged), sorted(ITEM_CODES))
			for code, kind in mall_items.ITEMS:
				self.assertEqual(staged[code][items.TYPE_FIRST:items.TYPE_FOURTH + 1], kind)
				self.assertGreater(int(staged[code][items.IDENTITY]), NATIVE_ID)
			staged_skills = self.shard_rows(text_root, mall_items.SKILL_SHARD, mall_items.SKILL_CODENAME)
			self.assertEqual(sorted(staged_skills), sorted(mall_items.SKILL_CODES))
			names = self.names(text_root)
			for code in ITEM_CODES:
				self.assertEqual(names["SN_" + code][8], "Text of SN_" + code)
			for code in mall_items.SKILL_CODES:
				self.assertEqual(names["SN_" + code][8], "Text of SN_" + code)
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertIn(mall_items.ITEM_SHARD, manifest)

	# ================
	# test_rebuild_keeps_identities_and_texts
	# ================
	def test_rebuild_keeps_identities_and_texts(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			first = self.run_import(later, native, output)
			second = self.run_import(later, native, output)
			self.assertEqual(first["items"], second["items"])
			self.assertEqual(first["skills"], second["skills"])
			names = self.names(text_root)
			self.assertEqual(len([key for key in names if key == "SN_" + ITEM_CODES[0]]), 1)
			self.assertEqual(sorted(self.shard_rows(text_root, mall_items.ITEM_SHARD, items.CODENAME)), sorted(ITEM_CODES))

	# ================
	# test_a_missing_row_refuses
	# ================
	def test_a_missing_row_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root), drop_item=ITEM_CODES[3])
			with self.assertRaisesRegex(ValueError, "no mall item row"):
				self.run_import(later, native, output)
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root), drop_skill=mall_items.SKILL_CODES[0])
			with self.assertRaisesRegex(ValueError, "no mall skill"):
				self.run_import(later, native, output)

	# ================
	# test_no_root_reaps_the_import
	# ================
	def test_no_root_reaps_the_import(self):
		with tempfile.TemporaryDirectory() as root:
			later, native, output, text_root = self.fixture(Path(root))
			self.run_import(later, native, output)
			summary = self.run_import(later, native, output, root=None)
			self.assertFalse(summary["enabled"])
			self.assertFalse((text_root / mall_items.ITEM_SHARD).exists())
			self.assertFalse((text_root / mall_items.SKILL_SHARD).exists())
			self.assertFalse((output / mall_items.MARKER).exists())
			names = self.names(text_root)
			self.assertNotIn("SN_" + ITEM_CODES[0], names)
			self.assertIn("SN_NATIVE_A", names)


if __name__ == "__main__":
	unittest.main()
