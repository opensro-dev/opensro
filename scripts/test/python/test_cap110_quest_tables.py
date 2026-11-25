"""
===========================================================================
test_cap110_quest_tables.py - the Blue Berserk chains' quest rows and texts.

They are the quests themselves, so they follow the quest switch.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import cap110_quest_tables as quest_tables  # noqa: E402
import import_vsro_alexandria as tables  # noqa: E402


# ================
# quest / contents / guide rows
# ================
def quest(code, identity):
	return ["1", str(identity), code, "95", "desc", "SN_" + code, "SN_PAY_" + code, "xxx", "SN_PAYCON_" + code, "SN_NN_" + code, "SN_NC_" + code]


def contents(code):
	return [code, "name", "3", "xxx", "1", "SN_CON_" + code, "xxx", "xxx", "xxx", "xxx", "xxx", "xxx", "xxx", "0", "xxx", "xxx", "0"]


def guide(code, identity):
	return ["1", str(identity), "name", "1", "140000", "SN_PAYCON_" + code, "SN_" + code]


# ================
# table
# ================
def table(*lines):
	return b"\xff\xfe" + ("\r\n".join(lines)).encode("utf-16-le")


# ================
# QuestTablesTest
# ================
class QuestTablesTest(unittest.TestCase):
	# ================
	# test_the_quest_switch_and_the_level_cap_decide
	# ================
	def test_the_quest_switch_and_the_level_cap_decide(self):
		self.assertTrue(quest_tables.enabled({"SRO_CAP110_QUESTS": "on", "SRO_LEVEL_CAP": "110"}))
		self.assertFalse(quest_tables.enabled({"SRO_CAP110_ITEMS": "on", "SRO_LEVEL_CAP": "110"}))
		self.assertFalse(quest_tables.enabled({"SRO_CAP110_QUESTS": "on"}))

	# ================
	# test_only_the_blue_berserk_quests_belong_to_the_import
	# ================
	def test_only_the_blue_berserk_quests_belong_to_the_import(self):
		self.assertTrue(quest_tables.is_quest_row("questdata.txt", quest("QNO_CH_HWAN_1_1", 346)))
		self.assertTrue(quest_tables.is_quest_row("questdata.txt", quest("QNO_SD_HW_002", 644)))
		self.assertFalse(quest_tables.is_quest_row("questdata.txt", quest("QNO_SD_NO_004", 700)))
		self.assertFalse(quest_tables.is_quest_row("questcontentsdata.txt", contents("QNO_EU_IVY_1")))
		self.assertTrue(quest_tables.is_quest_row("gameguidedata.txt", guide("QNO_EU_HWAN_2_1", 140050)))
		self.assertFalse(quest_tables.is_quest_row("gameguidedata.txt", guide("QNO_EU_IVY_1", 140051)))

	# ================
	# test_new_rows_are_selected_in_the_variants_layout
	# ================
	def test_new_rows_are_selected_in_the_variants_layout(self):
		later = {"questdata.txt": [quest("QNO_CH_HWAN_1_1", 346), quest("QNO_SD_HW_002", 644), quest("QNO_SD_NO_004", 700)],
			"questcontentsdata.txt": [contents("QNO_CH_HWAN_1_1"), contents("QNO_SD_HW_002"), contents("QNO_EU_IVY_1")],
			"gameguidedata.txt": [guide("QNO_CH_HWAN_1_1", 140015), guide("QNO_SD_HW_002", 140043)]}
		variant = {"questdata.txt": [quest("QNO_KT_SMITH_1", 27)], "questcontentsdata.txt": [contents("QNO_KT_SMITH_1")[:13]],
			"gameguidedata.txt": [guide("QNO_KT_SMITH_1", 140013)]}
		got = quest_tables.quest_rows(later, variant)
		self.assertEqual([row[2] for row in got["questdata.txt"]], ["QNO_CH_HWAN_1_1", "QNO_SD_HW_002"])
		self.assertEqual({len(row) for row in got["questcontentsdata.txt"]}, {13})
		self.assertEqual(len(got["gameguidedata.txt"]), 2)

	# ================
	# test_a_quest_the_variant_has_stays_native
	# ================
	def test_a_quest_the_variant_has_stays_native(self):
		later = {"questdata.txt": [quest("QNO_CH_HWAN_1_1", 346), quest("QNO_CH_HWAN_1_2", 347)],
			"questcontentsdata.txt": [contents("QNO_CH_HWAN_1_1"), contents("QNO_CH_HWAN_1_2")], "gameguidedata.txt": []}
		variant = {"questdata.txt": [quest("QNO_CH_HWAN_1_1", 346)], "questcontentsdata.txt": [contents("QNO_CH_HWAN_1_1")[:13]],
			"gameguidedata.txt": []}
		got = quest_tables.quest_rows(later, variant)
		self.assertEqual([row[2] for row in got["questdata.txt"]], ["QNO_CH_HWAN_1_2"])

	# ================
	# test_a_used_quest_id_refuses_and_so_does_an_empty_client
	# ================
	def test_a_used_quest_id_refuses_and_so_does_an_empty_client(self):
		later = {"questdata.txt": [quest("QNO_CH_HWAN_1_1", 346)], "questcontentsdata.txt": [], "gameguidedata.txt": []}
		with self.assertRaises(ValueError):
			quest_tables.quest_rows(later, {"questdata.txt": [quest("QNO_OTHER", 346)], "questcontentsdata.txt": [], "gameguidedata.txt": []})
		with self.assertRaises(ValueError):
			quest_tables.quest_rows({"questdata.txt": [], "questcontentsdata.txt": [], "gameguidedata.txt": []},
				{"questdata.txt": [], "questcontentsdata.txt": [], "gameguidedata.txt": []})

	# ================
	# test_the_text_symbols_are_the_quests_keys_and_the_extras
	# ================
	def test_the_text_symbols_are_the_quests_keys_and_the_extras(self):
		def row(key):
			return ["1", key] + ["x"] * 13
		got = quest_tables.text_symbols([[row("SN_TALK_QNO_CH_HWAN_1_1_01"), row("SN_TALK_QNO_EU_IVY_1_01"), row("SN_NPC_SD_M_AREA_SENMUTE_QS")],
			[row("SN_TALK_QNO_CH_HWAN_1_1_01"), row("SN_QNO_SD_HW_002")]])
		self.assertEqual(got, ("SN_TALK_QNO_CH_HWAN_1_1_01", "SN_NPC_SD_M_AREA_SENMUTE_QS", "SN_QNO_SD_HW_002"))

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
