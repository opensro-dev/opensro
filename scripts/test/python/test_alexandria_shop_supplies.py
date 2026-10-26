"""
===========================================================================

test_alexandria_shop_supplies.py - band consumables and alchemy (#1748)

Synthetic shop tables, no game data: the city's herbalist gets a potion
store of its own with the band grades, Khotan's store is left alone, the
alchemy bands land on the accessory store's alchemy tabs, a missing item
adds nothing, and without the band items no alchemy is stocked.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import alexandria_shop_d10 as d10  # noqa: E402
import alexandria_shop_supplies as supplies  # noqa: E402
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

POTION_GOODS = ["PACKAGE_ITEM_ETC_HP_POTION_05", "PACKAGE_ITEM_ETC_CURE_ALL_03"]
TEMPLATE_PACKAGES = [
	"PACKAGE_ITEM_ETC_HP_POTION_05", "PACKAGE_ITEM_ETC_CURE_ALL_03", "PACKAGE_ITEM_ETC_CURE_RANDOM_03",
	"PACKAGE_ITEM_ETC_DETECT_03", "PACKAGE_ITEM_ETC_ALL_POTION_03",
	"PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_09", "PACKAGE_ITEM_ETC_ELEMENT_FIRE_09",
	"PACKAGE_ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09"
]
ALCHEMY_GOODS = ["PACKAGE_ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09"]


def tables(potion_goods=POTION_GOODS, alchemy_goods=ALCHEMY_GOODS):
	packages = [["1", "11", str(10 + i), code, "0", "EXPAND_TERM_ALL", "SN", "SN_TT", "old.ddj", *FILLER] for i, code in enumerate(TEMPLATE_PACKAGES)]
	scraps = [["1", "11", c, c.replace("PACKAGE_ITEM_", "ITEM_", 1), "0", "0", "120", *["0"] * 13, *FILLER, str(100 + i)] for i, c in enumerate(TEMPLATE_PACKAGES)]
	prices = [["1", "11", c, "1", "1000", *FILLER] for c in TEMPLATE_PACKAGES]
	return {
		"refshop.txt": utf16([
			["1", "11", "5", "STORE_KT_POTION", *FILLER],
			["1", "11", "6", "STORE_SD_ACCESSORY", *FILLER]
		]),
		"refmappingshopgroup.txt": utf16([
			["1", "11", "GROUP_STORE_KT_POTION", "STORE_KT_POTION"],
			["1", "11", "GROUP_STORE_SD_M_POTION", "STORE_KT_POTION"],
			["1", "11", "GROUP_STORE_SD_M_ACCESSORY", "STORE_SD_ACCESSORY"]
		]),
		"refmappingshopwithtab.txt": utf16([
			["1", "11", "STORE_KT_POTION", "STORE_KT_POTION_GROUP1"],
			["1", "11", "STORE_SD_ACCESSORY", "STORE_SD_ACCESSORY_GROUP1"]
		]),
		"refshoptabgroup.txt": utf16([
			["1", "11", "7", "STORE_KT_POTION_GROUP1", "SN_GROUP"],
			["1", "11", "8", "STORE_SD_ACCESSORY_GROUP1", "SN_GROUP"]
		]),
		"refshoptab.txt": utf16([
			["1", "11", "9", "STORE_KT_POTION_TAB1", "STORE_KT_POTION_GROUP1", "SN_TAB_POTION", ""],
			["1", "11", "10", "STORE_SD_ACCESSORY_TAB1", "STORE_SD_ACCESSORY_GROUP1", "SN_TAB_ACCESSORY", ""],
			["1", "11", "11", "STORE_SD_ACCESSORY_TAB3", "STORE_SD_ACCESSORY_GROUP1", "SN_TAB_ALCHEMY", ""]
		]),
		"refshopgoods.txt": utf16(
			[["1", "11", "STORE_KT_POTION_TAB1", code, str(slot), *FILLER] for slot, code in enumerate(potion_goods)]
			+ [["1", "11", "STORE_SD_ACCESSORY_TAB3", code, str(slot), *FILLER] for slot, code in enumerate(alchemy_goods)]
		),
		"refpackageitem.txt": utf16(packages),
		"refscrapofpackageitem.txt": utf16(scraps),
		"refpricepolicyofitem.txt": utf16(prices)
	}


ITEMS = {
	"ITEM_ETC_CURE_ALL_04": item("ITEM_ETC_CURE_ALL_04", 260),
	"ITEM_ETC_CURE_RANDOM_04": item("ITEM_ETC_CURE_RANDOM_04", 100000),
	"ITEM_ETC_DETECT_04": item("ITEM_ETC_DETECT_04", 50000),
	"ITEM_ETC_ALL_POTION_04": item("ITEM_ETC_ALL_POTION_04", 600),
	"ITEM_ETC_ALL_POTION_05": item("ITEM_ETC_ALL_POTION_05", 900),
	"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_10": item("ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_10", 224000),
	"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_11": item("ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_11", 224000),
	"ITEM_ETC_ELEMENT_FIRE_10": item("ITEM_ETC_ELEMENT_FIRE_10", 903),
	"ITEM_ETC_ELEMENT_FIRE_11": item("ITEM_ETC_ELEMENT_FIRE_11", 1035),
	"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_10": item("ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_10", 63656),
	"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_11": item("ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_11", 70875)
}


def goods_of(merged, tab):
	return [r for r in table_rows(merged["refshopgoods.txt"]) if r[2] == tab]


class BandTests(unittest.TestCase):
	def test_the_alchemy_band_has_37_codes_per_degree(self):
		for degree in ("10", "11"):
			codes = supplies.alchemy_items(degree)
			self.assertEqual(len(codes), 37)
			self.assertEqual(len(set(codes)), 37)
			self.assertTrue(all(c.endswith("_" + degree) for c in codes))

	def test_lower_grade_of_walks_down_one_grade(self):
		self.assertEqual(supplies.lower_grade_of("PACKAGE_ITEM_ETC_CURE_ALL_04"), "PACKAGE_ITEM_ETC_CURE_ALL_03")
		self.assertIsNone(supplies.lower_grade_of("PACKAGE_ITEM_ETC_ARROW"))


class MergeTests(unittest.TestCase):
	def merge(self, items=ITEMS, **kwargs):
		return supplies.merge_supplies(tables(**kwargs), items)

	def test_the_city_gets_a_potion_store_of_its_own_and_khotans_stays_unchanged(self):
		raw = tables()
		merged, summary = supplies.merge_supplies(raw, ITEMS)
		self.assertEqual(summary["store"], "STORE_SD_POTION")
		mapping = {tuple(r[2:]) for r in table_rows(merged["refmappingshopgroup.txt"])}
		self.assertIn(("GROUP_STORE_SD_M_POTION", "STORE_SD_POTION"), mapping)
		self.assertIn(("GROUP_STORE_KT_POTION", "STORE_KT_POTION"), mapping)
		self.assertEqual([r[3] for r in goods_of(merged, "STORE_KT_POTION_TAB1")], POTION_GOODS)

	def test_the_copied_tab_gains_the_band_grades_with_consecutive_slots(self):
		merged, summary = self.merge()
		goods = goods_of(merged, "STORE_SD_POTION_TAB1")
		self.assertEqual([r[4] for r in goods], [str(n) for n in range(len(goods))])
		self.assertEqual([r[3] for r in goods], POTION_GOODS + list(supplies.CONSUMABLE_PACKAGES))
		self.assertEqual(summary["goods"], 3 + 3 + 3)

	def test_a_minted_package_takes_the_item_price_names_and_a_fresh_id(self):
		merged, _ = self.merge()
		package = next(r for r in table_rows(merged["refpackageitem.txt"]) if r[3] == "PACKAGE_ITEM_ETC_CURE_ALL_04")
		ids = [r[2] for r in table_rows(merged["refpackageitem.txt"])]
		self.assertEqual(len(ids), len(set(ids)))
		self.assertEqual(package[6:8], ["SN_ITEM_ETC_CURE_ALL_04", "SN_ITEM_ETC_CURE_ALL_04_TT_DESC"])
		price = next(r for r in table_rows(merged["refpricepolicyofitem.txt"]) if r[2] == "PACKAGE_ITEM_ETC_CURE_ALL_04")
		self.assertEqual(price[4], "260")
		scrap = next(r for r in table_rows(merged["refscrapofpackageitem.txt"]) if r[2] == "PACKAGE_ITEM_ETC_CURE_ALL_04")
		self.assertEqual(scrap[3], "ITEM_ETC_CURE_ALL_04")

	def test_degree_10_lands_on_the_alchemy_tab_and_degree_11_on_a_second_one(self):
		merged, summary = self.merge()
		self.assertEqual(summary["alchemyTab"], "STORE_SD_ACCESSORY_TAB3")
		ten = [r[3] for r in goods_of(merged, "STORE_SD_ACCESSORY_TAB3")]
		self.assertEqual(ten, ALCHEMY_GOODS + [
			"PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_10",
			"PACKAGE_ITEM_ETC_ELEMENT_FIRE_10",
			"PACKAGE_ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_10"
		])
		tabs = {r[3]: r for r in table_rows(merged["refshoptab.txt"])}
		self.assertIn("STORE_SD_ACCESSORY_TAB4", tabs)
		self.assertEqual(tabs["STORE_SD_ACCESSORY_TAB4"][5], "SN_TAB_ALCHEMY")
		eleven = goods_of(merged, "STORE_SD_ACCESSORY_TAB4")
		self.assertEqual([r[3] for r in eleven], [
			"PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_11",
			"PACKAGE_ITEM_ETC_ELEMENT_FIRE_11",
			"PACKAGE_ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_11"
		])
		self.assertEqual([r[4] for r in eleven], ["0", "1", "2"])

	def test_a_good_already_stocked_is_not_stocked_twice(self):
		merged, _ = self.merge(alchemy_goods=ALCHEMY_GOODS + ["PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_10"])
		ten = [r[3] for r in goods_of(merged, "STORE_SD_ACCESSORY_TAB3")]
		self.assertEqual(ten.count("PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_10"), 1)

	def test_without_the_band_items_no_alchemy_is_stocked_and_no_tab_is_authored(self):
		consumables_only = {k: v for k, v in ITEMS.items() if "_10" not in k and "_11" not in k}
		merged, summary = self.merge(consumables_only)
		self.assertEqual([r[3] for r in goods_of(merged, "STORE_SD_ACCESSORY_TAB3")], ALCHEMY_GOODS)
		self.assertNotIn("STORE_SD_ACCESSORY_TAB4", {r[3] for r in table_rows(merged["refshoptab.txt"])})
		self.assertEqual(summary["packages"], 3)

	def test_a_missing_item_is_reported_and_adds_nothing(self):
		items = {k: v for k, v in ITEMS.items() if k != "ITEM_ETC_DETECT_04"}
		merged, summary = self.merge(items)
		self.assertIn("PACKAGE_ITEM_ETC_DETECT_04", summary["missing"])
		self.assertNotIn("PACKAGE_ITEM_ETC_DETECT_04", [r[3] for r in goods_of(merged, "STORE_SD_POTION_TAB1")])

	def test_no_second_tab_is_authored_when_nothing_degree_11_can_be_minted(self):
		items = dict(ITEMS)
		items["ITEM_ETC_ARCHEMY_MAGICSTONE_HP_11"] = item("ITEM_ETC_ARCHEMY_MAGICSTONE_HP_11", 200000)
		for code in ("ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_11", "ITEM_ETC_ELEMENT_FIRE_11", "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_11"):
			del items[code]
		merged, summary = self.merge(items)
		self.assertNotIn("STORE_SD_ACCESSORY_TAB4", {r[3] for r in table_rows(merged["refshoptab.txt"])})
		self.assertIn("PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_HP_11", summary["missing"])

	def test_reentry_authors_no_second_degree_11_tab(self):
		merged, _ = self.merge()
		again, summary = supplies.merge_supplies(merged, ITEMS)
		tabs = {r[3] for r in table_rows(again["refshoptab.txt"])}
		self.assertEqual(tabs, {r[3] for r in table_rows(merged["refshoptab.txt"])})
		self.assertNotIn("STORE_SD_ACCESSORY_TAB5", tabs)
		for tab in ("STORE_SD_POTION_TAB1", "STORE_SD_ACCESSORY_TAB3", "STORE_SD_ACCESSORY_TAB4"):
			self.assertEqual([r[3] for r in goods_of(again, tab)], [r[3] for r in goods_of(merged, tab)])
		self.assertEqual(summary["goods"], 0)
		self.assertEqual(summary["packages"], 0)

	def test_a_tab_never_passes_the_native_maximum(self):
		full = [f"PACKAGE_ITEM_ETC_FILLER_{n:02d}" for n in range(44)]
		merged, _ = self.merge(alchemy_goods=full)
		self.assertEqual(len(goods_of(merged, "STORE_SD_ACCESSORY_TAB3")), d10.MAX_TAB_SLOTS)


if __name__ == "__main__":
	unittest.main()
