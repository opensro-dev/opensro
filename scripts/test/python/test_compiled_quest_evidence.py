"""
===========================================================================

test_compiled_quest_evidence.py - byte and word fields of a compiled quest

Synthetic HLIL lines exercise the production importer, and synthetic
snapshot rows the generator's field check: a prerequisite's completion
count is projected, never dropped, and a field it cannot carry is refused.
No licensed input or generated output is read or written.

===========================================================================
"""

import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from import_compiled_quest_evidence import parse_initializer
from generate_compiled_quests import Unsupported, check_fields, conditions


# ================
# CompiledQuestFieldTests
# ================
class CompiledQuestFieldTests(unittest.TestCase):
	# ================
	# test_sub_width_writes_are_recorded
	#
	# A run-time value is kept as null, not dropped.
	# ================
	def test_sub_width_writes_are_recorded(self):
		quest = parse_initializer(
			"arg3[0x106].b = 3\n"
			"arg1[0x15c].w = *eax_11\n"
			"arg3[0x130] = \"SN_TALK_FIXTURE_01\"\n"
		)
		self.assertEqual(quest["words"], {"0x106.b": 3, "0x15c.w": None, "0x130": "SN_TALK_FIXTURE_01"})

	# ================
	# test_constructor_defaults_pass
	# ================
	def test_constructor_defaults_pass(self):
		check_fields({"words": {"0x106.b": 1, "0xc3.b": 0, "0x130": "SN_TALK_FIXTURE_01"}})
		check_fields({"words": {}})
		# A completion count is projected as RequiredQuestCompletions.
		check_fields({"words": {"0x106.b": 3}})

	# ================
	# test_changed_fields_are_refused
	# ================
	def test_changed_fields_are_refused(self):
		cases = (
			({"0x106.b": None}, "quest field 0x106.b = None"),
			({"0x15b.b": 1, "0x15c.w": None}, "instance world quest"),
			({"0x1a0.b": 2}, "quest field 0x1a0.b = 2"),
		)
		for words, reason in cases:
			with self.subTest(words=words):
				with self.assertRaisesRegex(Unsupported, reason):
					check_fields({"words": words})

	# ================
	# test_table_flags_are_recorded_through_their_alias
	# ================
	def test_table_flags_are_recorded_through_their_alias(self):
		quest = parse_initializer(
			"int32_t* eax_3 = arg3[0xc2]\n"
			"*eax_3 |= 2\n"
			"int32_t* eax_8 = arg3[0xc2]\n"
			"*eax_8 |= 1\n"
			"*(arg3[0xc2] + 4) = 0x37\n"
		)
		self.assertEqual(quest["tables"], {"0xc2": {"flags": 3, "0x4": 55}})

	# ================
	# test_minimum_level_follows_flag_one
	#
	# 9262A0: flag 1 admits from +0x4; without it there is no minimum.
	# ================
	def test_minimum_level_follows_flag_one(self):
		self.assertEqual(conditions("QUEST", {"tables": {"0xc2": {"flags": 3, "0x4": 55, "0x23": 68}}}), {"MinLevel": 55})
		self.assertEqual(conditions("QUEST", {"tables": {"0xc2": {"flags": 2, "0x4": 55}}}), {})
		self.assertEqual(conditions("QUEST", {"tables": {"0xc2": {"flags": 0x103, "0x4": 5}}}), {"MinLevel": 5})

	# ================
	# test_unported_conditions_are_refused
	# ================
	def test_unported_conditions_are_refused(self):
		cases = (
			({"flags": 1, "0x4": 2}, "skip the prerequisites"),
			({"flags": 0x1003, "0x4": 2}, "condition flag 0x1000"),
			({"flags": 0x10003, "0x4": 2}, "condition flag 0x10000"),
			({"flags": 7, "0x4": 2}, "held-item condition without its items"),
			({"flags": 7, "0x4": 2, "0x28": [None]}, "held-item condition without its items"),
			({"flags": 3}, "minimum level unavailable"),
		)
		for table, reason in cases:
			with self.subTest(table=table):
				with self.assertRaisesRegex(Unsupported, reason):
					conditions("QUEST", {"tables": {"0xc2": table}})

	# ================
	# test_held_items_project_both_vectors
	#
	# 9262A0 flag 4: every item of +0x28, and one of +0x38 when listed.
	# ================
	def test_held_items_project_both_vectors(self):
		every = {"flags": 7, "0x4": 22, "0x28": ["ITEM_A", "ITEM_B"]}
		self.assertEqual(conditions("QUEST", {"tables": {"0xc2": every}}),
			{"RequiredHeldItems": ["ITEM_A", "ITEM_B"], "MinLevel": 22})
		anyone = {"flags": 6, "0x38": ["ITEM_C", "ITEM_D"]}
		self.assertEqual(conditions("QUEST", {"tables": {"0xc2": anyone}}), {"RequiredAnyHeldItems": ["ITEM_C", "ITEM_D"]})

	# ================
	# test_held_item_pushes_are_recorded_in_both_forms
	#
	# An initializer names the condition table as arg3[0xc2] or by its byte
	# offset; both pushes land in the table's vector.
	# ================
	def test_held_item_pushes_are_recorded_in_both_forms(self):
		quest = parse_initializer(
			'std_string_assign_cstr_n(&var_34, "ITEM_A", 6)\n'
			"QuestStringVector_PushBack(arg3[0xc2] + 0x28, &var_34)\n"
			'std_string_assign_cstr_n(&var_50, "ITEM_B", 6)\n'
			"QuestStringVector_PushBack(*(arg1 + 0x308) + 0x38, &var_50)\n"
		)
		self.assertEqual(quest["tables"], {"0xc2": {"0x28": ["ITEM_A"], "0x38": ["ITEM_B"]}})


if __name__ == "__main__":
	unittest.main()
