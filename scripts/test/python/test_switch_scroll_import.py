"""
===========================================================================
test_switch_scroll_import.py - the switch scrolls import's contract.

Each switch scroll whose flag is on becomes an item of its own: its
donor's v1.150 row copied under a new codename and a marker-kept
identity, with its English name and description in the name table. A
rebuild keeps the identities and one copy of each text; a flag off
reaps exactly what that scroll added; a missing donor refuses.
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import json
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import switch_scroll_import as switch  # noqa: E402

ALL_ON = {flag: "on" for flag, *_ in switch.SCROLLS}
ALL_OFF = {flag: "" for flag, *_ in switch.SCROLLS}


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


# ================
# item_row
# ================
def item_row(code, identity, icon):
	row = ["0"] * switch.ITEM_COLUMNS
	row[0], row[1], row[2] = "1", str(identity), code
	row[5], row[6] = "SN_" + code, "SN_" + code + "_TT_DESC"
	row[9:13] = ["3", "3", "13", "11"]
	row[54] = icon
	return row


# ================
# name_row
#
# One row of the v1.150 name table (service, key, then the language
# cells, English in column 8).
# ================
def name_row(key, text):
	return ["1", key, text, "", "", text, text, text, text, text, ""]


# ================
# SwitchScrollImportTest
# ================
class SwitchScrollImportTest(unittest.TestCase):
	# ================
	# fixture
	#
	# A scratch variant: the three donors and a native item in the
	# output's item tables, the native name table in the native tree.
	# ================
	def fixture(self, base, drop=None):
		native, output = base / "native", base / "output"
		text_root = output / switch.TEXT_ROOT
		text_root.mkdir(parents=True)
		(native / switch.TEXT_ROOT).mkdir(parents=True)
		rows = [item_row("ITEM_NATIVE_THING", 5000, "item\\etc\\thing.ddj")]
		for identity, (_, _, donor, _, _) in enumerate(switch.SCROLLS, start=3790):
			if donor != drop:
				rows.append(item_row(donor, identity, "item\\etc\\" + donor.lower() + ".ddj"))
		(text_root / "itemdata.txt").write_bytes(utf16([["itemdata_5000.txt"]]))
		(text_root / "itemdata_5000.txt").write_bytes(utf16(rows))
		(native / switch.TEXT_ROOT / switch.NAME_TABLE).write_bytes(
			utf16([name_row("SN_ITEM_NATIVE_THING", "Native thing")]))
		return native, output, text_root

	# ================
	# shard_rows
	# ================
	def shard_rows(self, text_root):
		lines = (text_root / switch.ITEM_SHARD).read_bytes().decode("utf-16").splitlines()
		return [line.split("\t") for line in lines if line]

	# ================
	# test_all_on_stages_three_items
	# ================
	def test_all_on_stages_three_items(self):
		with tempfile.TemporaryDirectory() as root:
			native, output, text_root = self.fixture(Path(root))
			with mock.patch.dict(os.environ, ALL_ON):
				summary = switch.add_switch_scrolls(native, output)
			self.assertEqual(sorted(summary["items"]), sorted(code for _, code, *_ in switch.SCROLLS))
			rows = {row[2]: row for row in self.shard_rows(text_root)}
			weapon = rows["ITEM_MALL_WEAPON_SWITCH_SCROLL"]
			# The donor's row, its icon and type kept, under a new identity
			# above every row of the tree and its own name symbols.
			self.assertEqual(weapon[54], "item\\etc\\item_mall_reinforce_weapon_inc_2_scroll.ddj")
			self.assertEqual(weapon[9:13], ["3", "3", "13", "11"])
			self.assertEqual(weapon[5:7], ["SN_ITEM_MALL_WEAPON_SWITCH_SCROLL", "SN_ITEM_MALL_WEAPON_SWITCH_SCROLL_TT_DESC"])
			self.assertTrue(all(identity > 5000 for identity in summary["items"].values()))
			self.assertEqual(int(weapon[1]), summary["items"]["ITEM_MALL_WEAPON_SWITCH_SCROLL"])
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertIn(switch.ITEM_SHARD, manifest)
			self.assertIn("itemdata_5000.txt", manifest)
			names = [line.split("\t") for line in (text_root / switch.NAME_TABLE).read_bytes().decode("utf-16").splitlines()]
			by_key = {row[1]: row for row in names if len(row) > 8}
			self.assertEqual(by_key["SN_ITEM_MALL_ARMOR_SWITCH_SCROLL"][8], "Armor Switch Scroll")
			self.assertEqual(
				by_key["SN_ITEM_MALL_ACCESSORY_SWITCH_SCROLL_TT_DESC"][8],
				"Turns an earring, necklace or ring in your bag into the same accessory of the other race. "
				"Plus and blue options are kept.")
			self.assertIn("SN_ITEM_NATIVE_THING", by_key)
			marker = json.loads((output / switch.MARKER).read_text(encoding="utf-8"))
			self.assertEqual(marker["items"], summary["items"])
			# A rebuild keeps the identities and one copy of each text.
			with mock.patch.dict(os.environ, ALL_ON):
				again = switch.add_switch_scrolls(native, output)
			self.assertEqual(again["items"], summary["items"])
			table = (text_root / switch.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertEqual(table.count("\tSN_ITEM_MALL_WEAPON_SWITCH_SCROLL\t"), 1)
			self.assertEqual(len(self.shard_rows(text_root)), 3)

	# ================
	# test_each_flag_stages_its_own_item
	# ================
	def test_each_flag_stages_its_own_item(self):
		with tempfile.TemporaryDirectory() as root:
			native, output, text_root = self.fixture(Path(root))
			with mock.patch.dict(os.environ, {**ALL_OFF, "SRO_ARMOR_SWITCH_SCROLL": "on"}):
				summary = switch.add_switch_scrolls(native, output)
			self.assertEqual(list(summary["items"]), ["ITEM_MALL_ARMOR_SWITCH_SCROLL"])
			self.assertEqual([row[2] for row in self.shard_rows(text_root)], ["ITEM_MALL_ARMOR_SWITCH_SCROLL"])

	# ================
	# test_off_rebuild_reaps_owned_outputs
	# ================
	def test_off_rebuild_reaps_owned_outputs(self):
		with tempfile.TemporaryDirectory() as root:
			native, output, text_root = self.fixture(Path(root))
			with mock.patch.dict(os.environ, ALL_ON):
				switch.add_switch_scrolls(native, output)
			with mock.patch.dict(os.environ, ALL_OFF):
				self.assertEqual(switch.add_switch_scrolls(native, output), {"enabled": False})
			self.assertFalse((text_root / switch.ITEM_SHARD).exists())
			self.assertFalse((output / switch.MARKER).exists())
			manifest = (text_root / "itemdata.txt").read_bytes().decode("utf-16")
			self.assertNotIn(switch.ITEM_SHARD, manifest)
			self.assertIn("itemdata_5000.txt", manifest)
			names = (text_root / switch.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertNotIn("SWITCH_SCROLL", names)
			self.assertIn("SN_ITEM_NATIVE_THING", names)

	# ================
	# test_missing_donor_refuses
	# ================
	def test_missing_donor_refuses(self):
		with tempfile.TemporaryDirectory() as root:
			native, output, _ = self.fixture(Path(root), drop="ITEM_MALL_REINFORCE_ACCESSARY_INC_1_SCROLL")
			with mock.patch.dict(os.environ, ALL_ON):
				with self.assertRaises(ValueError):
					switch.add_switch_scrolls(native, output)


if __name__ == "__main__":
	unittest.main()
