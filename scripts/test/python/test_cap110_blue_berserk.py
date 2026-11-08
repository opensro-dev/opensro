"""
===========================================================================
test_cap110_blue_berserk.py - the Blue Berserk items, shop rows and texts.

They follow the item switch and the level cap, not the quest switch.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import unittest

import cap110_blue_berserk as blue
import cap110_items as items
import import_vsro_alexandria as tables


# ================
# item
# ================
def item(code, identity=100):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME], row[items.PLACEHOLDER_NAME] = "1", str(identity), code, "korean"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = blue.ITEM_KINDS[code]
	for column in range(items.PARAM_FIRST, items.PARAM_LAST, items.PARAM_STRIDE):
		row[column], row[column + 1] = "-1", items.UNUSED
	return row


# ================
# text_table
#
# A UTF-16 table of the given line strings, as the variant stores one.
# ================
def text_table(*lines):
	return b"\xff\xfe" + ("\r\n".join(lines) + "\r\n").encode("utf-16-le")


# ================
# native_line / later_row
# ================
def native_line(key, english):
	return "\t".join(["1", key, "korean", "", "", "cn", "tw", "jp", english, "0", ""])


def later_row(key, english):
	return ["1", key, "korean", "", "tw", "cn", "de", "jp", english, "vi", "pt", "ru", "tr", "es", "ar"]


# ================
# BlueBerserkTest
# ================
class BlueBerserkTest(unittest.TestCase):
	# ================
	# test_the_item_switch_and_the_level_cap_decide
	# ================
	def test_the_item_switch_and_the_level_cap_decide(self):
		self.assertTrue(blue.enabled({"SRO_CAP110_ITEMS": "on", "SRO_LEVEL_CAP": "110"}))
		self.assertTrue(blue.enabled({"SRO_CAP110_ITEMS": "on", "SRO_LEVEL_CAP": "110", "SRO_CAP110_QUESTS": ""}))
		self.assertFalse(blue.enabled({"SRO_CAP110_QUESTS": "on", "SRO_LEVEL_CAP": "110"}))
		self.assertFalse(blue.enabled({"SRO_CAP110_ITEMS": "on"}))

	# ================
	# test_all_nine_items_are_selected_and_a_native_one_is_skipped
	# ================
	def test_all_nine_items_are_selected_and_a_native_one_is_skipped(self):
		later = {code: item(code) for code in blue.ITEM_KINDS}
		self.assertEqual(len(blue.select_items(later, {})), 9)
		native = {"ITEM_ETC_SECOND_HWAN": item("ITEM_ETC_SECOND_HWAN")}
		self.assertEqual(len(blue.select_items(later, native)), 8)

	# ================
	# test_a_missing_or_wrong_kind_item_refuses
	# ================
	def test_a_missing_or_wrong_kind_item_refuses(self):
		later = {code: item(code) for code in blue.ITEM_KINDS}
		del later["ITEM_ETC_SECOND_HWAN_SCROLL_02"]
		with self.assertRaises(ValueError):
			blue.select_items(later, {})
		later = {code: item(code) for code in blue.ITEM_KINDS}
		later["ITEM_ETC_SECOND_HWAN"][items.TYPE_FOURTH] = "9"
		with self.assertRaises(ValueError):
			blue.select_items(later, {})

	# ================
	# test_text_fills_blank_english_appends_missing_and_keeps_native_english
	# ================
	def test_text_fills_blank_english_appends_missing_and_keeps_native_english(self):
		raw = text_table(native_line("A", ""), native_line("B", "native words"), native_line("OTHER", ""))
		later = {key: later_row(key, "later " + key) for key in ("A", "B", "C")}
		got, appended, replaced = blue.fill_text(raw, later, ("A", "B", "C"))
		rows = {row[1]: row for row in tables.table_rows(got)}
		self.assertEqual(rows["A"][8], "later A")
		self.assertEqual(rows["B"][8], "native words")
		self.assertEqual(rows["C"][8], "later C")
		self.assertEqual(rows["OTHER"][8], "")
		self.assertEqual(appended, ["C"])
		self.assertEqual(list(replaced), ["A"])

	# ================
	# test_restoring_the_text_returns_the_original_bytes
	# ================
	def test_restoring_the_text_returns_the_original_bytes(self):
		raw = text_table("// a comment line", native_line("A", ""), native_line("B", "native words"), native_line("Z", "zz"))
		later = {key: later_row(key, "later " + key) for key in ("A", "B", "C")}
		got, appended, replaced = blue.fill_text(raw, later, ("A", "B", "C"))
		self.assertNotEqual(got, raw)
		self.assertEqual(blue.restore_text(got, set(appended), replaced), raw)

	# ================
	# test_shop_rows_use_the_next_free_slot_of_every_potion_tab
	# ================
	def test_shop_rows_use_the_next_free_slot_of_every_potion_tab(self):
		later = {
			"refpackageitem.txt": [["1", "15", str(100 + at), name, "0", "X"] + ["-1", "xxx"] * 5 for at, name in enumerate(blue.PACKAGES)],
			"refscrapofpackageitem.txt": [["1", "15", name, "ITEM_" + name[13:]] + ["0"] * 24 + [str(500 + at)] for at, name in enumerate(blue.PACKAGES)],
			"refpricepolicyofitem.txt": [["1", "15", name, "1", "0", str(200000 + 50000 * at)] + ["-1", "xxx"] * 4 for at, name in enumerate(blue.PACKAGES)],
		}
		variant = {"refshopgoods.txt": [["1", "11", tab, "PACKAGE_ITEM_ETC_HP_POTION_01", str(slot)] + ["-1", "xxx"] * 4
			for tab in blue.POTION_TABS for slot in range(0, 3 + len(tab) % 3)]}
		got = blue.shop_rows(later, variant)
		goods = got["refshopgoods.txt"]
		self.assertEqual(len(goods), 20)
		for tab in blue.POTION_TABS:
			existing = max(int(row[4]) for row in variant["refshopgoods.txt"] if row[2] == tab)
			self.assertEqual([int(row[4]) for row in goods if row[2] == tab], [existing + step for step in (1, 2, 3, 4)])
		for row in goods:
			self.assertEqual((len(row), row[0], row[1]), (13, "1", "11"))
		prices = got["refpricepolicyofitem.txt"]
		self.assertEqual({len(row) for row in prices}, {13})
		self.assertEqual([row[4] for row in prices], ["200000", "250000", "300000", "350000"])
		self.assertEqual({row[1] for row in got["refpackageitem.txt"] + got["refscrapofpackageitem.txt"]}, {"11"})

	# ================
	# test_shop_rows_refuse_a_missing_tab_or_package
	# ================
	def test_shop_rows_refuse_a_missing_tab_or_package(self):
		later = {"refpackageitem.txt": [], "refscrapofpackageitem.txt": [], "refpricepolicyofitem.txt": []}
		with self.assertRaises(ValueError):
			blue.shop_rows(later, {"refshopgoods.txt": []})


if __name__ == "__main__":
	unittest.main()
