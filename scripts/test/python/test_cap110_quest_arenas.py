"""
===========================================================================
test_cap110_quest_arenas.py - the Blue Berserk trial arenas.

Two isolated regions only the later client has; the quest switch seeds them
into the Alexandria import.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import unittest

import cap110_quest_arenas as arenas
import import_vsro_alexandria as stage1


# ================
# QuestArenasTest
# ================
class QuestArenasTest(unittest.TestCase):
	# ================
	# test_the_arena_ids_are_the_regions_the_trials_use
	# ================
	def test_the_arena_ids_are_the_regions_the_trials_use(self):
		ids = sorted((y << 8) | x for x, y in arenas.ARENA_REGIONS)
		self.assertEqual(ids, [22454, 22966])

	# ================
	# test_the_quest_switch_and_the_level_cap_decide
	# ================
	def test_the_quest_switch_and_the_level_cap_decide(self):
		on = {"SRO_CAP110_QUESTS": "on", "SRO_LEVEL_CAP": "110"}
		self.assertEqual(arenas.arena_seeds(on), set(arenas.ARENA_REGIONS))
		self.assertEqual(arenas.arena_seeds({"SRO_CAP110_QUESTS": "on"}), set())
		self.assertEqual(arenas.arena_seeds({"SRO_CAP110_QUESTS": "on", "SRO_LEVEL_CAP": "90"}), set())
		self.assertEqual(arenas.arena_seeds({"SRO_LEVEL_CAP": "110"}), set())
		self.assertEqual(arenas.arena_seeds({"SRO_CAP110_ITEMS": "on", "SRO_LEVEL_CAP": "110"}), set())
		self.assertEqual(arenas.arena_seeds({}), set())

	# ================
	# test_an_isolated_arena_joins_the_import_only_as_a_seed
	# ================
	def test_an_isolated_arena_joins_the_import_only_as_a_seed(self):
		city, candidates = [(10, 10), (11, 10)], {(11, 10), (12, 10), *arenas.ARENA_REGIONS}
		without = stage1.connected_regions(city, candidates, set())
		self.assertFalse(set(arenas.ARENA_REGIONS) & set(without))
		with_seeds = stage1.connected_regions(city, candidates, arenas.arena_seeds({"SRO_CAP110_QUESTS": "on", "SRO_LEVEL_CAP": "110"}))
		self.assertTrue(set(arenas.ARENA_REGIONS) <= set(with_seeds))
		self.assertEqual(set(with_seeds) - set(arenas.ARENA_REGIONS), set(without))


if __name__ == "__main__":
	unittest.main()
