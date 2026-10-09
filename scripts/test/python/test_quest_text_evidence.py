"""
===========================================================================
test_quest_text_evidence.py - multiline quest text, versioned reward precedence
and delivery mission items

Synthetic text tables exercise the production importer and reward projector.
The older popup owns advertised counts; newer SQL supplies unspecified values
and item rewards. No licensed input or generated output is read or written.
===========================================================================
"""

import pathlib
import sys
import tempfile
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from import_quest_text_evidence import advertised_rewards, plain, read_table
from generate_compiled_quests import Unsupported, delivery_items, handover_pages, project, rewards


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
# QuestTextEvidenceTests
# ================
class QuestTextEvidenceTests(unittest.TestCase):
	# ================
	# test_multiline_records_preserve_boundaries_and_reward_paragraph
	# ================
	def test_multiline_records_preserve_boundaries_and_reward_paragraph(self):
		text = (
			"1\tSN_PAYCON_FIXTURE\t<font>Quest title\r\n"
			"\r\n보상\r\n인벤토리 2칸</font>\tEND\r\n"
			"1\tSN_CON_NEXT\tCollect 20개\r\n"
			"0\tSN_DISABLED\tDisabled body"
		)
		for encoding in ("cp949", "utf-16"):
			with self.subTest(encoding=encoding), tempfile.TemporaryDirectory() as folder:
				path = pathlib.Path(folder) / "textquest.txt"
				encoded = text.encode(encoding)
				path.write_bytes(encoded)
				raw, rows = read_table(path)
				self.assertEqual(raw, encoded)
				self.assertEqual(len(rows), 3)
				self.assertEqual(rows[0], ["1", "SN_PAYCON_FIXTURE", "<font>Quest title\n\n보상\n인벤토리 2칸</font>", "END"])
				self.assertEqual(rows[1], ["1", "SN_CON_NEXT", "Collect 20개"])
				self.assertEqual(rows[2], ["0", "SN_DISABLED", "Disabled body"])
				self.assertEqual(advertised_rewards(plain(rows[0][2]))["inventorySlots"], 2)

	# ================
	# test_reward_numbers_stop_before_scenario_text
	# ================
	def test_reward_numbers_stop_before_scenario_text(self):
		body = plain("<b>보상</b> 경험치 10,500 스킬 경험치 500 GOLD 3,800 인벤토리 10칸 관련 시나리오 인벤토리 99칸")
		self.assertEqual(advertised_rewards(body), {
			"choices": False, "exp": 10500, "skillExp": 500, "gold": 3800, "inventorySlots": 10,
		})
		self.assertEqual(advertised_rewards("보상 경험치 42 관련 시나리오 인벤토리 99칸")["inventorySlots"], 0)
		self.assertIsNone(advertised_rewards("인벤토리 99칸 is scenario text without a reward heading"))

	# ================
	# test_popup_slot_counts_override_newer_sql_and_keep_sql_items
	# ================
	def test_popup_slot_counts_override_newer_sql_and_keep_sql_items(self):
		for slots in (2, 10):
			with self.subTest(slots=slots):
				text = {"reward": advertised_rewards(f"보상 경험치 10,500 인벤토리 {slots}칸")}
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
		for paragraph, slots in ((None, 4), ("보상 경험치 100", 4), ("보상 인벤토리 2칸", 2)):
			with self.subTest(paragraph=paragraph):
				advertised = advertised_rewards(paragraph) if paragraph else None
				projected = rewards("QUEST", {"reward": advertised}, {"QUEST": sql_reward()})
				self.assertEqual(projected["RewardInventorySlots"], slots)
				self.assertEqual(projected["RewardExp"], 100 if paragraph == "보상 경험치 100" else 12450)

	# ================
	# test_unimplemented_reward_kinds_are_not_silently_dropped
	# ================
	def test_unimplemented_reward_kinds_are_not_silently_dropped(self):
		for field in ("skillPoints", "ap", "hwan"):
			with self.subTest(field=field):
				row = sql_reward()
				row[field] = 1
				with self.assertRaisesRegex(Unsupported, "reward " + field):
					rewards("QUEST", {"reward": advertised_rewards("보상 인벤토리 2칸")}, {"QUEST": row})

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
		text = {"objectives": {}, "reward": advertised_rewards("")}
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
		text = {"objectives": {}, "reward": advertised_rewards("")}
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

if __name__ == "__main__":
	unittest.main()
