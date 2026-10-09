"""
===========================================================================

test_loot_catalog_merge.py - the ISRO-R / vSRO merge rules of the catalog

Small hand-built sources for each rule generate_loot_catalog.py applies:
class row precedence per level, the live generation of a row, and empty
classes kept only while a lower class can take their roll.

===========================================================================
"""

import pathlib
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "build"))
import generate_loot_catalog as catalog


# ================
# table
#
# A drop source with one class table whose rows are {level: probabilities}.
# ================
def table(width, rows):
	return {"classes": {"Equip": {"width": width, "rows": [[level, p] for level, p in sorted(rows.items())]}}}


# ================
# LootCatalogMergeTest
# ================
class LootCatalogMergeTest(unittest.TestCase):
	# ================
	# test_isro_row_wins_per_level_and_vsro_fills
	# ================
	def test_isro_row_wins_per_level_and_vsro_fills(self):
		drops = {"isro": table(3, {2: [0.0, 0.0, 0.5], 3: [0.0, 0.0, 0.0]}),
			"vsro": table(2, {1: [0.25, 0.0], 2: [0.1, 0.1], 3: [0.0, 0.75]})}
		rows, origins = catalog.merge_class_table(drops, "Equip")
		self.assertEqual(rows[:3], [[0.25, 0.0, 0.0], [0.0, 0.0, 0.5], [0.0, 0.75, 0.0]])
		self.assertEqual(origins["isro"], [[2, 2]])
		self.assertEqual(origins["vsro"], [[1, 1], [3, 3]])
		self.assertEqual(origins["none"], [[4, catalog.CATALOG_LEVELS]])

	# ================
	# test_newest_generation_is_live
	#
	# Two generations of one item and class: the newest-LSN page wins even
	# when it is the disabled one; another class of the item is its own row.
	# ================
	def test_newest_generation_is_live(self):
		def row(klass, lsn, service, absolute):
			return {"item": 7, "codename": "STONE", "class": klass, "service": service, "weight": 30,
				"absolute": absolute, "count": 1, "lsn": lsn}
		rows = [row(4, [10, 2, 1], 1, 0), row(4, [10, 9, 0], 1, 100), row(5, [3, 0, 0], 1, 0)]
		audit = []
		live = catalog.live_generations(rows, audit, "vsro")
		self.assertEqual([(r["class"], r["absolute"]) for r in live], [(4, 100), (5, 0)])
		self.assertEqual(audit, [{"source": "vsro", "item": "STONE", "class": 4, "kept": [1, 30, 100], "older": [[1, 30, 0]]}])
		rows.append(row(5, [4, 0, 0], 0, 0))
		# The newer class-5 generation is disabled; it alone is live.
		self.assertEqual([(r["class"], r["service"]) for r in catalog.live_generations(rows, [], "vsro")], [(4, 1), (5, 0)])
		with self.assertRaises(ValueError):
			catalog.live_generations([row(4, [1, 1, 1], 1, 0), row(4, [1, 1, 1], 1, 100)], [], "vsro")

	# ================
	# test_empty_class_falls_to_a_lower_class_or_is_zeroed
	# ================
	def test_empty_class_falls_to_a_lower_class_or_is_zeroed(self):
		rows = [[0.1, 0.0, 0.2], [0.0, 0.3, 0.0]]
		audit = []
		catalog.drop_itemless_classes("Equip", rows, {2}, audit)
		self.assertEqual(rows, [[0.0, 0.0, 0.2], [0.0, 0.0, 0.0]])
		self.assertEqual(audit, [{"table": "Equip", "class": 0, "fallsTo": None, "levels": [[1, 1]]},
			{"table": "Equip", "class": 1, "fallsTo": None, "levels": [[2, 2]]}])
		rows = [[0.0, 0.0, 0.2]]
		audit = []
		catalog.drop_itemless_classes("Equip", rows, {0}, audit)
		self.assertEqual(rows, [[0.0, 0.0, 0.2]])
		self.assertEqual(audit, [{"table": "Equip", "class": 2, "fallsTo": 0, "levels": [[1, 1]]}])


if __name__ == "__main__":
	unittest.main()
