"""
===========================================================================

test_later_mall_goods.py - which v1.696 mall goods the tree lists

The selection rules of later_mall_goods.goods_rows, on hand-built tables
in the v1.696 layout: a good lists only when its tab, package, price,
scraps, items and summoned creature are all there, and a rental only
when its refrentitem term resolves to a period naming the scrap's own
item (its row is then written with the good); it keeps its later price,
drops the payments v1.150 never knew, and takes the tab's next slot and
a free package identity.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import later_mall_goods as goods  # noqa: E402

NONE = ["-1", "xxx"] * 4


# ================
# later_good / tree_of
# ================
def later_good(tables, tab, item, slot, silk, package_id, rent="xxx", payments=("2",)):
	package = "PACKAGE_" + item
	tables["refpackageitem.txt"].append(["1", "11", str(package_id), package, "0", "EXPAND_TERM_ALL", "SN_" + item, "SN_DESC", "icon.ddj"] + NONE)
	tables["refscrapofpackageitem.txt"].append(["1", "11", package, item] + ["0"] * 16 + ["-1", rent] + ["-1", "xxx"] * 3 + ["9" + str(package_id)])
	for payment in payments:
		tables["refpricepolicyofitem.txt"].append(["1", "11", package, payment, "0", silk if payment == "2" else "0", "-1", "xxx"])
	tables["refshopgoods.txt"].append(["1", "11", tab, package, str(slot)] + NONE)
	return package


def empty():
	return {name: [] for name in ("refpackageitem.txt", "refscrapofpackageitem.txt", "refpricepolicyofitem.txt", "refshopgoods.txt")}


def tree_of(items, creatures=(), tabs=("MALL_ARCHEMY_ATHANASIA", "MALL_PET_B_SILK", "MALL_AVATAR_DRESS"), current=None):
	return goods.TreeView(set(items), set(creatures), current or empty(), set(tabs))


# ================
# LaterMallGoodsTest
# ================
class LaterMallGoodsTest(unittest.TestCase):
	# ================
	# test_a_later_degree_lists_after_the_native_ones_at_its_later_price
	#
	# The D10 Immortal stone: v1.150 lists D1..D9 in slots 0..8, v1.696
	# adds D10 at 57 silk. It takes slot 9 and keeps its price; the
	# second-currency payment row (2048) does not travel.
	# ================
	def test_a_later_degree_lists_after_the_native_ones_at_its_later_price(self):
		current = empty()
		later_good(current, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09", 8, "51", 20112)
		later = empty()
		later_good(later, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09", 8, "51", 20112)
		package = later_good(later, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10", 9, "57", 20113,
			payments=("2", "4", "2048"))
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10"}, current=current))
		self.assertEqual(summary["packages"], [package])
		self.assertEqual(rows["refshopgoods.txt"][0][2:5], ["MALL_ARCHEMY_ATHANASIA", package, "9"])
		self.assertEqual([(row[3], row[4]) for row in rows["refpricepolicyofitem.txt"]], [("2", "57"), ("4", "0")])
		self.assertEqual(rows["refpackageitem.txt"][0][2], "20113")

	# ================
	# test_a_good_the_tree_cannot_serve_stays_out
	#
	# No item, no summoned creature, a rental term with no row behind it,
	# a tab the tree lacks, a second-currency shop: each is reported and
	# nothing of it is written.
	# ================
	def test_a_good_the_tree_cannot_serve_stays_out(self):
		later = empty()
		later_good(later, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_12", 11, "70", 1)
		later_good(later, "MALL_PET_B_SILK", "ITEM_COS_P_KANGAROO_SCROLL", 0, "130", 2)
		later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3, rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D")
		later_good(later, "MALL_PREMIUM_NASRUN", "ITEM_MALL_AVATAR_M_NASRUN", 0, "150", 4)
		later_good(later, "PRE_MALL_ARCHEMY_ATHANASIA", "ITEM_PRE_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10", 9, "57", 5)
		items = {"ITEM_COS_P_KANGAROO_SCROLL", "ITEM_MALL_AVATAR_M_ARABIA_2", "ITEM_MALL_AVATAR_M_NASRUN",
			"ITEM_PRE_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10"}
		rows, summary = goods.goods_rows(later, tree_of(items))
		self.assertEqual(summary["listed"], 0)
		self.assertTrue(all(not table for table in rows.values()))
		self.assertEqual(sorted(summary["unlisted"]), sorted([
			"item not in the tree", "creature not in the tree", "rent term without a row", "no tab MALL_PREMIUM_NASRUN",
			"second-currency shop PRE_MALL_ARCHEMY_ATHANASIA"]))

	# ================
	# test_a_rental_lists_and_writes_its_rent_row
	#
	# The 1-day Arabia dress at 14 silk: its term resolves to a 1440
	# minute period naming the scrap's own item, so the good lists and
	# the term's refrentitem row is written for the server's clock.
	# ================
	def test_a_rental_lists_and_writes_its_rent_row(self):
		later = empty()
		package = later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D")
		rent_row = ["1", "RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D", "4242", "0", "0", "2",
			"NULL", "NULL", "1", "1440", "0", "0", "0", "0"]
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_ARABIA_2"}),
			rent_rows={rent_row[1]: rent_row}, item_by_id={"4242": "ITEM_MALL_AVATAR_M_ARABIA_2"})
		self.assertEqual(summary["packages"], [package])
		self.assertEqual(summary["rentals"], 1)
		self.assertEqual(summary["rentTerms"], ["RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D"])
		self.assertEqual(rows["refrentitem.txt"], [rent_row])
		self.assertEqual(rows["refscrapofpackageitem.txt"][0][21], "RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D")

	# ================
	# test_a_rental_window_stays_out
	#
	# A fixed begin/end window instead of a period is the other native
	# rental kind; no mall good names one and the server takes periods
	# only, so the good is reported, never half listed.
	# ================
	def test_a_rental_window_stays_out(self):
		later = empty()
		later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_EVENT")
		rent_row = ["1", "RENT_ITEM_MALL_AVATAR_M_ARABIA_2_EVENT", "4242", "0", "0", "1",
			"2026-12-20 00:00:00", "2027-01-03 00:00:00", "0", "0", "0", "0", "0", "0"]
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_ARABIA_2"}),
			rent_rows={rent_row[1]: rent_row}, item_by_id={"4242": "ITEM_MALL_AVATAR_M_ARABIA_2"})
		self.assertEqual(summary["listed"], 0)
		self.assertEqual(summary["unlisted"], {"rental window": 1})
		self.assertTrue(all(not table for table in rows.values()))

	# ================
	# test_a_rent_term_naming_another_item_refuses_the_build
	#
	# The native load fatals on this data; the step raises instead of
	# listing the good under a period that belongs to another item.
	# ================
	def test_a_rent_term_naming_another_item_refuses_the_build(self):
		later = empty()
		later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D")
		rent_row = ["1", "RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D", "4242", "0", "0", "2",
			"NULL", "NULL", "1", "1440", "0", "0", "0", "0"]
		with self.assertRaisesRegex(ValueError, "names item 4242"):
			goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_ARABIA_2"}),
				rent_rows={rent_row[1]: rent_row}, item_by_id={"4242": "ITEM_MALL_AVATAR_M_OTHER"})

	# ================
	# test_a_staged_rental_ships_its_rent_row
	#
	# A good an earlier step already staged sells through the same
	# goods table; its rent row ships with it or the mall load stops.
	# Nothing else is written: the good is already listed.
	# ================
	def test_a_staged_rental_ships_its_rent_row(self):
		current = empty()
		later_good(current, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D")
		later = empty()
		later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D")
		rent_row = ["1", "RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D", "4242", "0", "0", "2",
			"NULL", "NULL", "1", "1440", "0", "0", "0", "0"]
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_ARABIA_2"}, current=current),
			rent_rows={rent_row[1]: rent_row}, item_by_id={"4242": "ITEM_MALL_AVATAR_M_ARABIA_2"})
		self.assertEqual(summary["packages"], [])
		self.assertEqual(summary["rentals"], 1)
		self.assertEqual(summary["rentTerms"], ["RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D"])
		self.assertEqual(rows["refrentitem.txt"], [rent_row])
		self.assertEqual(summary["unstage"], [])

	# ================
	# test_a_staged_rental_window_unstages
	#
	# A staged good whose term is a fixed window cannot be sold (the
	# server takes periods only); it is unstaged for add_goods to
	# strip instead of breaking the mall load.
	# ================
	def test_a_staged_rental_window_unstages(self):
		current = empty()
		later_good(current, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_EVENT")
		later = empty()
		package = later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_EVENT")
		rent_row = ["1", "RENT_ITEM_MALL_AVATAR_M_ARABIA_2_EVENT", "4242", "0", "0", "1",
			"2026-12-20 00:00:00", "2027-01-03 00:00:00", "0", "0", "0", "0", "0", "0"]
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_ARABIA_2"}, current=current),
			rent_rows={rent_row[1]: rent_row}, item_by_id={"4242": "ITEM_MALL_AVATAR_M_ARABIA_2"})
		self.assertEqual(summary["rentals"], 0)
		self.assertEqual(summary["unlisted"], {"rental window": 1})
		self.assertEqual(summary["unstage"], [package])
		self.assertFalse(rows["refrentitem.txt"])

	# ================
	# test_a_summon_scroll_lists_once_its_creature_is_in_the_tree
	# ================
	def test_a_summon_scroll_lists_once_its_creature_is_in_the_tree(self):
		later = empty()
		later_good(later, "MALL_PET_B_SILK", "ITEM_COS_P_GGLIDER_SCROLL", 0, "88", 7)
		_, summary = goods.goods_rows(later, tree_of({"ITEM_COS_P_GGLIDER_SCROLL"}, {"COS_P_GGLIDER"}))
		self.assertEqual(summary["listed"], 1)

	# ================
	# test_a_used_package_identity_moves_past_both_clients
	# ================
	def test_a_used_package_identity_moves_past_both_clients(self):
		current = empty()
		later_good(current, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_NATIVE", 0, "130", 500)
		later = empty()
		later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_LATER", 1, "130", 500)
		later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_OTHER", 2, "130", 900)
		rows, _ = goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_LATER"}, current=current))
		self.assertEqual(rows["refpackageitem.txt"][0][2], "901")
		self.assertEqual(rows["refshopgoods.txt"][0][4], "1")

	# ================
	# test_a_full_tab_refuses
	# ================
	def test_a_full_tab_refuses(self):
		current = empty()
		current["refshopgoods.txt"].append(["1", "11", "MALL_AVATAR_DRESS", "PACKAGE_X", str(goods.mall.MALL_SLOTS - 1)] + NONE)
		later = empty()
		later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_LATER", 0, "130", 1)
		with self.assertRaisesRegex(ValueError, "one-byte slot"):
			goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_LATER"}, current=current))


if __name__ == "__main__":
	unittest.main()
