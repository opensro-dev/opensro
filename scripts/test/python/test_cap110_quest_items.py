"""
===========================================================================
test_cap110_quest_items.py - the Blue Berserk chains' quest items.

They follow the item switch and the level cap, not the quest switch.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import cap110_items as items  # noqa: E402
import cap110_quest_items as quest_items  # noqa: E402
import cap110_rewards as rewards  # noqa: E402


# ================
# item
#
# A later-client item row of the given kind.
# ================
def item(code, kind=quest_items.QUEST_ITEM_KIND, identity=100, service="1"):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE], row[items.IDENTITY], row[items.CODENAME], row[items.PLACEHOLDER_NAME] = service, str(identity), code, "korean"
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = kind
	for column in range(items.PARAM_FIRST, items.PARAM_LAST, items.PARAM_STRIDE):
		row[column], row[column + 1] = "-1", items.UNUSED
	return row


# ================
# indexed
# ================
def indexed(*rows):
	return {row[items.CODENAME]: row for row in rows}


# ================
# QuestItemsTest
# ================
class QuestItemsTest(unittest.TestCase):
	# ================
	# test_the_item_switch_and_the_level_cap_decide_not_the_quest_switch
	# ================
	def test_the_item_switch_and_the_level_cap_decide_not_the_quest_switch(self):
		on = {"SRO_CAP110_ITEMS": "on", "SRO_LEVEL_CAP": "110"}
		self.assertTrue(quest_items.enabled(on))
		self.assertTrue(quest_items.enabled({**on, "SRO_CAP110_QUESTS": ""}))
		self.assertTrue(quest_items.enabled({**on, "SRO_CAP110_QUESTS": "on"}))
		self.assertFalse(quest_items.enabled({"SRO_CAP110_ITEMS": "on"}))
		self.assertFalse(quest_items.enabled({"SRO_CAP110_ITEMS": "on", "SRO_LEVEL_CAP": "90"}))
		self.assertFalse(quest_items.enabled({"SRO_LEVEL_CAP": "110"}))
		self.assertFalse(quest_items.enabled({}))

	# ================
	# test_only_new_blue_berserk_quest_items_are_selected
	# ================
	def test_only_new_blue_berserk_quest_items_are_selected(self):
		later = indexed(item("ITEM_QNO_SD_HW_003_1"), item("ITEM_QNO_SD_HW_002_1"), item("ITEM_QNO_SD_HW_004_1", service="0"),
			item("ITEM_QNO_SD_NO_004_1"), item("ITEM_QNO_SD_HW_005_1"), item("ITEM_ETC_GOLD_01", kind=("3", "3", "5", "0")))
		native = indexed(item("ITEM_QNO_SD_HW_005_1"))
		got = quest_items.select_quest_items(later, native)
		self.assertEqual([row[items.CODENAME] for row in got], ["ITEM_QNO_SD_HW_002_1", "ITEM_QNO_SD_HW_003_1"])

	# ================
	# test_a_row_of_another_kind_refuses
	# ================
	def test_a_row_of_another_kind_refuses(self):
		later = indexed(item("ITEM_QNO_SD_HW_002_1", kind=("3", "3", "11", "4")))
		with self.assertRaises(ValueError):
			quest_items.select_quest_items(later, {})

	# ================
	# test_a_link_to_an_item_the_native_table_lacks_refuses
	# ================
	def test_a_link_to_an_item_the_native_table_lacks_refuses(self):
		row = item("ITEM_QNO_SD_HW_002_1")
		row[items.PARAM_FIRST + 1] = "ITEM_UNKNOWN"
		with self.assertRaises(ValueError):
			quest_items.select_quest_items(indexed(row), {})
		native = indexed(item("ITEM_UNKNOWN", kind=("3", "3", "11", "4")))
		self.assertEqual(len(quest_items.select_quest_items(indexed(row), native)), 1)

	# ================
	# test_a_client_without_the_items_refuses
	# ================
	def test_a_client_without_the_items_refuses(self):
		with self.assertRaises(ValueError):
			quest_items.select_quest_items(indexed(item("ITEM_ETC_GOLD_01", kind=("3", "3", "5", "0"))), {})

	# ================
	# test_identities_are_fresh_and_retained_across_rebuilds
	# ================
	def test_identities_are_fresh_and_retained_across_rebuilds(self):
		selected = [item("ITEM_QNO_SD_HW_002_1", identity=24829), item("ITEM_QNO_SD_HW_003_1", identity=24830)]
		first = rewards.convert_rows(selected, {24829, 24830, 25000}, {})
		identities = [int(row[items.IDENTITY]) for row in first]
		self.assertEqual(len(set(identities)), 2)
		self.assertTrue(all(identity not in {24829, 24830, 25000} for identity in identities))
		# A rebuild reserves everything but our own shard, so the retained identity is free.
		again = rewards.convert_rows(selected, {24829, 24830, 25000}, {"ITEM_QNO_SD_HW_003_1": identities[1]})
		self.assertEqual(int(again[1][items.IDENTITY]), identities[1])

	# ================
	# test_the_shard_is_not_one_of_the_reward_shards
	# ================
	def test_the_shard_is_not_one_of_the_reward_shards(self):
		self.assertNotEqual(quest_items.OUTPUT_SHARD, rewards.OUTPUT_SHARD)
		self.assertNotEqual(quest_items.MARKER, rewards.MARKER)
		self.assertIn("ITEM_QNO_SD_HW_", quest_items.ITEM_PREFIXES)


if __name__ == "__main__":
	unittest.main()
