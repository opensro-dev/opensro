"""
===========================================================================

test_alexandria_shop_d10_rebuild.py - retained city stock after NPC restaging

The NPC importer restores source mappings on every build. Existing cloned
stores must reconnect without duplicating goods or changing Khotan's stock.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

from test_alexandria_shop_d10 import tables, GOODS, ITEMS, table_rows, d10


# ================
# RebuildTests
# ================
class RebuildTests(unittest.TestCase):
	# ================
	# test_existing_city_store_reconnects_after_source_npc_mapping_is_restored
	# ================
	def test_existing_city_store_reconnects_after_source_npc_mapping_is_restored(self):
		built, _ = d10.merge_d10_stock(tables(GOODS), ITEMS)
		old_goods = built["refshopgoods.txt"]
		built["refmappingshopgroup.txt"] = tables(GOODS)["refmappingshopgroup.txt"]
		rebuilt, report = d10.merge_d10_stock(built, ITEMS)
		links = {r[2]: r[3] for r in table_rows(rebuilt["refmappingshopgroup.txt"])}
		self.assertEqual(links["GROUP_STORE_SD_M_SMITH"], "STORE_SD_SMITH")
		self.assertEqual(links["GROUP_STORE_KT_SMITH"], "STORE_KT_SMITH")
		self.assertEqual(rebuilt["refshopgoods.txt"], old_goods)
		self.assertEqual(report["stores"], [])


if __name__ == "__main__":
	unittest.main()
