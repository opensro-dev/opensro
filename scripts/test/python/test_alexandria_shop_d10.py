"""
===========================================================================

test_alexandria_shop_d10.py - ordinary degree 10 stock in the city's shops

Synthetic shop tables, no game data: a degree 9 good gains its degree 10
twin when the item exists, the shared Khotan stores are left alone, a tab
never passes the native maximum, and a missing item adds nothing.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import alexandria_shop_d10 as d10  # noqa: E402
from import_vsro_alexandria import table_rows  # noqa: E402


def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


def item(code, price):
	row = ["0"] * 40
	row[0], row[1], row[2], row[5], row[6] = "1", "9", code, "SN_" + code, "SN_" + code + "_TT_DESC"
	row[10] = "item\\x.ddj"
	row[d10.PRICE_COLUMN] = str(price)
	return row


FILLER = ["-1", "xxx"] * 4


def tables(goods):
	"""Khotan's smith store with one tab, the city's smith group on it."""
	packages = []
	for code in {c for c in goods}:
		packages.append(["1", "11", str(len(packages) + 10), code, "0", "EXPAND_TERM_ALL", "SN", "SN_TT", "old.ddj", *FILLER])
	scraps = [["1", "11", c, c.replace("PACKAGE_ITEM_", "ITEM_", 1), "0", "0", "120", *["0"] * 13, *FILLER, str(100 + i)] for i, c in enumerate(sorted(set(goods)))]
	prices = [["1", "11", c, "1", "1000", *FILLER] for c in sorted(set(goods))]
	return {
		"refshop.txt": utf16([["1", "11", "5", "STORE_KT_SMITH", *FILLER]]),
		"refmappingshopgroup.txt": utf16([
			["1", "11", "GROUP_STORE_KT_SMITH", "STORE_KT_SMITH"],
			["1", "11", "GROUP_STORE_SD_M_SMITH", "STORE_KT_SMITH"]
		]),
		"refmappingshopwithtab.txt": utf16([["1", "11", "STORE_KT_SMITH", "STORE_KT_SMITH_GROUP1"]]),
		"refshoptabgroup.txt": utf16([["1", "11", "7", "STORE_KT_SMITH_GROUP1", "SN_GROUP"]]),
		"refshoptab.txt": utf16([["1", "11", "9", "STORE_KT_SMITH_TAB1", "STORE_KT_SMITH_GROUP1", "SN_TAB_WEAPON", ""]]),
		"refshopgoods.txt": utf16([["1", "11", "STORE_KT_SMITH_TAB1", code, str(slot), *FILLER] for slot, code in enumerate(goods)]),
		"refpackageitem.txt": utf16(packages),
		"refscrapofpackageitem.txt": utf16(scraps),
		"refpricepolicyofitem.txt": utf16(prices)
	}


GOODS = ["PACKAGE_ITEM_CH_BLADE_08_A", "PACKAGE_ITEM_CH_BLADE_09_A", "PACKAGE_ITEM_CH_BOW_09_A", "PACKAGE_ITEM_ETC_AMMO_ARROW_01"]
ITEMS = {
	"ITEM_CH_BLADE_10_A": item("ITEM_CH_BLADE_10_A", 6492000),
	"ITEM_CH_BOW_10_A": item("ITEM_CH_BOW_10_A", 5000000)
}


class StockTests(unittest.TestCase):
	def test_a_degree_9_good_gains_its_degree_10_twin_right_after_it(self):
		stock, added = d10.next_stock(GOODS, ITEMS)
		self.assertEqual(added, ["PACKAGE_ITEM_CH_BLADE_10_A", "PACKAGE_ITEM_CH_BOW_10_A"])
		self.assertEqual(stock, [
			"PACKAGE_ITEM_CH_BLADE_08_A", "PACKAGE_ITEM_CH_BLADE_09_A", "PACKAGE_ITEM_CH_BLADE_10_A",
			"PACKAGE_ITEM_CH_BOW_09_A", "PACKAGE_ITEM_CH_BOW_10_A", "PACKAGE_ITEM_ETC_AMMO_ARROW_01"
		])

	def test_a_missing_item_adds_nothing(self):
		stock, added = d10.next_stock(GOODS, {})
		self.assertEqual((stock, added), (GOODS, []))

	def test_a_tab_never_passes_the_native_maximum_and_the_lowest_degrees_leave_first(self):
		goods = [f"PACKAGE_ITEM_CH_W{n:02d}_{degree:02d}_A" for n in range(10) for degree in (5, 9)]
		items = {f"ITEM_CH_W{n:02d}_10_A": item("x", 1) for n in range(10)}
		stock, _ = d10.next_stock(goods, items)
		self.assertEqual(len(stock), d10.MAX_TAB_SLOTS if len(goods) + 10 > d10.MAX_TAB_SLOTS else len(goods) + 10)
		self.assertTrue(all("_05_" not in p for p in stock) or len(goods) + 10 <= d10.MAX_TAB_SLOTS)


class MergeTests(unittest.TestCase):
	def merge(self, items=ITEMS):
		return d10.merge_d10_stock(tables(GOODS), items)

	def test_the_city_gets_a_store_of_its_own_and_khotans_stays_unchanged(self):
		raw = tables(GOODS)
		merged, summary = d10.merge_d10_stock(raw, ITEMS)
		self.assertEqual(summary["stores"], ["STORE_SD_SMITH"])
		mapping = {tuple(r[2:]) for r in table_rows(merged["refmappingshopgroup.txt"])}
		self.assertIn(("GROUP_STORE_SD_M_SMITH", "STORE_SD_SMITH"), mapping)
		self.assertIn(("GROUP_STORE_KT_SMITH", "STORE_KT_SMITH"), mapping)
		# Khotan's tab still has exactly its four goods.
		goods = table_rows(merged["refshopgoods.txt"])
		self.assertEqual(len([r for r in goods if r[2] == "STORE_KT_SMITH_TAB1"]), len(GOODS))

	def test_the_new_tab_lists_the_degree_10_goods_with_consecutive_slots(self):
		merged, summary = self.merge()
		goods = [r for r in table_rows(merged["refshopgoods.txt"]) if r[2] == "STORE_SD_SMITH_TAB1"]
		self.assertEqual([r[4] for r in goods], [str(n) for n in range(len(goods))])
		self.assertIn("PACKAGE_ITEM_CH_BLADE_10_A", [r[3] for r in goods])
		self.assertEqual(summary["packages"], 2)

	def test_a_new_package_takes_the_item_price_names_and_a_fresh_id(self):
		merged, _ = self.merge()
		package = next(r for r in table_rows(merged["refpackageitem.txt"]) if r[3] == "PACKAGE_ITEM_CH_BLADE_10_A")
		ids = [r[2] for r in table_rows(merged["refpackageitem.txt"])]
		self.assertEqual(len(ids), len(set(ids)))
		self.assertEqual(package[6:8], ["SN_ITEM_CH_BLADE_10_A", "SN_ITEM_CH_BLADE_10_A_TT_DESC"])
		price = next(r for r in table_rows(merged["refpricepolicyofitem.txt"]) if r[2] == "PACKAGE_ITEM_CH_BLADE_10_A")
		self.assertEqual(price[4], "6492000")
		scrap = next(r for r in table_rows(merged["refscrapofpackageitem.txt"]) if r[2] == "PACKAGE_ITEM_CH_BLADE_10_A")
		self.assertEqual(scrap[3], "ITEM_CH_BLADE_10_A")

	def test_reentry_stocks_nothing_twice(self):
		raw = {name: table_rows(tables(GOODS)[name]) for name in d10.NAMES}
		# The city's own accessory store, restocked in place by the pass:
		# the re-entry case the cloned stores are already guarded against.
		raw["refshop.txt"].append(["1", "11", "6", "STORE_SD_ACCESSORY", *FILLER])
		raw["refmappingshopwithtab.txt"].append(["1", "11", "STORE_SD_ACCESSORY", "STORE_SD_ACCESSORY_GROUP1"])
		raw["refshoptabgroup.txt"].append(["1", "11", "8", "STORE_SD_ACCESSORY_GROUP1", "SN_GROUP"])
		raw["refshoptab.txt"].append(["1", "11", "10", "STORE_SD_ACCESSORY_TAB1", "STORE_SD_ACCESSORY_GROUP1", "SN_TAB_ACCESSORY", ""])
		raw["refshopgoods.txt"].append(["1", "11", "STORE_SD_ACCESSORY_TAB1", "PACKAGE_ITEM_CH_RING_09_A", "0", *FILLER])
		raw["refpackageitem.txt"].append(["1", "11", "30", "PACKAGE_ITEM_CH_RING_09_A", "0", "EXPAND_TERM_ALL", "SN", "SN_TT", "old.ddj", *FILLER])
		raw["refscrapofpackageitem.txt"].append(["1", "11", "PACKAGE_ITEM_CH_RING_09_A", "ITEM_CH_RING_09_A", "0", "0", "120", *["0"] * 13, *FILLER, "200"])
		raw["refpricepolicyofitem.txt"].append(["1", "11", "PACKAGE_ITEM_CH_RING_09_A", "1", "1000", *FILLER])
		items = {**ITEMS, "ITEM_CH_RING_10_A": item("ITEM_CH_RING_10_A", 100)}
		merged, _ = d10.merge_d10_stock({name: utf16(r) for name, r in raw.items()}, items)
		accessory = ["PACKAGE_ITEM_CH_RING_09_A", "PACKAGE_ITEM_CH_RING_10_A"]
		self.assertEqual([r[3] for r in table_rows(merged["refshopgoods.txt"]) if r[2] == "STORE_SD_ACCESSORY_TAB1"], accessory)
		again, summary = d10.merge_d10_stock(merged, items)
		self.assertEqual(summary["goods"], 0)
		self.assertEqual(summary["packages"], 0)
		self.assertEqual([r[3] for r in table_rows(again["refshopgoods.txt"]) if r[2] == "STORE_SD_ACCESSORY_TAB1"], accessory)

	def test_without_the_degree_10_items_the_new_store_is_a_plain_copy(self):
		merged, summary = self.merge({})
		self.assertEqual(summary["packages"], 0)
		goods = [r for r in table_rows(merged["refshopgoods.txt"]) if r[2] == "STORE_SD_SMITH_TAB1"]
		self.assertEqual([r[3] for r in goods], GOODS)


if __name__ == "__main__":
	unittest.main()
