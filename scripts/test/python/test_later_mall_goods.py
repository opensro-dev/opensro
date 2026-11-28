"""
===========================================================================

test_later_mall_goods.py - which v1.696 mall goods the tree lists

The selection rules of later_mall_goods.goods_rows, on hand-built tables
in the v1.696 layout: a good lists only when its tab, package, price,
scraps, items and summoned creature are all there, and a rental only
when its refrentitem term resolves to a period naming the scrap's own
item (its row is then written with the good); it keeps its later price,
drops the payments v1.150 never knew, and takes the tab's next slot and
a free package identity. A good in the later miscellaneous alchemy tab
the tree lacks lists in the Athanasia tab instead (never its
second-currency copy).

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest
from pathlib import Path
import tempfile

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
		tables["refpricepolicyofitem.txt"].append(["1", "11", package, payment, "0", silk if payment == "2" else "0"] + NONE)
	tables["refshopgoods.txt"].append(["1", "11", tab, package, str(slot)] + NONE)
	return package


def empty():
	return {name: [] for name in ("refpackageitem.txt", "refscrapofpackageitem.txt", "refpricepolicyofitem.txt", "refshopgoods.txt")}


# The tree's own identity of the rented Arabia dress: never the later
# client's 4242, as the Winter attachment is 29411 here and 28268 there.
TREE_DRESS_ID = "29411"


# ================
# tree_of
#
# A built tree holding items: a mapping names each one's identity,
# any other collection numbers them from 9000.
# ================
def tree_of(items, creatures=(), tabs=("MALL_ARCHEMY_ATHANASIA", "MALL_PET_B_SILK", "MALL_AVATAR_DRESS"), current=None, carry=()):
	ids = dict(items) if isinstance(items, dict) else {code: str(9000 + n) for n, code in enumerate(sorted(items))}
	return goods.TreeView(ids, set(creatures), current or empty(), set(tabs), set(carry))


# ================
# tree_rent_row
#
# The later rent row as the tree ships it: the same row, renting the
# tree's own identity of the item.
# ================
def tree_rent_row(rent_row):
	return rent_row[:2] + [TREE_DRESS_ID] + rent_row[3:]


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
	# test_a_gold_shop_package_joins_the_mall_on_its_own_package
	#
	# Alexandria's accessory shop sells the D10 Immortal stone for gold, so
	# the tree already holds its package. v1.696 also lists it in the mall
	# at 57 silk: the listing adds the mall goods row and the silk price
	# only, never a second package, scrap or gold row.
	# ================
	def test_a_gold_shop_package_joins_the_mall_on_its_own_package(self):
		current = empty()
		later_good(current, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09", 8, "51", 20112)
		package = later_good(current, "STORE_SD_ACCESSORY_EU_TAB3", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10", 12, "0", 202517,
			payments=("1",))
		later = empty()
		later_good(later, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10", 9, "57", 20113,
			payments=("2", "16"))
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10"}, current=current))
		self.assertEqual(summary["listed"], 1)
		self.assertEqual(summary["packages"], [])
		self.assertEqual(summary["joined"], {package: ["16", "2"]})
		self.assertEqual(rows["refpackageitem.txt"], [])
		self.assertEqual(rows["refscrapofpackageitem.txt"], [])
		self.assertEqual([row[2:5] for row in rows["refshopgoods.txt"]], [["MALL_ARCHEMY_ATHANASIA", package, "9"]])
		self.assertEqual([(row[3], row[4]) for row in rows["refpricepolicyofitem.txt"]], [("2", "57"), ("16", "0")])

	# ================
	# test_a_package_no_shop_lists_stays_the_layouts
	#
	# The Pegasus scroll's package is in the tree with no goods row: the
	# server's mall category layout sells it, so the import must not list
	# it a second time.
	# ================
	def test_a_package_no_shop_lists_stays_the_layouts(self):
		current = empty()
		later_good(current, "MALL_PET_C_VEHICLE", "ITEM_COS_C_PEGASUS_SCROLL", 0, "0", 22543, payments=())
		current["refshopgoods.txt"].clear()
		later = empty()
		later_good(later, "MALL_PET_C_VEHICLE", "ITEM_COS_C_PEGASUS_SCROLL", 4, "500", 3)
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_COS_C_PEGASUS_SCROLL"}, creatures=("COS_C_PEGASUS",),
			tabs=("MALL_PET_C_VEHICLE",), current=current))
		self.assertEqual(summary["listed"], 0)
		self.assertEqual(rows["refshopgoods.txt"], [])

	# ================
	# test_a_joined_listing_strips_without_touching_the_gold_shop
	#
	# A rerun first undoes the previous run's joined rows: the mall goods
	# row and the silk price go, the gold shop's goods and price stay.
	# ================
	def test_a_joined_listing_strips_without_touching_the_gold_shop(self):
		package = "PACKAGE_ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10"
		rows = [
			["1", "11", "STORE_SD_ACCESSORY_EU_TAB3", package, "12"],
			["1", "11", "MALL_ARCHEMY_ATHANASIA", package, "9"],
		]
		prices = [["1", "11", package, "16", "225750"], ["1", "11", package, "2", "57"]]
		with tempfile.TemporaryDirectory() as root:
			text = Path(root) / goods.TEXT_ROOT
			text.mkdir(parents=True)
			for name, table in (("refshopgoods.txt", rows), ("refpricepolicyofitem.txt", prices)):
				(text / name).write_bytes(b"\xff\xfe" + "".join("\t".join(row) + "\r\n" for row in table).encode("utf-16-le"))
			goods.strip_joined(Path(root), {package: ["2"]})
			left = {name: (text / name).read_bytes()[2:].decode("utf-16-le").split("\r\n")[:-1]
				for name in ("refshopgoods.txt", "refpricepolicyofitem.txt")}
		self.assertEqual(left["refshopgoods.txt"], ["\t".join(rows[0])])
		self.assertEqual(left["refpricepolicyofitem.txt"], ["\t".join(prices[0])])

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
	# test_a_miscellaneous_alchemy_good_lists_in_the_athanasia_tab
	#
	# MALL_ARCHEMY_ETC has no v1.150 tab (the owner's 2026-11-25 rule:
	# no new tab is carried for one tab's goods), so a good whose items
	# stand in the tree lists in MALL_ARCHEMY_ATHANASIA, the last tab
	# of the same alchemy group, after that tab's own goods; a good
	# whose item is absent stays out on the item, and the tab's
	# second-currency copy stays out on its shop.
	# ================
	def test_a_miscellaneous_alchemy_good_lists_in_the_athanasia_tab(self):
		current = empty()
		later_good(current, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09", 8, "51", 20112)
		later = empty()
		package = later_good(later, "MALL_ARCHEMY_ETC", "ITEM_ETC_ARCHEMY_RONDO_03", 0, "28", 51530)
		later_good(later, "MALL_ARCHEMY_ETC", "ITEM_MALL_GOOD_LUCK_CHARM_12_RARE", 1, "999", 51531)
		later_good(later, "PRE_MALL_ARCHEMY_ETC", "ITEM_PRE_MALL_GOOD_LUCK_CHARM_12_RARE", 0, "999", 51532)
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_ETC_ARCHEMY_RONDO_03"}, current=current))
		self.assertEqual(summary["packages"], [package])
		self.assertEqual(summary["remapped"], {package: "MALL_ARCHEMY_ATHANASIA"})
		self.assertEqual(rows["refshopgoods.txt"][0][2:5], ["MALL_ARCHEMY_ATHANASIA", package, "9"])
		self.assertEqual([(row[3], row[4]) for row in rows["refpricepolicyofitem.txt"]], [("2", "28")])
		self.assertEqual(sorted(summary["unlisted"]), sorted([
			"item not in the tree", "second-currency shop PRE_MALL_ARCHEMY_ETC"]))

	# ================
	# test_a_spirit_lists_in_its_carried_tab_at_its_later_price
	#
	# A Devil's Spirit (#4240 part 4): its tab is one the step may
	# carry over, so the good lists at the later 150 silk (the
	# second-currency rows stay behind) and the summary names the
	# tab for add_goods to write.
	# ================
	def test_a_spirit_lists_in_its_carried_tab_at_its_later_price(self):
		later = empty()
		package = later_good(later, "MALL_PREMIUM_NASRUN", "ITEM_MALL_AVATAR_M_NASRUN", 0, "150", 51522,
			payments=("2", "4", "2048"))
		rows, summary = goods.goods_rows(later,
			tree_of({"ITEM_MALL_AVATAR_M_NASRUN"}, carry=("MALL_PREMIUM_NASRUN",)))
		self.assertEqual(summary["packages"], [package])
		self.assertEqual(summary["tabs"], ["MALL_PREMIUM_NASRUN"])
		self.assertEqual(rows["refshopgoods.txt"][0][2:5], ["MALL_PREMIUM_NASRUN", package, "0"])
		self.assertEqual([(row[3], row[4]) for row in rows["refpricepolicyofitem.txt"]], [("2", "150"), ("4", "0")])

	# ================
	# test_a_carried_tab_without_its_item_stays_out
	#
	# The tab may be carried, but without the Spirit's item row the
	# good stays out and no tab is written for it.
	# ================
	def test_a_carried_tab_without_its_item_stays_out(self):
		later = empty()
		later_good(later, "MALL_PREMIUM_NASRUN", "ITEM_MALL_AVATAR_M_NASRUN", 0, "150", 51522)
		rows, summary = goods.goods_rows(later, tree_of(set(), carry=("MALL_PREMIUM_NASRUN",)))
		self.assertEqual(summary["listed"], 0)
		self.assertEqual(summary["tabs"], [])
		self.assertEqual(summary["unlisted"], {"item not in the tree": 1})
		self.assertTrue(all(not table for table in rows.values()))

	# ================
	# test_a_rental_lists_and_writes_its_rent_row
	#
	# The 1-day Arabia dress at 14 silk: its term resolves to a 1440
	# minute period naming the scrap's own item, so the good lists and
	# the term's refrentitem row is written for the server's clock,
	# renting the tree's identity of the dress, not the later client's.
	# ================
	def test_a_rental_lists_and_writes_its_rent_row(self):
		later = empty()
		package = later_good(later, "MALL_AVATAR_DRESS", "ITEM_MALL_AVATAR_M_ARABIA_2", 3, "14", 3,
			rent="RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D")
		rent_row = ["1", "RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D", "4242", "0", "0", "2",
			"NULL", "NULL", "1", "1440", "0", "0", "0", "0"]
		rows, summary = goods.goods_rows(later, tree_of({"ITEM_MALL_AVATAR_M_ARABIA_2": TREE_DRESS_ID}),
			rent_rows={rent_row[1]: rent_row}, item_by_id={"4242": "ITEM_MALL_AVATAR_M_ARABIA_2"})
		self.assertEqual(summary["packages"], [package])
		self.assertEqual(summary["rentals"], 1)
		self.assertEqual(summary["rentTerms"], ["RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D"])
		self.assertEqual(rows["refrentitem.txt"], [tree_rent_row(rent_row)])
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
	# goods table; its rent row ships with it, renting the tree's
	# identity of the item, or the mall load stops. Nothing else is
	# written: the good is already listed.
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
		rows, summary = goods.goods_rows(later,
			tree_of({"ITEM_MALL_AVATAR_M_ARABIA_2": TREE_DRESS_ID}, current=current),
			rent_rows={rent_row[1]: rent_row}, item_by_id={"4242": "ITEM_MALL_AVATAR_M_ARABIA_2"})
		self.assertEqual(summary["packages"], [])
		self.assertEqual(summary["rentals"], 1)
		self.assertEqual(summary["rentTerms"], ["RENT_ITEM_MALL_AVATAR_M_ARABIA_2_1D"])
		self.assertEqual(rows["refrentitem.txt"], [tree_rent_row(rent_row)])
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
	# test_a_gears_stone_never_lists
	#
	# The owner's 2026-11-28 decision (#4836): a later mall package
	# holding a gears magic stone stays out even when its tab, price
	# and item all stand in the tree; a stone of another family in the
	# same tab still lists.
	# ================
	def test_a_gears_stone_never_lists(self):
		later = empty()
		stone = "ITEM_ETC_ARCHEMY_LEVEL_MAGICSTONE_STR_05"
		later_good(later, "MALL_ARCHEMY_ATHANASIA", stone, 0, "30", 8)
		athanasia = later_good(later, "MALL_ARCHEMY_ATHANASIA", "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10", 1, "57", 9)
		rows, summary = goods.goods_rows(later, tree_of({stone, "ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_10"}))
		self.assertEqual(summary["packages"], [athanasia])
		self.assertNotIn("PACKAGE_" + stone, [row[3] for row in rows["refshopgoods.txt"]])
		self.assertEqual(list(summary["unlisted"]), ["gears stone, never sold for silk"])

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
