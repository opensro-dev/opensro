"""
===========================================================================
test_loot_class_map.py - the ISRO-R / vSRO drop class of real v1.150 items

Real client records from the committed loot snapshot (client-source.json)
and assignment rows shaped as the backups author them.
===========================================================================
"""

import json
import pathlib
import sys
import unittest

ROOT = pathlib.Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / "build"))
from loot_class_map import class_for

CLIENT = json.loads((ROOT / "data/loot/client-source.json").read_text(encoding="utf-8"))["items"]


# ================
# row
#
# A _RefDropItemAssign row as the #458 snapshot keeps it.
# ================
def row(codename, assigned_class):
	return [1, codename, 100, 100, assigned_class, 1]


# ================
# LootClassMapTest
# ================
class LootClassMapTest(unittest.TestCase):
	# ================
	# test_equipment_class_comes_from_the_item
	# ================
	def test_equipment_class_comes_from_the_item(self):
		for source in ("isro", "vsro"):
			for codename, expected in (("ITEM_CH_BLADE_08_A", 21), ("ITEM_CH_BLADE_08_B", 22),
					("ITEM_CH_BLADE_08_C", 23), ("ITEM_CH_SWORD_01_A_RARE", 0), ("ITEM_CH_SWORD_02_C_RARE", 5)):
				table = "RareEquip" if codename.endswith("_RARE") else "Equip"
				self.assertEqual(class_for(source, table, row(codename, -1), CLIENT[codename]), expected, codename)

	# ================
	# test_stone_class_is_its_degree_in_both_sources
	# ================
	def test_stone_class_is_its_degree_in_both_sources(self):
		stones = sorted(name for name in CLIENT if name.startswith("ITEM_ETC_ARCHEMY_MAGICSTONE_STR_"))
		self.assertGreaterEqual(len(stones), 8, "the client snapshot lost its STR stones")
		for codename in stones:
			degree = CLIENT[codename]["param1"]
			for source in ("isro", "vsro"):
				self.assertEqual(class_for(source, "Alchemy_MagicStone", row(codename, degree), CLIENT[codename]), degree)
				# An authored class that disagrees with the v1.150 degree is refused.
				self.assertIsNone(class_for(source, "Alchemy_MagicStone", row(codename, degree + 1), CLIENT[codename]))

	# ================
	# test_unplaceable_rows
	# ================
	def test_unplaceable_rows(self):
		self.assertIsNone(class_for("isro", "Equip", row("ITEM_CH_BLADE_08_A", -1), None))
		self.assertIsNone(class_for("vsro", "Reinforce", row("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", -1),
			CLIENT["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"]))
		self.assertEqual(class_for("vsro", "Reinforce", row("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", 1),
			CLIENT["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"]), 1)
		with self.assertRaises(ValueError):
			class_for("kor", "Equip", row("ITEM_CH_BLADE_08_A", -1), CLIENT["ITEM_CH_BLADE_08_A"])


if __name__ == "__main__":
	unittest.main()
