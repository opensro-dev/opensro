"""
===========================================================================

test_pet_inference.py - rows for the COS models no client ships a row for

Synthetic later-layout rows stand in for the VSRO templates: the gorilla
follows the bear's two growth forms, the frilled lizard the raccoon dog,
and the fourth dragon horse tier extends the first three. Premium and
event scrolls never become the inferred kind's scroll, a missing
template infers nothing, and an icon no tree holds takes the template's.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import unittest

import alexandria_npcs as characters  # noqa: E402
import cap110_items as items  # noqa: E402
import pet_inference as inference  # noqa: E402

CHAR_COLUMNS = characters.VSRO_CHARACTER_COLUMNS
CHAR_ID = 1
CHAR_BASE = 4
CHAR_NEXT = 6
CHAR_MODEL = 52
CHAR_LEVEL = 57
CHAR_HP = 59


# ================
# char_row / scroll_row / text_row
# ================
def char_row(code, identity, model, icon, level="1", hp="100", base="", following=""):
	row = ["0"] * CHAR_COLUMNS
	row[CHAR_ID] = str(identity)
	row[inference.CHAR_CODE] = code
	row[CHAR_BASE] = base or "xxx"
	row[inference.CHAR_NAME_KEY] = "SN_" + inference.family_of(code)
	row[CHAR_NEXT] = following or "xxx"
	row[CHAR_MODEL] = model
	row[inference.CHAR_ICON] = icon
	row[CHAR_LEVEL] = level
	row[CHAR_HP] = hp
	return row


def scroll_row(code, identity, assoc, icon="cos\\item_cos_c_scroll.ddj", level="60"):
	row = ["0"] * items.ITEM_COLUMNS
	row[items.SERVICE] = "1"
	row[items.IDENTITY] = str(identity)
	row[items.CODENAME] = code
	row[items.NAME_KEY] = "SN_" + code
	row[inference.ITEM_DESC_KEY] = "SN_" + code + "_TT_DESC"
	row[33] = level
	row[items.ICON] = icon
	row[inference.ITEM_ASSOCIATED_CHAR] = assoc
	return row


def text_row(key, english):
	row = ["1", key, "korean"] + ["0"] * 13
	row[inference.TEXT_ENGLISH] = english
	return row


def bears():
	return [
		char_row("COS_P_BEAR_001", 100, "cos\\p_BEAR01.bsr", "cos\\cos_p_bear.ddj", base="COS_P_BEAR_001", following="COS_P_BEAR_002"),
		char_row("COS_P_BEAR_002", 101, "cos\\p_BEAR02.bsr", "cos\\cos_p_bear.ddj", level="40", base="COS_P_BEAR_001"),
	]


def dragon_horses():
	tiers = [("1", "3918", "75", "8875", "60"), ("2", "3919", "90", "14130", "75"), ("3", "3920", "105", "21957", "90")]
	chars = [char_row(f"COS_C_DHORSE{tier}", identity, f"cos\\c_dhorse{tier}.bsr", f"cos\\cos_c_dhorse{tier}.ddj", level, hp)
		for tier, identity, level, hp, _ in tiers]
	scrolls = [scroll_row(f"ITEM_COS_C_DHORSE{tier}", 3900 + int(tier), f"COS_C_DHORSE{tier}", level=need)
		for tier, _, _, _, need in tiers]
	return chars, scrolls


class GrowthKindTests(unittest.TestCase):
	# ================
	# test_gorilla_follows_the_bear_forms
	#
	# Both bear forms become gorilla forms: codes, the growth chain
	# columns, the two models and the plain scroll; the premium scroll
	# never becomes the gorilla's.
	# ================
	def test_gorilla_follows_the_bear_forms(self):
		scrolls = [scroll_row("ITEM_COS_P_BEAR_SCROLL", 23322, "COS_P_BEAR_001", "cos\\item_cos_p_bear_scroll.ddj"),
			scroll_row("ITEM_PRE_COS_P_GORILLA_SCROLL", 35562, "COS_P_GORILLA_001")]
		chars, item_rows, _ = inference.infer_kinds(bears(), scrolls, {})
		gorillas = [row for row in chars if row[inference.CHAR_CODE].startswith("COS_P_GORILLA")]
		self.assertEqual([row[inference.CHAR_CODE] for row in gorillas], ["COS_P_GORILLA_001", "COS_P_GORILLA_002"])
		self.assertEqual([row[CHAR_MODEL] for row in gorillas], ["cos\\p_gorilla01.bsr", "cos\\p_gorilla02.bsr"])
		self.assertEqual(gorillas[0][CHAR_NEXT], "COS_P_GORILLA_002")
		self.assertEqual(gorillas[1][CHAR_BASE], "COS_P_GORILLA_001")
		self.assertEqual(gorillas[0][CHAR_LEVEL], "1")
		self.assertEqual([row[items.CODENAME] for row in item_rows], ["ITEM_COS_P_GORILLA_SCROLL"])
		self.assertEqual(item_rows[0][inference.ITEM_ASSOCIATED_CHAR], "COS_P_GORILLA_001")

	# ================
	# test_absent_icon_takes_the_templates
	# ================
	def test_absent_icon_takes_the_templates(self):
		scrolls = [scroll_row("ITEM_COS_P_BEAR_SCROLL", 23322, "COS_P_BEAR_001", "cos\\item_cos_p_bear_scroll.ddj")]

		def has_icon(name):
			return "gorilla" not in name

		lizards = [char_row("COS_P_RACCOONDOG", 300, "cos\\p_raccoondog.bsr", "cos\\cos_p_raccoondog.ddj")]
		lizard_scrolls = [scroll_row("ITEM_COS_P_RACCOONDOG_SCROLL", 301, "COS_P_RACCOONDOG", "cos\\item_cos_p_raccoondog_scroll.ddj")]
		chars, item_rows, _ = inference.infer_kinds(bears() + lizards, scrolls + lizard_scrolls, {}, has_icon)
		by_code = {row[inference.CHAR_CODE]: row for row in chars}
		self.assertEqual(by_code["COS_P_GORILLA_001"][inference.CHAR_ICON], "cos\\cos_p_bear.ddj")
		# The lizard's own icon name is held, so it keeps it.
		self.assertEqual(by_code["COS_P_FRILLED_LIZARD"][inference.CHAR_ICON], "cos\\cos_p_frilled_lizard.ddj")
		self.assertEqual({row[items.CODENAME]: row[items.ICON] for row in item_rows}["ITEM_COS_P_GORILLA_SCROLL"],
			"cos\\item_cos_p_bear_scroll.ddj")
		self.assertEqual(by_code["COS_P_FRILLED_LIZARD"][CHAR_MODEL], "cos\\p_frilled_lizard.bsr")
		self.assertIn("ITEM_COS_P_FRILLED_LIZARD_SCROLL", [row[items.CODENAME] for row in item_rows])

	# ================
	# test_missing_template_infers_nothing
	# ================
	def test_missing_template_infers_nothing(self):
		self.assertEqual(inference.infer_kinds([], [], {}), ([], [], []))

	# ================
	# test_names_carry_the_kind_in_every_language_cell
	# ================
	def test_names_carry_the_kind_in_every_language_cell(self):
		scrolls = [scroll_row("ITEM_COS_P_BEAR_SCROLL", 23322, "COS_P_BEAR_001")]
		texts = {"SN_COS_P_BEAR": text_row("SN_COS_P_BEAR", "Bear"),
			"SN_ITEM_COS_P_BEAR_SCROLL": text_row("SN_ITEM_COS_P_BEAR_SCROLL", "Bear Summon Scroll")}
		_, _, text_rows = inference.infer_kinds(bears(), scrolls, texts)
		by_key = {row[inference.TEXT_KEY]: row for row in text_rows}
		self.assertEqual(sorted(by_key), ["SN_COS_P_GORILLA", "SN_ITEM_COS_P_GORILLA_SCROLL"])
		self.assertEqual(by_key["SN_ITEM_COS_P_GORILLA_SCROLL"][inference.TEXT_ENGLISH], "Gorilla Summon Scroll")
		# The Korean cell no longer says bear; the numeric cells stay.
		self.assertEqual(by_key["SN_COS_P_GORILLA"][2], "Gorilla")
		self.assertEqual(by_key["SN_COS_P_GORILLA"][0], "1")


class TierKindTests(unittest.TestCase):
	# ================
	# test_fourth_dragon_horse_extends_the_tiers
	#
	# Level and scroll level step by 15 (linear); HP grows by the last
	# tier's ratio; the identity steps on; the model is tier 4's.
	# ================
	def test_fourth_dragon_horse_extends_the_tiers(self):
		chars, scrolls = dragon_horses()
		inferred, item_rows, _ = inference.infer_kinds(chars, scrolls, {})
		horse = [row for row in inferred if row[inference.CHAR_CODE] == "COS_C_DHORSE4"]
		self.assertEqual(len(horse), 1)
		self.assertEqual(horse[0][CHAR_LEVEL], "120")
		self.assertEqual(horse[0][CHAR_HP], str(round(21957 * 21957 / 14130)))
		self.assertEqual(horse[0][CHAR_ID], "3921")
		self.assertEqual(horse[0][CHAR_MODEL], "cos\\c_dhorse4.bsr")
		self.assertEqual(horse[0][inference.CHAR_NAME_KEY], "SN_COS_C_DHORSE4")
		self.assertEqual([row[items.CODENAME] for row in item_rows], ["ITEM_COS_C_DHORSE4"])
		self.assertEqual(item_rows[0][33], "105")
		self.assertEqual(item_rows[0][inference.ITEM_ASSOCIATED_CHAR], "COS_C_DHORSE4")
		# The shared scroll icon is the same every tier, so it is kept.
		self.assertEqual(item_rows[0][items.ICON], "cos\\item_cos_c_scroll.ddj")

	# ================
	# test_tier_portrait_falls_down_to_a_held_tier
	#
	# v1.150 ships only the first tier's portrait; tier 4 takes it rather
	# than the third tier's name, which no tree holds either.
	# ================
	def test_tier_portrait_falls_down_to_a_held_tier(self):
		chars, scrolls = dragon_horses()

		def has_icon(name):
			return name == "cos/cos_c_dhorse1.ddj"

		inferred, _, _ = inference.infer_kinds(chars, scrolls, {}, has_icon)
		horse = [row for row in inferred if row[inference.CHAR_CODE] == "COS_C_DHORSE4"][0]
		self.assertEqual(horse[inference.CHAR_ICON], "cos\\cos_c_dhorse1.ddj")

	# ================
	# test_an_ambiguous_tier_scroll_infers_nothing
	#
	# Two scrolls for one tier (v1.696's ITEM_COS_LEVEL_ copy) leave no
	# single row to extrapolate; the kind is not guessed.
	# ================
	def test_an_ambiguous_tier_scroll_infers_nothing(self):
		chars, scrolls = dragon_horses()
		scrolls.append(scroll_row("ITEM_COS_LEVEL_C_DHORSE1", 50000, "COS_C_DHORSE1"))
		inferred, _, _ = inference.infer_kinds(chars, scrolls, {})
		self.assertNotIn("COS_C_DHORSE4", [row[inference.CHAR_CODE] for row in inferred])


if __name__ == "__main__":
	unittest.main()
