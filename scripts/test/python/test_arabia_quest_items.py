"""
===========================================================================
test_arabia_quest_items.py - the Arabia quests' items (#4134).

They follow the continent's switch, inside its layer.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import arabia_quest_items as quest_items  # noqa: E402
import cap110_items as items  # noqa: E402
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
# ArabiaQuestItemsTest
# ================
class ArabiaQuestItemsTest(unittest.TestCase):
	# ================
	# test_the_continent_switch_decides
	# ================
	def test_the_continent_switch_decides(self):
		self.assertTrue(quest_items.enabled({"SRO_ARABIA": "on"}))
		self.assertTrue(quest_items.enabled({"SRO_ARABIA": "1"}))
		self.assertFalse(quest_items.enabled({"SRO_ARABIA": "off"}))
		self.assertFalse(quest_items.enabled({}))

	# ================
	# test_only_new_arabia_quest_items_are_selected
	# ================
	def test_only_new_arabia_quest_items_are_selected(self):
		later = indexed(item("ITEM_QNO_AR_007_01"), item("ITEM_QNO_AR_006_01"), item("ITEM_QNO_AR_008_01", service="0"),
			item("ITEM_QNO_SD_HW_002_1"), item("ITEM_QNO_AR_009_01"), item("ITEM_ETC_GOLD_01", kind=("3", "3", "5", "0")))
		held = indexed(item("ITEM_QNO_AR_009_01"))
		got = quest_items.select_quest_items(later, held)
		self.assertEqual([row[items.CODENAME] for row in got], ["ITEM_QNO_AR_006_01", "ITEM_QNO_AR_007_01"])

	# ================
	# test_a_row_of_another_kind_refuses
	# ================
	def test_a_row_of_another_kind_refuses(self):
		later = indexed(item("ITEM_QNO_AR_006_01", kind=("3", "3", "11", "4")))
		with self.assertRaises(ValueError):
			quest_items.select_quest_items(later, {})

	# ================
	# test_a_link_to_an_item_the_build_lacks_refuses
	# ================
	def test_a_link_to_an_item_the_build_lacks_refuses(self):
		row = item("ITEM_QNO_AR_006_01")
		row[items.PARAM_FIRST + 1] = "ITEM_UNKNOWN"
		with self.assertRaises(ValueError):
			quest_items.select_quest_items(indexed(row), {})
		held = indexed(item("ITEM_UNKNOWN", kind=("3", "3", "11", "4")))
		self.assertEqual(len(quest_items.select_quest_items(indexed(row), held)), 1)

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
		selected = [item("ITEM_QNO_AR_006_01", identity=40329), item("ITEM_QNO_AR_007_01", identity=40330)]
		first = rewards.convert_rows(selected, {40329, 40330, 41000}, {})
		identities = [int(row[items.IDENTITY]) for row in first]
		self.assertEqual(len(set(identities)), 2)
		self.assertTrue(all(identity not in {40329, 40330, 41000} for identity in identities))
		again = rewards.convert_rows(selected, {40329, 40330, 41000}, {"ITEM_QNO_AR_007_01": identities[1]})
		self.assertEqual(int(again[1][items.IDENTITY]), identities[1])

	# ================
	# test_the_shard_is_its_own
	# ================
	def test_the_shard_is_its_own(self):
		self.assertNotEqual(quest_items.OUTPUT_SHARD, rewards.OUTPUT_SHARD)
		self.assertEqual(quest_items.OUTPUT_SHARD, "itemdata_200001.txt")
		self.assertTrue(quest_items.MARKER.startswith(".opensro-arabia-"))


if __name__ == "__main__":
	unittest.main()
