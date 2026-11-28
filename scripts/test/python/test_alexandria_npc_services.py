"""
===========================================================================
test_alexandria_npc_services.py - complete city shop and map dependencies
===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sys
from pathlib import Path
import unittest

import alexandria_npc_services as services
import alexandria_shop_d10 as d10
import alexandria_npcs as npcs
import import_vsro_alexandria as tables
import shop_layout


# ================
# raw
# ================
def raw(rows):
	return b"\xff\xfe" + npcs.table_text(rows).encode("utf-16-le")


# ================
# later
#
# A row of table name as the later clients ship it: the given cells,
# then "0" to the later width (v1.150's columns and the later extras).
# ================
def later(name, *cells):
	width = len(shop_layout.LAYOUTS[name]) + len(shop_layout.LATER_EXTRA.get(name, ()))
	return list(cells) + ["0"] * (width - len(cells))


# ================
# native
#
# A row of table name in v1.150's width, padded with "0".
# ================
def native(name, *cells):
	return list(cells) + ["0"] * (len(shop_layout.LAYOUTS[name]) - len(cells))


# ================
# later_store
#
# One later store (group, shop, tab group and tab NEW_TAB) for NPC
# selling goods, a list of (package, scrap item codes, price rows as
# (device, previous cost, cost)).
# ================
def later_store(goods):
	source = {name: raw([]) for name in services.SHOP_TABLES}
	source.update({
		"refshopgroup.txt": raw([later("refshopgroup.txt", "1", "15", "1", "GROUP", "NPC")]),
		"refmappingshopgroup.txt": raw([later("refmappingshopgroup.txt", "1", "15", "GROUP", "STORE")]),
		"refshop.txt": raw([later("refshop.txt", "1", "15", "1", "STORE")]),
		"refmappingshopwithtab.txt": raw([later("refmappingshopwithtab.txt", "1", "15", "STORE", "TABS")]),
		"refshoptabgroup.txt": raw([later("refshoptabgroup.txt", "1", "15", "1", "TABS", "SN_GROUP")]),
		"refshoptab.txt": raw([["1", "15", "7", "NEW_TAB", "TABS", "SN_TAB", "a designer's note"]]),
		"refshopgoods.txt": raw([later("refshopgoods.txt", "1", "15", "NEW_TAB", code, str(slot))
			for slot, (code, _, _) in enumerate(goods)]),
		"refpackageitem.txt": raw([later("refpackageitem.txt", "1", "15", str(n + 2), code) for n, (code, _, _) in enumerate(goods)]),
		"refscrapofpackageitem.txt": raw([later("refscrapofpackageitem.txt", "1", "15", code, item)
			for code, scrap, _ in goods for item in scrap]),
		"refpricepolicyofitem.txt": raw([later("refpricepolicyofitem.txt", "1", "15", code, *price)
			for code, _, prices in goods for price in prices]),
	})
	return source


# ================
# empty_native
#
# The v1.150 shop tables holding nothing.
# ================
def empty_native():
	return {name: raw([]) for name in services.SHOP_TABLES}


# ================
# ServicesTests
# ================
class ServicesTests(unittest.TestCase):
	# ================
	# test_a_shared_goods_slot_moves_the_later_package
	#
	# Two packages in one tab slot: the first keeps it, the second takes
	# the tab's lowest free slot; in a full tab the second is dropped.
	# ================
	def test_a_shared_goods_slot_moves_the_later_package(self):
		goods = [["1", "15", "TAB", "A", "1"], ["1", "15", "TAB", "B", "1"], ["1", "15", "TAB", "C", "0"]]
		kept, report = services.unique_slots(goods)
		self.assertEqual([(r[3], r[4]) for r in kept], [("A", "1"), ("B", "2"), ("C", "0")])
		self.assertEqual(report, {"moved": ["TAB:1->2:B"], "dropped": []})
		full = [["1", "15", "FULL", f"P{s}", str(s)] for s in range(d10.MAX_TAB_SLOTS)]
		kept, report = services.unique_slots(full + [["1", "15", "FULL", "LATE", "3"]])
		self.assertEqual(len(kept), d10.MAX_TAB_SLOTS)
		self.assertEqual(report["dropped"], ["FULL:3:LATE"])

	# ================
	# test_shop_closure_keeps_native_prices_and_defers_missing_packages
	# ================
	def test_shop_closure_keeps_native_prices_and_defers_missing_packages(self):
		base = empty_native()
		base["refpackageitem.txt"] = raw([native("refpackageitem.txt", "1", "11", "1", "PRESENT")])
		base["refpricepolicyofitem.txt"] = raw([native("refpricepolicyofitem.txt", "1", "11", "PRESENT", "1", "10")])
		base["refshoptab.txt"] = raw([["1", "11", "7", "OLD_TAB", "OLD_GROUP", "SN_OLD"]])
		source = later_store([("PRESENT", ["ITEM_OK"], [("1", "0", "999")]), ("MISSING", ["ITEM_MISSING"], [("1", "0", "5")])])
		merged, report = services.merge_shops(base, source, {"ITEM_OK"}, {"NPC"})
		self.assertEqual(tables.table_rows(merged["refshopgoods.txt"])[0][3], "PRESENT")
		self.assertEqual(len(tables.table_rows(merged["refshopgoods.txt"])), 1)
		self.assertEqual(merged["refpricepolicyofitem.txt"], base["refpricepolicyofitem.txt"])
		self.assertNotEqual(tables.table_rows(merged["refshoptab.txt"])[-1][2], "7")
		self.assertEqual(report["deferredPackages"], {"MISSING": ["ITEM_MISSING"]})
		self.assertEqual(services.merge_shops(merged, source, {"ITEM_OK"}, {"NPC"})[0], merged)

	# ================
	# test_an_imported_price_is_the_cost_not_the_previous_cost
	#
	# #4704: a later price row carries PreviousCost (0) before Cost. The
	# imported row is v1.150's 13 columns with the cost where the client
	# and the server read it, and the shop tab loses its note column.
	# ================
	def test_an_imported_price_is_the_cost_not_the_previous_cost(self):
		source = later_store([("PACKAGE_A_10", ["ITEM_A_10"], [("1", "0", "63656")])])
		merged, _ = services.merge_shops(empty_native(), source, {"ITEM_A_10"}, {"NPC"})
		price = tables.table_rows(merged["refpricepolicyofitem.txt"])
		self.assertEqual([len(r) for r in price], [len(shop_layout.LAYOUTS["refpricepolicyofitem.txt"])])
		self.assertEqual(price[0][2:5], ["PACKAGE_A_10", "1", "63656"])
		tabs = tables.table_rows(merged["refshoptab.txt"])
		self.assertEqual([r[3:6] for r in tabs], [["NEW_TAB", "TABS", "SN_TAB"]])
		self.assertEqual({len(r) for r in tabs}, {6})

	# ================
	# test_a_coin_price_is_dropped_and_reported
	#
	# The v1.150 client holds prices for PaymentDevice bits 0..5 only
	# (CRefPackageItemData_AddPrice 0x812990): a coin price (64, 128) is
	# kept out and reported, the package's gold price is imported.
	# ================
	def test_a_coin_price_is_dropped_and_reported(self):
		source = later_store([("PACKAGE_RING", ["ITEM_RING"], [("1", "0", "100000"), ("64", "0", "9"), ("128", "0", "5")])])
		merged, report = services.merge_shops(empty_native(), source, {"ITEM_RING"}, {"NPC"})
		self.assertEqual([r[3:5] for r in tables.table_rows(merged["refpricepolicyofitem.txt"])], [["1", "100000"]])
		self.assertEqual(report["unpayablePrices"], {"PACKAGE_RING": ["128", "64"]})

	# ================
	# test_a_package_of_an_item_with_no_use_yet_waits
	#
	# A package of socket stones only (no socket alchemy yet, #3911) is
	# neither listed nor priced; the reason is reported.
	# ================
	def test_a_package_of_an_item_with_no_use_yet_waits(self):
		stone = "ITEM_ETC_SOCKET_STONE_HEAL_E"
		source = later_store([("PACKAGE_" + stone, [stone], [("1", "0", "100000")]), ("PACKAGE_RING", ["ITEM_RING"], [("1", "0", "7")])])
		merged, report = services.merge_shops(empty_native(), source, {stone, "ITEM_RING"}, {"NPC"})
		self.assertEqual([r[3] for r in tables.table_rows(merged["refshopgoods.txt"])], ["PACKAGE_RING"])
		self.assertEqual([r[2] for r in tables.table_rows(merged["refpricepolicyofitem.txt"])], ["PACKAGE_RING"])
		self.assertEqual(report["pendingUse"], {"PACKAGE_" + stone: "#3911 socket alchemy"})

	# ================
	# test_a_row_v1150_cannot_hold_raises
	#
	# A width neither layout has, or a PreviousCost v1.150 would lose,
	# stops the build instead of being cut to fit.
	# ================
	def test_a_row_v1150_cannot_hold_raises(self):
		short = ["1", "15", "PACKAGE_A", "1", "999"]
		discounted = later("refpricepolicyofitem.txt", "1", "15", "PACKAGE_A", "1", "120", "100")
		for row in (short, discounted):
			source = later_store([])
			source["refpricepolicyofitem.txt"] = raw([row])
			with self.assertRaises(ValueError):
				services.merge_shops(empty_native(), source, set(), {"NPC"})


if __name__ == "__main__":
	unittest.main()
