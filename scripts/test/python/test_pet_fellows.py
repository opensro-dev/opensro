"""
===========================================================================

test_pet_fellows.py - v1.696's Fellow pets as v1.150 growth pets

Synthetic later-layout rows stand in for v1.696: a wolf curve of 140
rows and its flute, one Fellow kind with two innate chains (a DEF chain
from level 1, a tier chain from level 20), its plain, event and premium
scrolls, and an evolution summon. The kind becomes 140 chained rows on
the template's stats under its own look, learns each chain's highest
unlocked link, and summons through growth scrolls; the premium twin is
dropped, the evolution summon passes through, and pet_import runs the
result on the attack lane.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import alexandria_npcs as characters  # noqa: E402
import cap110_items as items  # noqa: E402
import pet_fellows as fellows  # noqa: E402
import pet_import  # noqa: E402

KIND = "COS_PET2_ASS_BIRD_A"
EVOLUTION = "COS_PET2_ASS_EVOLUTION_A"
DEF_CHAIN = "P2SKILL_INNATE_ASS_DEF_B"
TIER_CHAIN = "P2SKILL_INNATE_ASS_020_B"
FELLOW_ID = "38715"
TEMPLATE_ID_BASE = 6106


# ================
# char_row / item_row / skill_row
# ================
def char_row(code, row_id, level):
	row = ["0"] * characters.VSRO_CHARACTER_COLUMNS
	row[characters.SERVICE_COLUMN] = "1"
	row[characters.ID_COLUMN] = str(row_id)
	row[characters.CODENAME_COLUMN] = code
	row[characters.LEVEL_COLUMN] = str(level)
	return row


def wolf(level):
	row = char_row(fellows.level_code(fellows.TEMPLATE_FAMILY, level), TEMPLATE_ID_BASE + level - 1, level)
	row[characters.TID_COLUMNS[0]:characters.TID4_COLUMN + 1] = ["1", "2", "3", "3"]
	row[characters.MAX_HP_COLUMN] = str(300 + 10 * level)
	row[characters.MODEL_COLUMN] = "cos\\p_wolf_01.bsr"
	row[characters.NAME_KEY_COLUMN] = "SN_COS_P_WOLF_01"
	return row


def fellow(code, skills):
	row = char_row(code, FELLOW_ID, 1)
	row[characters.TID_COLUMNS[0]:characters.TID4_COLUMN + 1] = ["1", "2", "3", "9"]
	row[characters.NAME_STRING_COLUMN] = "fellow"
	row[characters.NAME_KEY_COLUMN] = "SN_PET2_ASS_BIRD_A"
	row[characters.WALK_SPEED_COLUMN] = "20"
	row[characters.SCALE_COLUMN] = "65"
	row[characters.MODEL_COLUMN] = "pet2\\pet_bird_a.bsr"
	row[characters.ICON_COLUMN] = "pet2\\item\\item_pet2_scroll_ass_bird_a.ddj"
	row[characters.LATER_DEFAULT_SKILL_AT:characters.LATER_DEFAULT_SKILL_AT + len(skills)] = skills
	return row


def item_row(code, row_id, kind, summons, tail="xxx"):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE] = "1"
	row[items.IDENTITY] = str(row_id)
	row[items.CODENAME] = code
	row[items.TYPE_FIRST:items.TYPE_FOURTH + 1] = ["3", "2", "1", kind]
	row[items.SUMMONED_CHARACTER] = summons
	row[items.SUMMONED_CHARACTER + 2] = tail
	return row


def skill_row(skill_id, code):
	row = ["0"] * 8
	row[fellows.SKILL_ID_COLUMN] = str(skill_id)
	row[fellows.SKILL_CODE_COLUMN] = code
	return row


# The DEF chain links every 7 levels from 1, the tier chain from 20.
SKILLS = [skill_row(1000 + level, f"{DEF_CHAIN}_{level:03d}") for level in range(1, 149, 7)] + \
	[skill_row(2000 + level, f"{TIER_CHAIN}_{level:03d}") for level in range(20, 147, 7)]


def tables():
	chars = [wolf(level) for level in range(1, fellows.MAX_LEVEL + 1)]
	chars += [fellow(KIND, ["1001", "2020"]), fellow(EVOLUTION, ["1001"])]
	later_items = [
		item_row(fellows.TEMPLATE_SCROLL, 7488, "1", "COS_P_WOLF_001"),
		item_row("ITEM_PET2_SCROLL_ASS_BIRD_A", 38372, "3", KIND, tail="ITEM_PET2_EQUIP_ASS_CLAW_DEF"),
		item_row("ITEM_EVENT_PET2_SCROLL_ASS_BIRD_A", 38400, "3", KIND),
		item_row("ITEM_PRE_PET2_SCROLL_ASS_BIRD_A", 38401, "3", KIND),
		item_row("ITEM_PET2_SCROLL_ASS_EVOLUTION_A", 38369, "3", EVOLUTION),
	]
	return chars, later_items


# ================
# FellowTablesTest
# ================
class FellowTablesTest(unittest.TestCase):
	def setUp(self):
		chars, later_items = tables()
		self.chars, self.items, self.kinds = fellows.fellow_tables(chars, later_items, SKILLS)
		self.by_code = {row[characters.CODENAME_COLUMN]: row for row in self.chars}

	def level(self, level):
		return self.by_code[fellows.level_code(KIND, level)]

	def test_a_kind_becomes_140_chained_rows(self):
		self.assertEqual(self.kinds, [KIND])
		self.assertNotIn(KIND, self.by_code)
		codes = [fellows.level_code(KIND, level) for level in range(1, fellows.MAX_LEVEL + 1)]
		self.assertTrue(all(code in self.by_code for code in codes))
		for level in (1, 70, fellows.MAX_LEVEL):
			self.assertEqual(self.level(level)[characters.FIRST_FORM_COLUMN], codes[0])
		self.assertEqual(self.level(1)[characters.NEXT_FORM_COLUMN], codes[1])
		self.assertEqual(self.level(fellows.MAX_LEVEL)[characters.NEXT_FORM_COLUMN], characters.UNUSED_NAME)

	def test_stats_and_band_are_the_template_levels_and_the_look_is_the_kinds(self):
		row = self.level(50)
		self.assertEqual(row[characters.LEVEL_COLUMN], "50")
		self.assertEqual(row[characters.MAX_HP_COLUMN], wolf(50)[characters.MAX_HP_COLUMN])
		self.assertEqual(row[characters.TID4_COLUMN], "3")
		source = fellow(KIND, [])
		for column in fellows.OWN_COLUMNS:
			self.assertEqual(row[column], source[column], column)

	def test_level_one_keeps_the_kinds_id(self):
		self.assertEqual(self.level(1)[characters.ID_COLUMN], FELLOW_ID)
		self.assertEqual(self.level(2)[characters.ID_COLUMN], str(TEMPLATE_ID_BASE + 1))

	def test_each_chain_teaches_its_highest_unlocked_link(self):
		def learned(level):
			return [skill for skill in self.level(level)[characters.LATER_DEFAULT_SKILL_AT:characters.LATER_DEFAULT_SKILL_END]
				if skill != fellows.EMPTY_SKILL]

		self.assertEqual(learned(1), ["1001"])
		self.assertEqual(learned(7), ["1001"])
		self.assertEqual(learned(8), ["1008"])
		self.assertEqual(learned(19), ["1015"])
		self.assertEqual(learned(20), ["1015", "2020"])
		self.assertEqual(learned(fellows.MAX_LEVEL), ["1134", "2139"])
		self.assertEqual(len(self.level(1)[characters.LATER_DEFAULT_SKILL_AT:characters.LATER_DEFAULT_SKILL_END]),
			characters.DEFAULT_SKILL_COUNT)

	def test_scrolls_become_growth_scrolls_and_the_premium_twin_is_dropped(self):
		by_code = {row[items.CODENAME]: row for row in self.items}
		self.assertNotIn("ITEM_PRE_PET2_SCROLL_ASS_BIRD_A", by_code)
		template = by_code[fellows.TEMPLATE_SCROLL]
		for code in ("ITEM_PET2_SCROLL_ASS_BIRD_A", "ITEM_EVENT_PET2_SCROLL_ASS_BIRD_A"):
			scroll = by_code[code]
			self.assertEqual(scroll[items.TYPE_FOURTH], fellows.GROWTH_SCROLL_KIND)
			self.assertEqual(scroll[items.SUMMONED_CHARACTER], fellows.level_code(KIND, 1))
			self.assertEqual(scroll[items.SUMMONED_CHARACTER + 1:], template[items.SUMMONED_CHARACTER + 1:])

	def test_the_evolution_summon_passes_through(self):
		self.assertIn(EVOLUTION, self.by_code)
		self.assertEqual(self.by_code[EVOLUTION], fellow(EVOLUTION, ["1001"]))
		scroll = next(row for row in self.items if row[items.CODENAME] == "ITEM_PET2_SCROLL_ASS_EVOLUTION_A")
		self.assertEqual(scroll[items.TYPE_FOURTH], "3")

	def test_pet_import_runs_the_kind_on_the_attack_lane_and_defers_the_evolution(self):
		_, item_rows, report = pet_import.select_pets([], [], set(), self.chars, self.items)
		added = {kind["family"]: kind for kind in report["added"]}
		self.assertEqual(added[KIND]["kind"], "attack")
		self.assertEqual(added[KIND]["levels"], [1, fellows.MAX_LEVEL])
		self.assertEqual(added[KIND]["scrolls"], ["ITEM_EVENT_PET2_SCROLL_ASS_BIRD_A", "ITEM_PET2_SCROLL_ASS_BIRD_A"])
		self.assertEqual(pet_import.MALL_TAB[added[KIND]["kind"]], "MALL_PET_A_GROWTH")
		self.assertIn(EVOLUTION, [kind["family"] for kind in report["deferred"]])


# ================
# FellowRefusalTest
# ================
class FellowRefusalTest(unittest.TestCase):
	def test_a_source_without_fellows_is_unchanged(self):
		chars = [wolf(1)]
		later_items = [item_row(fellows.TEMPLATE_SCROLL, 7488, "1", "COS_P_WOLF_001")]
		self.assertEqual(fellows.fellow_tables(chars, later_items, SKILLS), (chars, later_items, []))

	def test_a_missing_template_level_refuses(self):
		chars, later_items = tables()
		chars = [row for row in chars if row[characters.CODENAME_COLUMN] != "COS_P_WOLF_077"]
		with self.assertRaisesRegex(ValueError, "missing levels \\[77\\]"):
			fellows.fellow_tables(chars, later_items, SKILLS)

	def test_a_skill_no_chain_holds_refuses(self):
		chars, later_items = tables()
		chars[-2][characters.LATER_DEFAULT_SKILL_AT] = "9999"
		with self.assertRaisesRegex(ValueError, "9999"):
			fellows.fellow_tables(chars, later_items, SKILLS)


if __name__ == "__main__":
	unittest.main()
