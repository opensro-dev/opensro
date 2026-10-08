"""
===========================================================================
test_quest_text_evidence.py - multiline quest text and versioned reward precedence

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
from generate_compiled_quests import Unsupported, rewards


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


if __name__ == "__main__":
	unittest.main()
