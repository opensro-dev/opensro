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

import alexandria_npcs as characters  # noqa: E402
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


def quest_guide(code, identity):
	return ["1", str(identity), "name", "1", "300000", "SN_PAYCON_" + code, "SN_" + code, code]


def text_row(key, korean, english):
	return ["1", key, korean, "", "", "", "", "", english, english]


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
	# test_stub_rows_are_carried_and_completed
	#
	# A stub row of the archive (key and Korean only, 4 columns) used
	# to leave by the length filters, and the stubbed quest with it.
	# It now projects to a blank-English row, and complete_english
	# fills it under the same contract as any authored line. A stub
	# naming no Arabia quest keeps the old path, untouched.
	# ================
	def test_stub_rows_are_carried_and_completed(self):
		families = {name: table("") for name in quest_tables.TEXT_FAMILIES}
		families["textquest_queststring"] = table("1\t10843\tSN_TALK_QNO_AR_001_01\t한국어 원문",
			"1\t2\tSN_TALK_QNO_OK_01\t짧은 행")

		def read(name):
			return families[name.removesuffix(".txt")]

		rows = quest_tables.later_text_rows(read)
		stub = [row for row in rows[0] if len(row) > 1 and row[1] == "SN_TALK_QNO_AR_001_01"]
		self.assertEqual(len(stub), 1)
		self.assertEqual(stub[0][2], "한국어 원문")
		self.assertEqual(stub[0][8], "")
		self.assertIn(["1", "2", "SN_TALK_QNO_OK_01", "짧은 행"], rows[0])
		with mock.patch.dict(quest_tables.QUEST_ENGLISH, {"SN_TALK_QNO_AR_001_01": ("한국어 원문", "Authored line")}):
			filled = quest_tables.complete_english(rows[0])
			self.assertEqual([row[8] for row in filled if len(row) > 1 and row[1] == "SN_TALK_QNO_AR_001_01"],
				["Authored line"])

	# ================
	# test_odd_length_quest_rows_keep_their_english
	#
	# SN_TALK_QNO_AR_026_03 ships 15 columns and SN_PAYCON_QNO_AR_002
	# 18; both put their English at the usual cell once the row is
	# padded or cut to the 17 the projection reads. A short row naming
	# no quest is left as it was.
	# ================
	def test_odd_length_quest_rows_keep_their_english(self):
		short = ["1", "11002", "SN_TALK_QNO_AR_026_03", "원문"] + ["0"] * 5 + ["Shipped line"] + ["0"] * 5
		long = ["1", "1389", "SN_PAYCON_QNO_AR_002", "원문"] + ["0"] * 5 + ["Shipped body"] + ["0"] * 7 + ["extra"]
		self.assertEqual((len(short), len(long)), (15, 18))
		for raw in (short, long):
			got = quest_tables.project_later_row(raw)
			self.assertEqual(got[1], raw[2])
			self.assertEqual(got[8], raw[9])
		plain = ["1", "2", "SN_OTHER", "짧은"]
		self.assertEqual(quest_tables.project_later_row(plain), plain)

	# ================
	# test_the_recovered_talk_lines_are_carried
	#
	# The twelve quests' thirteen lines (owner, 2026-11-26): each names
	# the Korean it translates and an English line the build's rule
	# accepts as text.
	# ================
	def test_the_recovered_talk_lines_are_carried(self):
		wants = ["SN_TALK_QNO_AR_001_01", "SN_TALK_QNO_AR_001_05", "SN_TALK_QNO_AR_002_01", "SN_TALK_QNO_AR_002_02",
			"SN_TALK_QNO_AR_003_01", "SN_TALK_QNO_AR_004_01", "SN_TALK_QNO_AR_010_05", "SN_TALK_QNO_AR_012_01",
			"SN_TALK_QNO_AR_014_05", "SN_TALK_QNO_AR_018_06", "SN_TALK_QNO_AR_019_05", "SN_TALK_QNO_AR_026_05",
			"SN_TALK_QNO_AR_027_01"]
		talk = {k: v for k, v in quest_tables.QUEST_ENGLISH.items() if k.startswith("SN_TALK_QNO_AR_")}
		self.assertEqual(sorted(talk), sorted(wants))
		for key, (korean, english) in talk.items():
			self.assertTrue(korean.strip(), key)
			self.assertFalse(characters.is_placeholder_text(english), key)

	# ================
	# test_a_placeholder_english_is_missing_english
	#
	# r204 (2026-11-24): v1.696 ships 38 SN_PAY_QNO_AR_* titles with "0"
	# in the English cell. The import read "0" as English and the asset
	# build refused it as untranslated; the guard now asks the build's
	# own rule, so a "0" row takes its authored line or refuses here.
	# ================
	def test_a_placeholder_english_is_missing_english(self):
		def row(key, korean, english):
			return ["1", key, korean, "", "", "", "", "", english]

		with mock.patch.dict(quest_tables.QUEST_ENGLISH, {"SN_PAY_QNO_AR_071": ("소원의 램프 1", "Lamp of Wishes 1")}, clear=True):
			for blank in ("0", "xxx", "-", "NULL", " 0 "):
				got = quest_tables.complete_english([row("SN_PAY_QNO_AR_071", "소원의 램프 1", blank)])
				self.assertEqual(got[0][8], "Lamp of Wishes 1", blank)
			with self.assertRaises(ValueError):
				quest_tables.complete_english([row("SN_PAY_QNO_AR_072", "소원의 램프 2", "0")])

	# ================
	# test_every_shipped_placeholder_title_is_authored
	#
	# Each authored title names the Korean it translates and an English
	# line that the build's rule accepts as text.
	# ================
	def test_every_shipped_placeholder_title_is_authored(self):
		titles = {k: v for k, v in quest_tables.QUEST_ENGLISH.items() if k.startswith("SN_PAY_QNO_AR_")}
		self.assertEqual(sorted(titles), [f"SN_PAY_QNO_AR_{n:03d}" for n in range(71, 109)])
		for key, (korean, english) in titles.items():
			self.assertTrue(korean.strip(), key)
			self.assertFalse(characters.is_placeholder_text(english), key)

	# ================
	# test_dropping_rows_returns_the_original_bytes_with_or_without_a_final_newline
	# ================
	def test_dropping_rows_returns_the_original_bytes_with_or_without_a_final_newline(self):
		for ending in ("", "\r\n"):
			native = table("1\t1\tA", "1\t2\tB") + ending.encode("utf-16-le")
			appended = table("1\t1\tA", "1\t2\tB" + (ending or "\r\n"), "1\t9\tNEW")
			self.assertEqual(quest_tables.drop_rows(appended, 1, {"9"}, bool(ending)), native)
			self.assertEqual(len(tables.table_rows(quest_tables.drop_rows(appended, 1, {"9"}, bool(ending)))), 2)

	# ================
	# test_a_guide_row_whose_quest_exists_nowhere_leaves
	#
	# v1.696's 370005 names QNO_AR_109: no questdata row and no text. It
	# leaves; a row with a quest, or with only its title text, or a
	# category row, stays.
	# ================
	def test_a_guide_row_whose_quest_exists_nowhere_leaves(self):
		rows = [quest_guide("QNO_AR_001", 300001), quest_guide("QNO_AR_071", 340001), quest_guide("QNO_AR_109", 370005),
			["1", "300000", "town", "0", "7", "SRO_GGW_MENU", "SRO_GGW_MENU", "xxx"]]
		kept = quest_tables.guide_rows(rows, [quest("QNO_AR_001", 1448)], {"SN_QNO_AR_071"})
		self.assertEqual([row[1] for row in kept], ["300001", "340001", "300000"])

	# ================
	# test_a_region_header_comes_with_its_quests
	#
	# 300000 (Baghdad) names no quest, but heads QNO_AR_001; without it
	# the client refuses the guide ("Orphan quest guide article", r204h).
	# A header that heads no imported quest stays out, and the header's
	# title comes from the later help strings.
	# ================
	def test_a_region_header_comes_with_its_quests(self):
		head = ["1", "300000", "town", "0", "7", "SRO_GGW_MENU_QSD_AR_01_MSG", "SRO_GGW_MENU_QSD_AR_01_MSG", "xxx"]
		other = ["1", "140000", "elsewhere", "0", "3", "SRO_GGW_MENU_X", "SRO_GGW_MENU_X", "xxx"]
		rows = [head, other, quest_guide("QNO_AR_001", 300001)]
		self.assertEqual(quest_tables.guide_heads(rows), {"300000"})
		later = {quest_tables.QUEST_TABLE: [quest("QNO_AR_001", 1448)], quest_tables.CONTENTS_TABLE: [],
			quest_tables.GUIDE_TABLE: rows}
		variant = {name: [] for name in quest_tables.KEYS}
		chosen = quest_tables.quest_rows(later, variant)[quest_tables.GUIDE_TABLE]
		self.assertEqual([row[1] for row in chosen], ["300000", "300001"])
		raw = ["1", "271", "SRO_GGW_MENU_QSD_AR_01_MSG", "바그다드", "0", "0", "0", "0", "0", "Baghdad",
			"0", "0", "0", "Baghdad", "Baghdad", "Baghdad", "Baghdad"]
		unrelated = ["1", "272", "SRO_GGW_MENU_X", "x", "0", "0", "0", "0", "0", "X", "0", "0", "0", "X", "X", "X", "X"]
		help_table = table("\t".join(raw), "\t".join(unrelated))
		titles = quest_tables.head_help_rows(lambda name: help_table, chosen)
		self.assertEqual([(row[1], row[8]) for row in titles], [("SRO_GGW_MENU_QSD_AR_01_MSG", "Baghdad")])

	# ================
	# test_a_missing_guide_body_is_composed_from_the_quests_lines
	#
	# 300002 (QNO_AR_002) names a body v1.696 never wrote; it is the
	# quest's task lines in step order and its format line, per language.
	# A row whose body exists, or whose quest has no lines, gets none.
	# ================
	def test_a_missing_guide_body_is_composed_from_the_quests_lines(self):
		texts = [[text_row("SN_CON_QNO_AR_002_2", "둘", "Second"), text_row("SN_CON_QNO_AR_002_1", "하나", "First"),
			text_row("SN_NC_QNO_AR_002", "1회", "One-time"), text_row("SN_PAYCON_QNO_AR_001", "본문", "Body")]]
		guide_rows = [quest_guide("QNO_AR_001", 300001), quest_guide("QNO_AR_002", 300002), quest_guide("QNO_AR_003", 300003)]
		bodies = quest_tables.composed_guide_bodies(guide_rows, texts)
		self.assertEqual([body[1] for body in bodies], ["SN_PAYCON_QNO_AR_002"])
		english = bodies[0][8]
		self.assertTrue(english.startswith("<sml2>") and english.endswith("One-time</sml2>"), english)
		self.assertLess(english.index("First"), english.index("Second"))
		self.assertIn("Task", english)
		self.assertIn("임무", bodies[0][2])
		self.assertEqual(bodies[0][3], "")

	# ================
	# test_a_guide_row_without_text_refuses_at_import
	#
	# The client refuses an article whose title or body has no text
	# (quest-guide.ts); the import refuses it first, by the build's own
	# no-text rule, and accepts text the variant already carries.
	# ================
	def test_a_guide_row_without_text_refuses_at_import(self):
		row = quest_guide("QNO_AR_002", 300002)
		quest_tables.check_guide_text([row], {"SN_QNO_AR_002": "Threat", "SN_PAYCON_QNO_AR_002": "Body"}, set())
		quest_tables.check_guide_text([row], {"SN_QNO_AR_002": "Threat"}, {"SN_PAYCON_QNO_AR_002"})
		for texts in ({"SN_QNO_AR_002": "Threat"}, {"SN_QNO_AR_002": "0", "SN_PAYCON_QNO_AR_002": "Body"}):
			with self.assertRaisesRegex(ValueError, "300002"):
				quest_tables.check_guide_text([row], texts, set())
		heading = row[:3] + ["0"] + row[4:]
		quest_tables.check_guide_text([heading], {"SN_QNO_AR_002": "Threat"}, set())


if __name__ == "__main__":
	unittest.main()
