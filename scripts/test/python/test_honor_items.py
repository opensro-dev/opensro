"""
===========================================================================

test_honor_items.py - the derived honor items' contract (#3548)

The rule every shipped honor row follows (the rare row, no trade, borrow
or drop, level -2/+2/+6, a flat price and 40% back), the linear price
continuation, and the import's switch: on stages the derived rows under
marker-kept identities with their texts and resinfo lines, a rerun is
byte-identical, and off gives back exactly the tree an unswitched build
has.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock

import cap110_item_import as shared  # noqa: E402
import cap110_items as items  # noqa: E402
import honor_items as honor  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402

RARE_LEVEL = 90
STONE_ID = 900
FIRST_ID = 1000


# ================
# utf16
# ================
def utf16(rows):
	return b"\xff\xfe" + "".join("\t".join(row) + "\r\n" for row in rows).encode("utf-16-le")


# ================
# item_row
# ================
def item_row(code, identity, level=RARE_LEVEL):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME] = "1", str(identity), code
	row[items.NAME_KEY] = "SN_" + code
	row[items.REQUIRED_LEVEL] = str(level)
	for column in (honor.CAN_TRADE, honor.CAN_BORROW, honor.CAN_DROP):
		row[column] = "1"
	row[honor.DURABILITY] = "136"
	return row


# ================
# stone_row
# ================
def stone_source():
	row = item_row(honor.stone_code(9), STONE_ID)
	row[items.CLASS] = row[items.PARAM_FIRST] = "9"
	return row


# ================
# build_tree
#
# A native extraction and a build output: degree 9 and 10 Chinese blade
# rares (10 grade A only), a native degree 9 honor blade, the degree 9
# stone, the client's resinfo naming the blade and a shield, and the
# effect programs.
# ================
def build_tree(root):
	native, output = root / "native", root / "out"
	for base in (native, output):
		(base / shared.TEXT_ROOT).mkdir(parents=True)
	rows = [item_row("ITEM_CH_BLADE_09_A_RARE", FIRST_ID), item_row("ITEM_CH_BLADE_10_A_RARE", FIRST_ID + 1, 100),
		item_row("ITEM_CH_BLADE_09_A_RARE_HONOR", FIRST_ID + 2, 88), stone_source()]
	(output / shared.TEXT_ROOT / "itemdata_5000.txt").write_bytes(utf16(rows))
	# The builder's output links every native table, so it holds the
	# manifest and the name table too.
	names = [["1", "SN_" + honor.stone_code(9), "x", "x", "x", "x", "x", "x", "Repair stone (9 degree)"]]
	for base in (native, output):
		(base / shared.TEXT_ROOT / honor.MANIFEST).write_bytes(utf16([["itemdata_5000.txt"]]))
		(base / shared.TEXT_ROOT / honor.NAME_TABLE).write_bytes(utf16(names))
	(native / shared.GLOW_ROOT).mkdir(parents=True)
	(native / shared.GLOW_ROOT / shared.GLOW_TABLE).write_bytes(b"ITEM_CH_BLADE_10_A_RARE\t2\r\n")
	rare = "ITEM_CH_BLADE_10_A_RARE\tsystem\\system_rareblade_a.efp\t143\tBone01\tnone\r\n"
	rare += "ITEM_CH_SHIELD_10_A_RARE_HONOR\tsystem\\system_rareshield_a_honor.efp\t143\tBone01\tnone\r\n"
	(native / shared.GLOW_ROOT / shared.RARE_TABLE).write_bytes(rare.encode("latin1"))
	(native / honor.PARTICLES / "system").mkdir(parents=True)
	(native / honor.PARTICLES / "system" / "system_rareblade_a_honor.efp").write_bytes(b"")
	return native, output


# ================
# snapshot
# ================
def snapshot(root):
	return {str(path.relative_to(root)): path.read_bytes() for path in sorted(root.rglob("*")) if path.is_file()}


# ================
# run
# ================
def run(native, output, switch):
	with mock.patch.dict(os.environ, {honor.FLAG: switch}):
		return honor.add_honor_items(native, output)


class HonorRuleTest(unittest.TestCase):
	def test_honor_row_follows_the_shipped_rule(self):
		rare = item_row("ITEM_CH_BLADE_10_A_RARE", FIRST_ID)
		for grade, level in (("A", 88), ("B", 92), ("C", 96)):
			row = honor.honor_row(rare, 77, 10, grade)
			self.assertEqual(row[items.CODENAME], "ITEM_CH_BLADE_10_A_RARE_HONOR")
			self.assertEqual(row[items.IDENTITY], "77")
			self.assertEqual(row[items.REQUIRED_LEVEL], str(level))
			self.assertEqual([row[c] for c in (honor.CAN_TRADE, honor.CAN_BORROW, honor.CAN_DROP)], ["0"] * 3)
			gold = honor.honor_gold(10, grade)
			self.assertEqual((row[honor.PRICE], row[honor.SELL_PRICE]), (str(gold), str(gold * 2 // 5)))
		self.assertEqual(rare[honor.CAN_TRADE], "1")

	def test_prices_keep_native_and_continue_linearly(self):
		self.assertEqual(honor.honor_gold(9, "A"), 5800000)
		self.assertEqual([honor.honor_gold(d, "B") for d in (10, 11)], [11600000, 15600000])
		self.assertEqual([honor.honor_points(d, "C") for d in (10, 11)], [13500, 19000])
		self.assertEqual([honor.stone_gold(d) for d in (10, 11)], [70000, 90000])
		self.assertEqual([honor.stone_points(d) for d in (10, 11)], [20, 25])

	def test_stone_row_and_text_carry_their_degree(self):
		row = honor.stone_row(stone_source(), 5, 11)
		self.assertEqual((row[items.CODENAME], row[items.NAME_KEY]), (honor.stone_code(11), "SN_" + honor.stone_code(11)))
		self.assertEqual((row[items.CLASS], row[items.PARAM_FIRST]), ("11", "11"))
		self.assertEqual(honor.stone_text(["1", "SN_X", "Stone (9 degree)", "19"], 10)[2:], ["Stone (10 degree)", "19"])


class HonorImportTest(unittest.TestCase):
	def test_off_build_is_untouched(self):
		with tempfile.TemporaryDirectory() as temp:
			native, output = build_tree(Path(temp))
			before = snapshot(output)
			self.assertEqual(run(native, output, "0"), {"enabled": False})
			self.assertEqual(snapshot(output), before)

	def test_on_stages_the_line_and_off_gives_it_back(self):
		with tempfile.TemporaryDirectory() as temp:
			native, output = build_tree(Path(temp))
			before = snapshot(output)
			summary = run(native, output, "1")
			# Degree 10 blade A from its rare (no B, C or degree 11 rare), and
			# the degree 10 and 11 stones; the native degree 9 honor blade stays.
			self.assertEqual(sorted(summary["items"]), ["ITEM_CH_BLADE_10_A_RARE_HONOR", honor.stone_code(10), honor.stone_code(11)])
			self.assertTrue(all(identity > FIRST_ID + 2 for identity in summary["items"].values()))
			shard = tables.table_rows((output / shared.TEXT_ROOT / honor.ITEM_SHARD).read_bytes())
			self.assertEqual(len(shard), 3)
			names = (output / shared.TEXT_ROOT / honor.NAME_TABLE).read_bytes().decode("utf-16")
			self.assertIn("Repair stone (11 degree)", names)
			rare = (output / shared.GLOW_ROOT / shared.RARE_TABLE).read_bytes().decode("latin1")
			self.assertIn("ITEM_CH_BLADE_10_A_RARE_HONOR\tsystem\\system_rareblade_a_honor.efp", rare)
			glow = (output / shared.GLOW_ROOT / shared.GLOW_TABLE).read_bytes().decode("latin1")
			self.assertIn("ITEM_CH_BLADE_10_A_RARE_HONOR\t2", glow)

			staged = snapshot(output)
			run(native, output, "1")
			self.assertEqual(snapshot(output), staged)

			run(native, output, "0")
			self.assertEqual(snapshot(output), before)
			self.assertFalse((output / shared.GLOW_ROOT).exists())

	def test_identities_are_kept_across_reruns(self):
		with tempfile.TemporaryDirectory() as temp:
			native, output = build_tree(Path(temp))
			first = run(native, output, "1")["items"]
			self.assertEqual(run(native, output, "1")["items"], first)


if __name__ == "__main__":
	unittest.main()
