"""
===========================================================================

test_alexandria_shop_honor.py - Alexandria's own degree 9 honor store

Synthetic shop tables in the shape the variant ships (#3548): both SD
honor groups map onto Hotan's honor store. The pass gives the city its
own store, its degree 8 goods become their native degree 9 twins, the
European degree 9 honor items gain packages minted from the Chinese
package of their grade, Hotan's store is untouched, and a second run
changes nothing. With the flag off the services step never calls it.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import alexandria_shop_d10 as d10  # noqa: E402
import alexandria_shop_honor as honor  # noqa: E402
from import_vsro_alexandria import table_rows  # noqa: E402

FILLER = ["-1", "xxx"] * 4
STONE_08 = honor.REPAIR_STONE + "08"
STONE_09 = honor.REPAIR_STONE + "09"
CH_08 = ["PACKAGE_ITEM_CH_BLADE_08_A_RARE_HONOR", "PACKAGE_ITEM_CH_BLADE_08_C_RARE_HONOR"]
CH_09 = ["PACKAGE_ITEM_CH_BLADE_09_A_RARE_HONOR", "PACKAGE_ITEM_CH_BLADE_09_C_RARE_HONOR"]
# Each grade's honor price and the degree 9 magic parameter, as native.
HONOR_PRICE = {"A": "4000", "C": "8000"}
MAGIC_09 = "25769804011"


def utf16(rows):
	return b"\xff\xfe" + ("\r\n".join("\t".join(row) for row in rows) + "\r\n").encode("utf-16-le")


def item(code, price, durability):
	row = ["0"] * 120
	row[0], row[1], row[2], row[5], row[6] = "1", "9", code, "SN_" + code, "SN_" + code + "_TT_DESC"
	row[10] = "item\\europe\\weapon\\axe_09.ddj"
	row[d10.PRICE_COLUMN] = str(price)
	row[honor.DURABILITY_COLUMN] = f"{durability}.0"
	return row


ITEMS = {
	"ITEM_EU_AXE_09_A_RARE_HONOR": item("ITEM_EU_AXE_09_A_RARE_HONOR", 5800000, 84),
	"ITEM_EU_AXE_09_C_RARE_HONOR": item("ITEM_EU_AXE_09_C_RARE_HONOR", 9500000, 90),
	# Already packaged natively; never minted again.
	"ITEM_CH_BLADE_09_A_RARE_HONOR": item("ITEM_CH_BLADE_09_A_RARE_HONOR", 5800000, 74)
}


def scrap(code, at, data="0", magic="0"):
	return ["1", "11", code, code.replace("PACKAGE_ITEM_", "ITEM_", 1), "0", "0", data, "1", magic, *["0"] * 11, *FILLER, str(at)]


def price_rows(code, gold, points):
	return [["1", "11", code, "1", gold, *FILLER], ["1", "11", code, "32", points, *FILLER]]


def tables():
	codes = CH_08 + CH_09 + [STONE_08, STONE_09]
	packages = [["1", "11", str(20 + i), code, "0", "EXPAND_TERM_ALL", "SN", "SN_TT", "old.ddj", *FILLER] for i, code in enumerate(codes)]
	scraps = [scrap(code, 100 + i, "74", MAGIC_09 if "_09_" in code else "0") for i, code in enumerate(codes)]
	prices = []
	for code in CH_09:
		prices += price_rows(code, "5800000", HONOR_PRICE[code.split("_")[5]])
	prices += price_rows(STONE_09, "50000", "15")
	return {
		"refshop.txt": utf16([["1", "11", "5", "STORE_KT_HONOR", *FILLER]]),
		"refmappingshopgroup.txt": utf16([
			["1", "11", "GROUP_STORE_KT_HONOR", "STORE_KT_HONOR"],
			["1", "11", "GROUP_STORE_SD_HONOR", "STORE_KT_HONOR"],
			["1", "11", "GROUP_STORE_SD_HONOR2", "STORE_KT_HONOR"]
		]),
		"refmappingshopwithtab.txt": utf16([["1", "11", "STORE_KT_HONOR", "STORE_KT_HONOR_GROUP1"]]),
		"refshoptabgroup.txt": utf16([["1", "11", "7", "STORE_KT_HONOR_GROUP1", "SN_STORE_HONOR_GROUP1"]]),
		"refshoptab.txt": utf16([
			["1", "11", "9", "STORE_KT_HONOR_TAB1", "STORE_KT_HONOR_GROUP1", "SN_TAB_HONOR"],
			["1", "11", "10", "STORE_KT_HONOR_TAB2", "STORE_KT_HONOR_GROUP1", "SN_TAB_HONOR2"]
		]),
		"refshopgoods.txt": utf16(
			[["1", "11", "STORE_KT_HONOR_TAB1", code, str(slot), *FILLER] for slot, code in enumerate(CH_08)]
			+ [["1", "11", "STORE_KT_HONOR_TAB2", STONE_08, "0", *FILLER]]
		),
		"refpackageitem.txt": utf16(packages),
		"refscrapofpackageitem.txt": utf16(scraps),
		"refpricepolicyofitem.txt": utf16(prices)
	}


def goods_of(merged, tab):
	return [r[3] for r in sorted((r for r in table_rows(merged["refshopgoods.txt"]) if r[2] == tab), key=lambda r: int(r[4]))]


def rows_for(merged, name, code, column=2):
	return [r for r in table_rows(merged[name]) if len(r) > column and r[column] == code]


class StockTests(unittest.TestCase):
	def test_degree_8_goods_become_their_degree_9_twins(self):
		known = set(CH_08 + CH_09 + [STONE_08, STONE_09])
		stock, mint, missing, full = honor.next_stock(CH_08, {}, known)
		self.assertEqual((stock, mint, missing, full), (CH_09, [], [], []))
		self.assertEqual(honor.next_stock([STONE_08], {}, known)[0], [STONE_09])

	def test_a_twin_without_a_package_keeps_the_good_and_is_reported(self):
		stock, _, missing, _ = honor.next_stock(CH_08, {}, set(CH_08))
		self.assertEqual(stock, CH_08)
		self.assertEqual(missing, CH_09)

	def test_only_a_weapon_tab_gains_the_unpackaged_honor_items(self):
		known = {STONE_08, STONE_09}
		self.assertEqual(honor.next_stock([STONE_08], ITEMS, known)[1], [])
		stock, mint, _, _ = honor.next_stock(CH_09, ITEMS, set(CH_09))
		self.assertEqual(mint, ["PACKAGE_ITEM_EU_AXE_09_A_RARE_HONOR", "PACKAGE_ITEM_EU_AXE_09_C_RARE_HONOR"])
		self.assertEqual(stock, CH_09 + mint)

	def test_a_tab_never_passes_the_native_maximum(self):
		goods = [f"PACKAGE_ITEM_CH_W{chr(65 + n // 26)}{chr(65 + n % 26)}_09_A_RARE_HONOR" for n in range(d10.MAX_TAB_SLOTS - 1)]
		stock, mint, _, full = honor.next_stock(goods, ITEMS, set(goods) | {"PACKAGE_ITEM_CH_BLADE_09_A_RARE_HONOR"})
		self.assertEqual(len(stock), d10.MAX_TAB_SLOTS)
		self.assertEqual(len(mint), 1)
		self.assertEqual(full, ["PACKAGE_ITEM_EU_AXE_09_C_RARE_HONOR"])


class MergeTests(unittest.TestCase):
	def setUp(self):
		self.raw = tables()
		self.merged, self.summary = honor.merge_honor_store(self.raw, ITEMS)

	def test_both_sd_groups_move_to_the_citys_own_store(self):
		mapping = {r[2]: r[3] for r in table_rows(self.merged["refmappingshopgroup.txt"])}
		self.assertEqual(mapping["GROUP_STORE_SD_HONOR"], honor.HONOR_STORE)
		self.assertEqual(mapping["GROUP_STORE_SD_HONOR2"], honor.HONOR_STORE)
		self.assertEqual(mapping["GROUP_STORE_KT_HONOR"], "STORE_KT_HONOR")
		self.assertEqual(self.summary["stores"], [honor.HONOR_STORE])

	def test_the_city_sells_the_degree_9_line_and_hotan_keeps_degree_8(self):
		self.assertEqual(goods_of(self.merged, "STORE_SD_HONOR_TAB1"), CH_09 + [
			"PACKAGE_ITEM_EU_AXE_09_A_RARE_HONOR", "PACKAGE_ITEM_EU_AXE_09_C_RARE_HONOR"
		])
		self.assertEqual(goods_of(self.merged, "STORE_SD_HONOR_TAB2"), [STONE_09])
		self.assertEqual(goods_of(self.merged, "STORE_KT_HONOR_TAB1"), CH_08)
		self.assertEqual(goods_of(self.merged, "STORE_KT_HONOR_TAB2"), [STONE_08])

	def test_a_minted_package_takes_its_grades_honor_price_and_its_own_gold(self):
		code = "PACKAGE_ITEM_EU_AXE_09_C_RARE_HONOR"
		prices = {r[3]: r[4] for r in rows_for(self.merged, "refpricepolicyofitem.txt", code)}
		self.assertEqual(prices, {"1": "9500000", "32": HONOR_PRICE["C"]})
		[package] = rows_for(self.merged, "refpackageitem.txt", code, 3)
		self.assertEqual(package[6:9], ["SN_ITEM_EU_AXE_09_C_RARE_HONOR", "SN_ITEM_EU_AXE_09_C_RARE_HONOR_TT_DESC", "item\\europe\\weapon\\axe_09.ddj"])
		[minted] = rows_for(self.merged, "refscrapofpackageitem.txt", code)
		self.assertEqual(minted[3], "ITEM_EU_AXE_09_C_RARE_HONOR")
		self.assertEqual(minted[honor.SCRAP_DATA_COLUMN], "90")
		self.assertEqual(minted[8], MAGIC_09)
		ids = [r[2] for r in table_rows(self.merged["refpackageitem.txt"])]
		self.assertEqual(len(ids), len(set(ids)))

	def test_a_native_package_is_never_minted_again(self):
		self.assertEqual(self.summary["packages"], 2)
		self.assertEqual(len(rows_for(self.merged, "refpackageitem.txt", "PACKAGE_ITEM_CH_BLADE_09_A_RARE_HONOR", 3)), 1)
		self.assertEqual(self.summary["missing"], [])

	def test_a_second_run_changes_nothing(self):
		again, summary = honor.merge_honor_store({**self.raw, **self.merged}, ITEMS)
		for name in d10.NAMES:
			self.assertEqual(table_rows(again[name]), table_rows(self.merged[name]), name)
		self.assertEqual(summary["packages"], 0)


if __name__ == "__main__":
	unittest.main()
