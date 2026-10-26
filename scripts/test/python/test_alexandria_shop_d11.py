"""
===========================================================================

test_alexandria_shop_d11.py - ordinary degree 11 gear in the city's shops

Synthetic shop tables in the shape the degree 10 pass leaves behind: a
degree 10 grade A good gains its degree 11 twin right after it when the
item exists, rare and set packages never gain one, other stores stay
untouched, a tab never passes the native maximum, and restocking a
restocked tab adds nothing.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import alexandria_shop_d10 as d10  # noqa: E402
import alexandria_shop_d11 as d11  # noqa: E402
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

SMITH_GOODS = ["PACKAGE_ITEM_CH_BLADE_09_A", "PACKAGE_ITEM_CH_BLADE_10_A", "PACKAGE_ITEM_CH_BOW_10_A", "PACKAGE_ITEM_ETC_AMMO_ARROW_01"]
ACCESSORY_GOODS = ["PACKAGE_ITEM_CH_RING_10_A"]
POTION_GOODS = ["PACKAGE_ITEM_ETC_HP_POTION_05"]
ALL_GOODS = SMITH_GOODS + ACCESSORY_GOODS + POTION_GOODS + ["PACKAGE_ITEM_CH_BLADE_09_A"]


def tables(smith_goods=SMITH_GOODS):
	codes = list(dict.fromkeys(smith_goods + ACCESSORY_GOODS + POTION_GOODS))
	packages = [["1", "11", str(10 + i), code, "0", "EXPAND_TERM_ALL", "SN", "SN_TT", "old.ddj", *FILLER] for i, code in enumerate(codes)]
	scraps = [["1", "11", c, c.replace("PACKAGE_ITEM_", "ITEM_", 1), "0", "0", "120", *["0"] * 13, *FILLER, str(100 + i)] for i, c in enumerate(codes)]
	prices = [["1", "11", c, "1", "1000", *FILLER] for c in codes]
	return {
		"refshop.txt": utf16([
			["1", "11", "5", "STORE_SD_SMITH", *FILLER],
			["1", "11", "6", "STORE_SD_ACCESSORY", *FILLER],
			["1", "11", "7", "STORE_SD_POTION", *FILLER]
		]),
		"refmappingshopgroup.txt": utf16([
			["1", "11", "GROUP_STORE_SD_M_SMITH", "STORE_SD_SMITH"]
		]),
		"refmappingshopwithtab.txt": utf16([
			["1", "11", "STORE_SD_SMITH", "STORE_SD_SMITH_GROUP1"],
			["1", "11", "STORE_SD_ACCESSORY", "STORE_SD_ACCESSORY_GROUP1"],
			["1", "11", "STORE_SD_POTION", "STORE_SD_POTION_GROUP1"]
		]),
		"refshoptabgroup.txt": utf16([
			["1", "11", "7", "STORE_SD_SMITH_GROUP1", "SN_GROUP"],
			["1", "11", "8", "STORE_SD_ACCESSORY_GROUP1", "SN_GROUP"],
			["1", "11", "9", "STORE_SD_POTION_GROUP1", "SN_GROUP"]
		]),
		"refshoptab.txt": utf16([
			["1", "11", "9", "STORE_SD_SMITH_TAB1", "STORE_SD_SMITH_GROUP1", "SN_TAB_WEAPON", ""],
			["1", "11", "10", "STORE_SD_ACCESSORY_TAB1", "STORE_SD_ACCESSORY_GROUP1", "SN_TAB_ACCESSORY", ""],
			["1", "11", "11", "STORE_SD_POTION_TAB1", "STORE_SD_POTION_GROUP1", "SN_TAB_POTION", ""]
		]),
		"refshopgoods.txt": utf16(
			[["1", "11", "STORE_SD_SMITH_TAB1", code, str(slot), *FILLER] for slot, code in enumerate(smith_goods)]
			+ [["1", "11", "STORE_SD_ACCESSORY_TAB1", code, str(slot), *FILLER] for slot, code in enumerate(ACCESSORY_GOODS)]
			+ [["1", "11", "STORE_SD_POTION_TAB1", code, str(slot), *FILLER] for slot, code in enumerate(POTION_GOODS)]
		),
		"refpackageitem.txt": utf16(packages),
		"refscrapofpackageitem.txt": utf16(scraps),
		"refpricepolicyofitem.txt": utf16(prices)
	}


ITEMS = {
	"ITEM_CH_BLADE_11_A": item("ITEM_CH_BLADE_11_A", 9142000),
	"ITEM_CH_BOW_11_A": item("ITEM_CH_BOW_11_A", 5000000),
	"ITEM_CH_RING_11_A": item("ITEM_CH_RING_11_A", 3738000),
	"ITEM_CH_BLADE_11_A_RARE": item("ITEM_CH_BLADE_11_A_RARE", 1),
	"ITEM_CH_SPEAR_11_SET_A": item("ITEM_CH_SPEAR_11_SET_A", 1)
}


def goods_of(merged, tab):
	return [r for r in table_rows(merged["refshopgoods.txt"]) if r[2] == tab]


class StockTests(unittest.TestCase):
	def test_a_degree_10_good_gains_its_degree_11_twin_right_after_it(self):
		stock, added = d11.next_stock(SMITH_GOODS, ITEMS)
		self.assertEqual(added, ["PACKAGE_ITEM_CH_BLADE_11_A", "PACKAGE_ITEM_CH_BOW_11_A"])
		self.assertEqual(stock, [
			"PACKAGE_ITEM_CH_BLADE_09_A", "PACKAGE_ITEM_CH_BLADE_10_A", "PACKAGE_ITEM_CH_BLADE_11_A",
			"PACKAGE_ITEM_CH_BOW_10_A", "PACKAGE_ITEM_CH_BOW_11_A", "PACKAGE_ITEM_ETC_AMMO_ARROW_01"
		])

	def test_a_missing_item_adds_nothing(self):
		stock, added = d11.next_stock(SMITH_GOODS, {})
		self.assertEqual((stock, added), (SMITH_GOODS, []))

	def test_a_good_already_stocked_is_never_stocked_twice(self):
		stock, added = d11.next_stock(SMITH_GOODS + ["PACKAGE_ITEM_CH_BLADE_11_A"], ITEMS)
		self.assertEqual(stock.count("PACKAGE_ITEM_CH_BLADE_11_A"), 1)
		self.assertNotIn("PACKAGE_ITEM_CH_BLADE_11_A", added)

	def test_rare_and_set_packages_never_gain_a_twin(self):
		goods = ["PACKAGE_ITEM_CH_BLADE_10_A_RARE", "PACKAGE_ITEM_CH_SPEAR_10_SET_A"]
		stock, added = d11.next_stock(goods, ITEMS)
		self.assertEqual((stock, added), (goods, []))

	def test_a_tab_never_passes_the_native_maximum_and_the_lowest_degrees_leave_first(self):
		goods = [f"PACKAGE_ITEM_CH_W{n:02d}_{degree:02d}_A" for n in range(20) for degree in (5, 10)]
		items = {f"ITEM_CH_W{n:02d}_11_A": item("x", 1) for n in range(20)}
		stock, _ = d11.next_stock(goods, items)
		self.assertEqual(len(stock), d10.MAX_TAB_SLOTS)
		self.assertEqual(len([p for p in stock if "_11_" in p]), 20)


class MergeTests(unittest.TestCase):
	def merge(self, items=ITEMS, **kwargs):
		return d11.merge_d11_stock(tables(**kwargs), items)

	def test_the_stores_gain_the_degree_11_goods_and_other_stores_stay_untouched(self):
		merged, summary = self.merge()
		self.assertEqual([r[3] for r in goods_of(merged, "STORE_SD_POTION_TAB1")], POTION_GOODS)
		smith = goods_of(merged, "STORE_SD_SMITH_TAB1")
		self.assertEqual([r[4] for r in smith], [str(n) for n in range(len(smith))])
		self.assertIn("PACKAGE_ITEM_CH_BLADE_11_A", [r[3] for r in smith])
		self.assertEqual([r[3] for r in goods_of(merged, "STORE_SD_ACCESSORY_TAB1")], ACCESSORY_GOODS + ["PACKAGE_ITEM_CH_RING_11_A"])
		self.assertEqual(summary["goods"], 3)

	def test_a_new_package_takes_the_item_price_names_and_a_fresh_id(self):
		merged, _ = self.merge()
		package = next(r for r in table_rows(merged["refpackageitem.txt"]) if r[3] == "PACKAGE_ITEM_CH_BLADE_11_A")
		ids = [r[2] for r in table_rows(merged["refpackageitem.txt"])]
		self.assertEqual(len(ids), len(set(ids)))
		self.assertEqual(package[6:8], ["SN_ITEM_CH_BLADE_11_A", "SN_ITEM_CH_BLADE_11_A_TT_DESC"])
		price = next(r for r in table_rows(merged["refpricepolicyofitem.txt"]) if r[2] == "PACKAGE_ITEM_CH_BLADE_11_A")
		self.assertEqual(price[4], "9142000")
		scrap = next(r for r in table_rows(merged["refscrapofpackageitem.txt"]) if r[2] == "PACKAGE_ITEM_CH_BLADE_11_A")
		self.assertEqual(scrap[3], "ITEM_CH_BLADE_11_A")

	def test_without_the_degree_11_items_nothing_is_stocked(self):
		merged, summary = self.merge({})
		self.assertEqual(summary, {"goods": 0, "packages": 0, "missing": []})
		self.assertEqual([r[3] for r in goods_of(merged, "STORE_SD_SMITH_TAB1")], SMITH_GOODS)

	def test_a_missing_template_stocks_nothing_and_is_reported(self):
		# The bow's degree 10 package has no price row, so its degree 11
		# twin has no template: never partly granted, always reported.
		raw = tables()
		raw["refpricepolicyofitem.txt"] = utf16(
			[r for r in table_rows(raw["refpricepolicyofitem.txt"]) if r[2] != "PACKAGE_ITEM_CH_BOW_10_A"])
		merged, summary = d11.merge_d11_stock(raw, ITEMS)
		smith = [r[3] for r in goods_of(merged, "STORE_SD_SMITH_TAB1")]
		self.assertIn("PACKAGE_ITEM_CH_BLADE_11_A", smith)
		self.assertNotIn("PACKAGE_ITEM_CH_BOW_11_A", smith)
		self.assertEqual(summary["missing"], ["PACKAGE_ITEM_CH_BOW_11_A"])
		self.assertNotIn("PACKAGE_ITEM_CH_BOW_11_A", [r[3] for r in table_rows(merged["refpackageitem.txt"])])
		for name in ("refscrapofpackageitem.txt", "refpricepolicyofitem.txt"):
			self.assertNotIn("PACKAGE_ITEM_CH_BOW_11_A", [r[2] for r in table_rows(merged[name])])

	def test_reentry_stocks_nothing_twice(self):
		merged, _ = self.merge()
		again, summary = d11.merge_d11_stock(merged, ITEMS)
		self.assertEqual(summary["goods"], 0)
		self.assertEqual(summary["packages"], 0)
		for tab in ("STORE_SD_SMITH_TAB1", "STORE_SD_ACCESSORY_TAB1"):
			self.assertEqual([r[3] for r in goods_of(again, tab)], [r[3] for r in goods_of(merged, tab)])


if __name__ == "__main__":
	unittest.main()
