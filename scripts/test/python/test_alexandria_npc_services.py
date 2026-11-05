"""
===========================================================================
test_alexandria_npc_services.py - complete city shop and map dependencies
===========================================================================
"""
import sys
from pathlib import Path
import unittest

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import alexandria_npc_services as services
import alexandria_npcs as npcs
import import_vsro_alexandria as tables


# ================
# raw
# ================
def raw(rows):
	return b"\xff\xfe" + npcs.table_text(rows).encode("utf-16-le")


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
		full = [["1", "15", "FULL", f"P{s}", str(s)] for s in range(services.MAX_TAB_SLOTS)]
		kept, report = services.unique_slots(full + [["1", "15", "FULL", "LATE", "3"]])
		self.assertEqual(len(kept), services.MAX_TAB_SLOTS)
		self.assertEqual(report["dropped"], ["FULL:3:LATE"])

	# ================
	# test_shop_closure_keeps_native_prices_and_defers_missing_packages
	# ================
	def test_shop_closure_keeps_native_prices_and_defers_missing_packages(self):
		base = {name: raw([]) for name in services.SHOP_TABLES}
		base["refpackageitem.txt"] = raw([["1", "11", "1", "PRESENT"]])
		base["refpricepolicyofitem.txt"] = raw([["1", "11", "PRESENT", "1", "10"]])
		base["refshoptab.txt"] = raw([["1", "11", "7", "OLD_TAB", "OLD_GROUP", "SN_OLD"]])
		source = {name: raw([]) for name in services.SHOP_TABLES}
		source.update({
			"refshopgroup.txt": raw([["1", "15", "1", "GROUP", "NPC"]]),
			"refmappingshopgroup.txt": raw([["1", "15", "GROUP", "STORE"]]),
			"refshop.txt": raw([["1", "15", "1", "STORE"]]),
			"refmappingshopwithtab.txt": raw([["1", "15", "STORE", "TABS"]]),
			"refshoptabgroup.txt": raw([["1", "15", "1", "TABS", "SN_GROUP"]]),
			"refshoptab.txt": raw([["1", "15", "7", "NEW_TAB", "TABS", "SN_TAB"]]),
			"refshopgoods.txt": raw([["1", "15", "NEW_TAB", "PRESENT", "0"], ["1", "15", "NEW_TAB", "MISSING", "1"]]),
			"refpackageitem.txt": raw([["1", "15", "2", "PRESENT"], ["1", "15", "3", "MISSING"]]),
			"refscrapofpackageitem.txt": raw([["1", "15", "PRESENT", "ITEM_OK"] + ["0"] * 16, ["1", "15", "MISSING", "ITEM_MISSING"] + ["0"] * 16]),
			"refpricepolicyofitem.txt": raw([["1", "15", "PRESENT", "1", "999"]])
		})
		merged, report = services.merge_shops(base, source, {"ITEM_OK"}, {"NPC"})
		self.assertEqual(tables.table_rows(merged["refshopgoods.txt"])[0][3], "PRESENT")
		self.assertEqual(len(tables.table_rows(merged["refshopgoods.txt"])), 1)
		self.assertEqual(merged["refpricepolicyofitem.txt"], base["refpricepolicyofitem.txt"])
		self.assertNotEqual(tables.table_rows(merged["refshoptab.txt"])[-1][2], "7")
		self.assertEqual(report["deferredPackages"], {"MISSING": ["ITEM_MISSING"]})
		self.assertEqual(services.merge_shops(merged, source, {"ITEM_OK"}, {"NPC"})[0], merged)

	# ================
	# test_city_labels_follow_the_page_projection_and_are_idempotent
	# ================
	def test_city_labels_follow_the_page_projection_and_are_idempotent(self):
		page = ["12", "1", "Alex", "map_alex.ddj", "512", "512", "512", "512", "512", "512", "1", "2", "2", "1", "0", "0", "0", "0", "0"]
		character = ["1", "10", "NPC_SD_T_AREA_ARMOR", "xxx", "xxx", "SN_ARMOR"]
		positions = raw([["10", "257", "960", "0", "960"]])
		labels, count = services.npc_map_labels(raw([page]), positions, [character], raw([]))
		self.assertEqual(count, 1)
		row = tables.table_rows(labels)[0]
		self.assertEqual(row[10:12], ["256", "256"])
		self.assertEqual(row[2], "SN_ARMOR")
		self.assertEqual(services.npc_map_labels(raw([page]), positions, [character], labels), (labels, 0))


if __name__ == "__main__":
	unittest.main()
