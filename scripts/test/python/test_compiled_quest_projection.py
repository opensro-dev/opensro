"""
===========================================================================
test_compiled_quest_projection.py - versioned reward precedence and delivery
mission items in the compiled-quest generator

The advertised popup rewards are given as the parsed values the quest-text
importer (private tools) produces; the generator owns what follows: the
older popup's counts win, newer SQL supplies unspecified values and item
rewards, and delivery missions project their items and legs. No licensed
input or generated output is read or written.
===========================================================================
"""

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from generate_compiled_quests import Unsupported, delivery_items, handover_pages, project, rewards


# ================
# advertised
#
# One popup's parsed reward paragraph.
# ================
def advertised(exp=0, slots=0):
	return {"choices": False, "exp": exp, "skillExp": 0, "gold": 0, "inventorySlots": slots}


# ================
# sql_reward
# ================
def sql_reward():
	return {
		"exp": 12450, "skillExp": 500, "gold": 3800,
		"skillPoints": 0, "ap": 0, "hwan": 0, "inventorySlots": 4,
		"selectionCount": 0, "checkCountry": False,
		"items": [{"item": "ITEM_FIXTURE", "count": 3}],
	}


# ================
# CompiledQuestProjectionTests
# ================
class CompiledQuestProjectionTests(unittest.TestCase):
	# ================
	# test_popup_slot_counts_override_newer_sql_and_keep_sql_items
	# ================
	def test_popup_slot_counts_override_newer_sql_and_keep_sql_items(self):
		for slots in (2, 10):
			with self.subTest(slots=slots):
				text = {"reward": advertised(exp=10500, slots=slots)}
				projected = rewards("QUEST", text, {"QUEST": sql_reward()})
				self.assertEqual(projected, {
					"RewardExp": 10500, "RewardSkillExp": 0, "RewardGold": 0,
					"RewardInventorySlots": slots,
					"RewardItems": [{"ItemCodename": "ITEM_FIXTURE", "Count": 3}],
				})

	# ================
	# test_sql_fills_unspecified_rewards_and_slot_only_popup_keeps_its_count
	# ================
	def test_sql_fills_unspecified_rewards_and_slot_only_popup_keeps_its_count(self):
		for paragraph, slots in ((None, 4), (advertised(exp=100), 4), (advertised(slots=2), 2)):
			with self.subTest(paragraph=paragraph):
				projected = rewards("QUEST", {"reward": paragraph}, {"QUEST": sql_reward()})
				self.assertEqual(projected["RewardInventorySlots"], slots)
				self.assertEqual(projected["RewardExp"], 100 if paragraph and paragraph["exp"] == 100 else 12450)

	# ================
	# test_unimplemented_reward_kinds_are_not_silently_dropped
	# ================
	def test_unimplemented_reward_kinds_are_not_silently_dropped(self):
		for field in ("skillPoints", "ap", "hwan"):
			with self.subTest(field=field):
				row = sql_reward()
				row[field] = 1
				with self.assertRaisesRegex(Unsupported, "reward " + field):
					rewards("QUEST", {"reward": advertised(slots=2)}, {"QUEST": row})

	# ================
	# test_delivery_items_pair_each_codename_with_its_quantity
	#
	# +0x19 is the number of items, not a quantity: QNO_CA_HORSE_3 hands
	# over thirty of its one item.
	# ================
	def test_delivery_items_pair_each_codename_with_its_quantity(self):
		text = {"objectives": {}}
		lone = {"fields": {"0xd": "SN_CON_LONE", "0x19": 1, "0x1a": 30, "0x42": "ITEM_LEATHER"}}
		self.assertEqual(delivery_items(text, lone), [{"ItemCodename": "ITEM_LEATHER", "Count": 30}])
		pair = {"fields": {"0x19": 2, "0x1a": 1, "0x1e": 4, "0x42": "ITEM_MEDICINE", "0x46": "ITEM_LETTER"}}
		self.assertEqual(delivery_items(text, pair), [
			{"ItemCodename": "ITEM_MEDICINE", "Count": 1}, {"ItemCodename": "ITEM_LETTER", "Count": 4}])

	# ================
	# test_delivery_line_count_wins_for_a_lone_item
	# ================
	def test_delivery_line_count_wins_for_a_lone_item(self):
		text = {"objectives": {"SN_CON_LONE": {"count": 50}}}
		lone = {"fields": {"0xd": "SN_CON_LONE", "0x19": 1, "0x1a": 60, "0x42": "ITEM_PADDLE"}}
		self.assertEqual(delivery_items(text, lone), [{"ItemCodename": "ITEM_PADDLE", "Count": 50}])

	# ================
	# test_delivery_item_without_a_quantity_is_unsupported
	# ================
	def test_delivery_item_without_a_quantity_is_unsupported(self):
		text = {"objectives": {}}
		for fields in ({"0x19": 1, "0x42": "ITEM_LEATHER"}, {"0x19": 2, "0x1a": 1, "0x42": "ITEM_LEATHER"}, {"0x1a": 1}):
			with self.subTest(fields=fields):
				with self.assertRaises(Unsupported):
					delivery_items(text, {"fields": fields})

	# ================
	# test_delivery_keeps_items_only_when_the_mission_clears_0x111
	#
	# 91CA00 removes the delivered items while +0x111 is set, the
	# constructor's default; the importer records only explicit writes.
	# ================
	def test_delivery_keeps_items_only_when_the_mission_clears_0x111(self):
		text = {"objectives": {}, "reward": None}
		sql = {"QUEST": sql_reward() | {"items": [], "inventorySlots": 0}}
		for written, kept in ((None, False), (1, False), (0, True)):
			with self.subTest(written=written):
				fields = {"0x9": 3, "0x15": "NPC_END", "0x19": 1, "0x1a": 1, "0x42": "ITEM_LEATHER", "0xc0": "SN_HAND_OVER", "0x110": 1}
				if written is not None:
					fields["0x111"] = written
				quest = {
					"words": {"0x130": "SN_OFFER"}, "lists": {}, "overrides": [],
					"tables": {"0xc2": {"flags": 2}, "0xc4": {"0x8": "NPC_START"}}, "missions": [{"fields": fields}],
				}
				spec = project("QUEST", quest, text, sql)
				self.assertEqual(spec.get("DeliveryKeepsItems", False), kept)
				self.assertNotIn(None, spec.values())


	# ================
	# test_two_leg_delivery_hands_over_then_reports_to_the_start_npc
	#
	# +0x110 clear (the 872040 default): 91CA00 latches at the mission NPC
	# and gives +0x6A/+0x93/+0x6B back; the quest pays at its start NPC with
	# the ACHIEVED word. +0x110 set keeps the one-leg turn-in.
	# ================
	def test_two_leg_delivery_hands_over_then_reports_to_the_start_npc(self):
		text = {"objectives": {}, "reward": None}
		sql = {"QUEST": sql_reward() | {"items": [], "inventorySlots": 0}}
		fields = {
			"0x9": 3, "0x15": "NPC_HAND_OVER", "0x19": 1, "0x1a": 1, "0x42": "ITEM_FIRECRACKERS",
			"0xc0": "SN_HAND_OVER", "0x6a": 1, "0x6b": 1, "0x93": "ITEM_RECEIPT", "0xc8": "SN_EXCHANGE_FULL",
		}
		quest = {
			"words": {"0x130": "SN_OFFER", "0x134": "SN_ACHIEVED"}, "lists": {}, "overrides": [],
			"tables": {"0xc2": {"flags": 2}, "0xc4": {"0x8": "NPC_START"}}, "missions": [{"fields": fields}],
		}
		spec = project("QUEST", quest, text, sql)
		self.assertEqual((spec["HandOverNpcCodename"], spec["HandOverSymbol"]), ("NPC_HAND_OVER", "SN_HAND_OVER"))
		self.assertEqual((spec["EndNpcCodename"], spec["CompletePromptSymbol"]), ("NPC_START", "SN_ACHIEVED"))
		self.assertEqual(spec["ExchangeItems"], [{"ItemCodename": "ITEM_RECEIPT", "Count": 1}])
		self.assertEqual(spec["ExchangeFullSymbol"], "SN_EXCHANGE_FULL")
		one_leg = project("QUEST", quest | {"missions": [{"fields": fields | {"0x110": 1}}]}, text, sql)
		self.assertNotIn("HandOverNpcCodename", one_leg)
		self.assertEqual((one_leg["EndNpcCodename"], one_leg["CompletePromptSymbol"]), ("NPC_HAND_OVER", "SN_HAND_OVER"))
		quest["words"].pop("0x134")
		with self.assertRaisesRegex(Unsupported, "achieved line"):
			project("QUEST", quest, text, sql)

	# ================
	# test_handover_pages_follow_the_mission_layout
	#
	# 91CA00: +0xC0 [+0xE8], then +0xC4+4(k+1) [+0xE8+4k], and the hand-over
	# line +0xC8+4N; without pages, +0xC0 itself (QNO_CA_THIEF_4: N = 3).
	# ================
	def test_handover_pages_follow_the_mission_layout(self):
		self.assertEqual(handover_pages({"0xc0": "SN_LINE"}), ([], "SN_LINE"))
		fields = {"0xbf": 3, "0xc0": "P0", "0xe8": "R0", "0xcc": "P1", "0xec": "R1", "0xd0": "P2", "0xf0": "R2", "0xd4": "HAND_OVER"}
		pages, line = handover_pages(fields)
		self.assertEqual(pages, [{"PromptSymbol": "P0", "ReplySymbol": "R0"}, {"PromptSymbol": "P1", "ReplySymbol": "R1"},
			{"PromptSymbol": "P2", "ReplySymbol": "R2"}])
		self.assertEqual(line, "HAND_OVER")
		del fields["0xd4"]
		with self.assertRaisesRegex(Unsupported, "hand-over line after 3 pages"):
			handover_pages(fields)

	# ================
	# test_parallel_deliveries_hand_over_then_report
	#
	# Two deliver missions, each two-leg at its own NPC, project as parallel
	# hand-overs paid at the start NPC; a one-leg one among them is refused.
	# ================
	def test_parallel_deliveries_hand_over_then_report(self):
		text = {"objectives": {}, "reward": None}
		sql = {"QUEST": sql_reward() | {"items": [], "inventorySlots": 0}}
		def mission(index, npc):
			return {"fields": {"0x8": index, "0x9": 3, "0x15": npc, "0xd": "SN_CON_%d" % index, "0x19": 1, "0x1a": 1,
				"0x42": "ITEM_%d" % index, "0xc0": "SN_HAND_%d" % index, "0xc4": "SN_MISSING_%d" % index, "0x108": "SN_LEFT_%d" % index}}
		quest = {"words": {"0x130": "SN_OFFER", "0x134": "SN_ACHIEVED"}, "lists": {}, "overrides": [],
			"tables": {"0xc2": {"flags": 2}, "0xc4": {"0x8": "NPC_START"}}, "missions": [mission(0, "NPC_A"), mission(1, "NPC_B")]}
		spec = project("QUEST", quest, text, sql)
		self.assertEqual((spec["Objective"], spec["EndNpcCodename"], spec["CompletePromptSymbol"]), (3, "NPC_START", "SN_ACHIEVED"))
		self.assertEqual([m["HandOverNpcCodename"] for m in spec["Objectives"]], ["NPC_A", "NPC_B"])
		self.assertEqual(spec["Objectives"][1]["PendingNoticeSymbol"], "SN_LEFT_1")
		quest["missions"][1]["fields"]["0x110"] = 1
		with self.assertRaisesRegex(Unsupported, "one-leg mission in a parallel delivery"):
			project("QUEST", quest, text, sql)


if __name__ == "__main__":
	unittest.main()
