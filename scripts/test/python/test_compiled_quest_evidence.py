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
from generate_compiled_quests import Unsupported, check_fields, conditions, project


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
			({"flags": 0x1003, "0x4": 2}, "job condition unavailable"),
			({"flags": 0x1003, "0x4": 2, "0x20": 1, "0x21": 2}, "job condition unavailable"),
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
	# test_job_condition_follows_its_mode
	#
	# 9262A0 flag 0x1000: +0x21 set requires the job +0x20 to be worn
	# (TRADE_*_SPECIAL); clear, refuses it (Rahid 3 and 4, the thief).
	# ================
	def test_job_condition_follows_its_mode(self):
		trade = {"flags": 0x1003, "0x4": 20, "0x20": 1, "0x21": 1}
		self.assertEqual(conditions("QUEST", {"tables": {"0xc2": trade}}),
			{"JobCondition": {"Job": 1, "Required": True}, "MinLevel": 20})
		rahid = {"flags": 0x1003, "0x4": 79, "0x20": 2}
		self.assertEqual(conditions("QUEST", {"tables": {"0xc2": rahid}}),
			{"JobCondition": {"Job": 2, "Required": False}, "MinLevel": 79})

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


	# ================
	# test_loop_bound_and_shared_label_follow_the_initializer
	#
	# QNO_CA_TREASURE_4 builds two gathers in a loop bounded by "var_58 + 1
	# s< 2" although the quest holds three missions; the second branch jumps
	# into the first's label for the shared writes.
	# ================
	def test_loop_bound_and_shared_label_follow_the_initializer(self):
		quest = parse_initializer("\n".join([
			"arg3[0x121] = 3",
			"int32_t var_58 = 0",
			"do",
			"    void* eax_11 = CRT_operator_new(0x25a)",
			"    *(eax_11 + 8) = var_58.b",
			"    *(eax_11 + 9) = 1",
			"    if (var_58 == 0)",
			"        x87_r7_1 = fconvert.t(100f)",
			"        *(eax_11 + 0x241) = \"ITEM_A\"",
			"    label_8c6d17:",
			"        *(eax_11 + 0x14d) = fconvert.s(x87_r7_1)",
			"        *(eax_11 + 0x23d) = 1",
			"    else if (var_58 == 1)",
			"        x87_r7_1 = fconvert.t(10f)",
			"        *(eax_11 + 0x241) = \"ITEM_B\"",
			"        goto label_8c6d17",
			"    QuestMissionPointerVector_PushBack(&arg3[0xfb], &var_54)",
			"    cond:1_1 = var_58 + 1 s< 2",
			"    var_58 += 1",
			"while (cond:1_1)",
		]))
		gathers = [(m["fields"].get("0x241"), m["fields"].get("0x14d"), m["fields"].get("0x23d")) for m in quest["missions"]]
		self.assertEqual(gathers, [("ITEM_A", 100.0, 1), ("ITEM_B", 10.0, 1)])

	# ================
	# test_labels_fall_through_into_the_next
	#
	# QNO_CH_POTION_5: label_890bca sets 50 and falls into label_890bd0's
	# writes; a branch that sets 100 jumps straight to label_890bd0.
	# ================
	def test_labels_fall_through_into_the_next(self):
		quest = parse_initializer("\n".join([
			"arg3[0x121] = 3",
			"do",
			"    void* esi_1 = CRT_operator_new(0x25a)",
			"    *(esi_1 + 8) = var_58.b",
			"    if (var_58 == 0)",
			"        *(esi_1 + 0x241) = \"ITEM_1\"",
			"    label_890bca:",
			"        x87_r7_1 = fconvert.t(50f)",
			"    label_890bd0:",
			"        *(esi_1 + 0x14d) = fconvert.s(x87_r7_1)",
			"    else if (var_58 == 1)",
			"        *(esi_1 + 0x241) = \"ITEM_2\"",
			"        goto label_890bca",
			"    else if (var_58 == 2)",
			"        x87_r7_1 = fconvert.t(100f)",
			"        *(esi_1 + 0x241) = \"ITEM_3\"",
			"        goto label_890bd0",
			"    cond:1_1 = var_58 + 1 s< 3",
			"while (cond:1_1)",
		]))
		chances = [(m["fields"].get("0x241"), m["fields"].get("0x14d")) for m in quest["missions"]]
		self.assertEqual(chances, [("ITEM_1", 50.0), ("ITEM_2", 50.0), ("ITEM_3", 100.0)])

	# ================
	# test_required_missions_word_follows_both_call_shapes
	#
	# The initializer's closing vtable +0x17C call names N, assigned or
	# tested (QNO_EU_IVY_3 writes "if (edx_5(eax_24, 1) == 0)").
	# ================
	def test_required_missions_word_follows_both_call_shapes(self):
		assigned = parse_initializer("\n".join([
			"int32_t edx_5 = *(*arg3 + 0x17c)",
			"int32_t result = neg.d(neg.d(edx_5(ecx_6, 1, eax_2) != 0 ? 1 : 0))",
		]))
		tested = parse_initializer("\n".join([
			"int32_t edx_5 = *(*arg1 + 0x17c)",
			"if (edx_5(eax_24, 1) == 0)",
		]))
		every = parse_initializer("\n".join([
			"int32_t edx_3 = *(*arg1 + 0x17c)",
			"int32_t result = neg.d(neg.d(edx_3(ecx_3, 0, eax_2) != 0 ? 1 : 0))",
		]))
		self.assertEqual([q["words"].get("vf17c") for q in (assigned, tested, every)], [1, 1, 0])

	# ================
	# test_missions_past_the_required_count_are_optional
	#
	# vf17C's N = 1 over two gathers (QNO_EU_ADVENTURER_1): the second
	# gather runs but never gates the pay. A class that owns an item-use
	# handler (vtable +0x4) waits for its CLASS_BEHAVIOUR row.
	# ================
	def test_missions_past_the_required_count_are_optional(self):
		def gather(index, item):
			return {"fields": {"0x8": index, "0x9": 1, "0xd": "SN_CON_Q", "0x19": 1, "0x1d": "MOB_A",
				"0x23d": 5, "0x241": item, "0x14d": 50.0}}
		quest = {"words": {"0x130": "OFFER", "0x134": "PAY", "vf17c": 1}, "tables": {"0xc2": {"flags": 2},
			"0xc4": {"0x8": "NPC_A"}}, "lists": {}, "missions": [gather(0, "ITEM_A"), gather(1, "ITEM_B")], "overrides": []}
		text = {"objectives": {}, "reward": {"exp": 1, "skillExp": 0, "gold": 0, "inventorySlots": 0}}
		spec = project("QUEST", quest, text, {})
		self.assertEqual([row.get("Optional", False) for row in spec["Objectives"]], [False, True])
		quest["words"]["vf17c"] = 0
		self.assertNotIn("Optional", project("QUEST", quest, text, {})["Objectives"][1])
		with self.assertRaisesRegex(Unsupported, "custom behaviour 0x4"):
			project("QNO_EU_WITCH_1", quest, text, {})

	# ================
	# test_change_item_mission_holds_its_result
	#
	# CMissionChangeItem (91D540) holds +0x19 of +0x1D; an optional hunt
	# keeps its class cap; Irina's orders carry v1.150's one caption.
	# ================
	def test_change_item_mission_holds_its_result(self):
		change = {"fields": {"0x8": 0, "0x9": 10, "0xd": "SN_CON_QNO_WC_WAREHOUSE_W_2_01", "0x15": "ITEM_HUNTED",
			"0x19": 70, "0x1d": "ITEM_AUTHENTIC"}}
		hunt = {"fields": {"0x8": 1, "0x9": 1, "0xd": "SN_CON_QNO_WC_WAREHOUSE_W_2_01", "0x19": 1, "0x1d": "MOB_A",
			"0x23d": 10000, "0x241": "ITEM_HUNTED", "0x14d": 50.0}}
		quest = {"words": {"0x130": "OFFER", "0x134": "PAY", "vf17c": 1}, "tables": {"0xc2": {"flags": 2},
			"0xc4": {"0x8": "NPC_A"}}, "lists": {}, "missions": [change, hunt], "overrides": []}
		text = {"objectives": {"SN_CON_QNO_WC_WAREHOUSE_W_2_01": {"count": 100}}, "symbols": ["SN_CON_QNO_WC_WAREHOUSE_W_2"],
			"reward": {"exp": 1, "skillExp": 0, "gold": 0, "inventorySlots": 0}}
		rows = project("QNO_WC_WAREHOUSE_W_2", quest, text, {})["Objectives"]
		self.assertEqual([(r["CollectItemCodename"], r["CollectCount"], r.get("Optional", False)) for r in rows],
			[("ITEM_AUTHENTIC", 100, False), ("ITEM_HUNTED", 10000, True)])
		with self.assertRaisesRegex(Unsupported, "contents caption unavailable"):
			project("QNO_WC_WAREHOUSE_W_2", quest, {**text, "symbols": []}, {})
		text["symbols"] = ["SN_CON_QNO_WC_WAREHOUSE_W_3"]
		rows = project("QNO_WC_WAREHOUSE_W_3", quest, text, {})["Objectives"]
		self.assertEqual({r["ContentsSymbol"] for r in rows}, {"SN_CON_QNO_WC_WAREHOUSE_W_3"})

if __name__ == "__main__":
	unittest.main()
