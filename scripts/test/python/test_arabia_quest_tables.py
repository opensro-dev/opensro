"""
===========================================================================

test_arabia_quest_tables.py - the Arabia quests' rows and texts (#4134).

They are the continent's quests, so they follow its switch.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest
from unittest import mock

import arabia_quest_tables as quest_tables  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402


# ================
# quest / contents / guide rows
# ================
def quest(code, identity):
	return ["1", str(identity), code, "121", "desc", "SN_" + code, "SN_PAY_" + code, "xxx", "SN_PAYCON_" + code, "SN_NN_" + code, "SN_NC_" + code]


def contents(code):
	return [code, "name", "3", "xxx", "1", "SN_CON_" + code, "xxx", "xxx", "xxx", "xxx", "xxx", "xxx", "xxx", "0", "xxx", "xxx", "0", "NPC_AR_KAREM", "NPC_AR_KAREM"]


def guide(code, identity):
	return ["1", str(identity), "name", "1", "140000", "SN_PAYCON_" + code, "SN_" + code, "0"]


# ================
# table
# ================
def table(*lines):
	return b"\xff\xfe" + ("\r\n".join(lines)).encode("utf-16-le")


# ================
# ArabiaQuestTablesTest
# ================
class ArabiaQuestTablesTest(unittest.TestCase):
	# ================
	# test_the_continent_switch_decides
	# ================
	def test_the_continent_switch_decides(self):
		self.assertTrue(quest_tables.enabled({"SRO_ARABIA": "on"}))
		self.assertTrue(quest_tables.enabled({"SRO_ARABIA": "1"}))
		self.assertFalse(quest_tables.enabled({"SRO_ARABIA": "off"}))
		self.assertFalse(quest_tables.enabled({}))

	# ================
	# test_only_the_arabia_quests_belong_to_the_import
	# ================
	def test_only_the_arabia_quests_belong_to_the_import(self):
		self.assertTrue(quest_tables.is_quest_row("questdata.txt", quest("QNO_AR_001", 1448)))
		self.assertFalse(quest_tables.is_quest_row("questdata.txt", quest("QNO_SD_HW_002", 644)))
		self.assertFalse(quest_tables.is_quest_row("questdata.txt", quest("QNO_ARABIA_001", 2000)))
		self.assertTrue(quest_tables.is_quest_row("questcontentsdata.txt", contents("QNO_AR_001")))
		self.assertFalse(quest_tables.is_quest_row("questcontentsdata.txt", contents("QNO_EU_IVY_1")))
		self.assertTrue(quest_tables.is_quest_row("gameguidedata.txt", guide("QNO_AR_001", 140050)))
		self.assertFalse(quest_tables.is_quest_row("gameguidedata.txt", guide("QNO_EU_IVY_1", 140051)))

	# ================
	# test_new_rows_are_selected_in_the_variants_layout
	# ================
	def test_new_rows_are_selected_in_the_variants_layout(self):
		later = {"questdata.txt": [quest("QNO_AR_001", 1448), quest("QNO_AR_002", 1449), quest("QNO_SD_NO_004", 700)],
			"questcontentsdata.txt": [contents("QNO_AR_001"), contents("QNO_AR_002"), contents("QNO_EU_IVY_1")],
			"gameguidedata.txt": [guide("QNO_AR_001", 140015), guide("QNO_AR_002", 140043)]}
		variant = {"questdata.txt": [quest("QNO_KT_SMITH_1", 27)], "questcontentsdata.txt": [contents("QNO_KT_SMITH_1")[:13]],
			"gameguidedata.txt": [guide("QNO_KT_SMITH_1", 140013)]}
		got = quest_tables.quest_rows(later, variant)
		self.assertEqual([row[2] for row in got["questdata.txt"]], ["QNO_AR_001", "QNO_AR_002"])
		self.assertEqual({len(row) for row in got["questcontentsdata.txt"]}, {13})
		self.assertEqual(len(got["gameguidedata.txt"]), 2)

	# ================
	# test_a_quest_the_variant_has_stays_native
	# ================
	def test_a_quest_the_variant_has_stays_native(self):
		later = {"questdata.txt": [quest("QNO_AR_001", 1448), quest("QNO_AR_002", 1449)],
			"questcontentsdata.txt": [contents("QNO_AR_001"), contents("QNO_AR_002")], "gameguidedata.txt": []}
		variant = {"questdata.txt": [quest("QNO_AR_001", 1448)], "questcontentsdata.txt": [contents("QNO_AR_001")[:13]],
			"gameguidedata.txt": []}
		got = quest_tables.quest_rows(later, variant)
		self.assertEqual([row[2] for row in got["questdata.txt"]], ["QNO_AR_002"])

	# ================
	# test_a_used_quest_id_refuses_and_so_does_an_empty_client
	# ================
	def test_a_used_quest_id_refuses_and_so_does_an_empty_client(self):
		later = {"questdata.txt": [quest("QNO_AR_001", 1448)], "questcontentsdata.txt": [], "gameguidedata.txt": []}
		with self.assertRaises(ValueError):
			quest_tables.quest_rows(later, {"questdata.txt": [quest("QNO_OTHER", 1448)], "questcontentsdata.txt": [], "gameguidedata.txt": []})
		with self.assertRaises(ValueError):
			quest_tables.quest_rows({"questdata.txt": [], "questcontentsdata.txt": [], "gameguidedata.txt": []},
				{"questdata.txt": [], "questcontentsdata.txt": [], "gameguidedata.txt": []})

	# ================
	# test_sharded_rows_fans_out_an_index_and_keeps_a_table
	# ================
	def test_sharded_rows_fans_out_an_index_and_keeps_a_table(self):
		files = {"questdata.txt": table("QuestData_250.txt", "QuestData_500.txt"),
			"questdata_250.txt": table("1\t1448\tQNO_AR_001"), "questdata_500.txt": table("1\t1449\tQNO_AR_002"),
			"plain.txt": table("1\t2\t3", "4\t5\t6")}

		def read(name):
			return files[name.lower()]

		got = quest_tables.sharded_rows(read, "questdata.txt")
		self.assertEqual([row[2] for row in got], ["QNO_AR_001", "QNO_AR_002"])
		self.assertEqual(quest_tables.sharded_rows(read, "plain.txt"), [["1", "2", "3"], ["4", "5", "6"]])

	# ================
	# test_the_text_symbols_are_the_quests_keys
	# ================
	def test_the_text_symbols_are_the_quests_keys(self):
		def row(key):
			return ["1", key, "kr"] + ["x"] * 8

		got = quest_tables.text_symbols([[row("SN_TALK_QNO_AR_001_01"), row("SN_TALK_QNO_SD_HW_002_01")],
			[row("SN_TALK_QNO_AR_001_01"), row("SN_QNO_AR_002")]])
		self.assertEqual(got, ("SN_TALK_QNO_AR_001_01", "SN_QNO_AR_002"))

	# ================
	# test_english_completion_follows_its_contract
	# ================
	def test_english_completion_follows_its_contract(self):
		def row(key, korean, english):
			return ["1", key, korean, "", "", "", "", "", english]

		with mock.patch.dict(quest_tables.QUEST_ENGLISH, {"SN_TALK_QNO_AR_001_01": ("한국어 원문", "Authored line")}):
			got = quest_tables.complete_english([row("SN_TALK_QNO_AR_001_01", "한국어 원문", ""),
				row("SN_TALK_QNO_AR_001_02", "다른 원문", "Shipped line")])
			self.assertEqual(got[0][8], "Authored line")
			self.assertEqual(got[1][8], "Shipped line")
			with self.assertRaises(ValueError):
				quest_tables.complete_english([row("SN_TALK_QNO_AR_001_01", "바뀐 원문", "")])
			with self.assertRaises(ValueError):
				quest_tables.complete_english([row("SN_TALK_QNO_AR_099_01", "원문", "")])

	# ================
	# test_dropping_rows_returns_the_original_bytes_with_or_without_a_final_newline
	# ================
	def test_dropping_rows_returns_the_original_bytes_with_or_without_a_final_newline(self):
		for ending in ("", "\r\n"):
			native = table("1\t1\tA", "1\t2\tB") + ending.encode("utf-16-le")
			appended = table("1\t1\tA", "1\t2\tB" + (ending or "\r\n"), "1\t9\tNEW")
			self.assertEqual(quest_tables.drop_rows(appended, 1, {"9"}, bool(ending)), native)
			self.assertEqual(len(tables.table_rows(quest_tables.drop_rows(appended, 1, {"9"}, bool(ending)))), 2)


if __name__ == "__main__":
	unittest.main()
