"""
===========================================================================
test_cap110_quest_monsters.py - the monsters the Blue Berserk chains spawn.

They follow the quest switch and the level cap.

	py -3 -B -m unittest discover -s scripts/test/python

===========================================================================
"""
import unittest

import cap110_quest_monsters as quest_monsters
import cap110_skill_effects as effects


# ================
# later_row
#
# A later-client character row; the converter widens it to the v1.150 layout.
# ================
def later_row(code, service="1", skills=("31041", "31045")):
	row = ["0"] * 104
	row[0], row[1], row[2], row[3] = service, "23000", code, "korean"
	row[4], row[5] = "COS_GUARD", "SN_" + code
	row[83:83 + len(skills)] = skills
	return row


# ================
# QuestMonstersTest
# ================
class QuestMonstersTest(unittest.TestCase):
	# ================
	# test_the_quest_switch_and_the_level_cap_decide
	# ================
	def test_the_quest_switch_and_the_level_cap_decide(self):
		on = {"SRO_CAP110_QUESTS": "on", "SRO_LEVEL_CAP": "110"}
		self.assertTrue(quest_monsters.enabled(on))
		self.assertFalse(quest_monsters.enabled({"SRO_CAP110_QUESTS": "on"}))
		self.assertFalse(quest_monsters.enabled({"SRO_CAP110_QUESTS": "on", "SRO_LEVEL_CAP": "90"}))
		self.assertFalse(quest_monsters.enabled({"SRO_LEVEL_CAP": "110"}))
		# The item switch is not the quest switch.
		self.assertFalse(quest_monsters.enabled({"SRO_CAP110_ITEMS": "on", "SRO_LEVEL_CAP": "110"}))
		self.assertFalse(quest_monsters.enabled({}))

	# ================
	# test_every_listed_monster_is_selected_converted
	# ================
	def test_every_listed_monster_is_selected_converted(self):
		later = {code: later_row(code) for code in quest_monsters.MONSTERS}
		refs = quest_monsters.select_monsters(later, set())
		self.assertEqual(sorted(refs), sorted(quest_monsters.MONSTERS))
		self.assertEqual(quest_monsters.needed_skills(refs), {31041, 31045})

	# ================
	# test_a_native_monster_keeps_its_native_row
	# ================
	def test_a_native_monster_keeps_its_native_row(self):
		later = {code: later_row(code) for code in quest_monsters.MONSTERS}
		refs = quest_monsters.select_monsters(later, {"MOB_QT_CH_TOMBSTONE_01"})
		self.assertNotIn("MOB_QT_CH_TOMBSTONE_01", refs)
		self.assertEqual(len(refs), len(quest_monsters.MONSTERS) - 1)

	# ================
	# test_a_monster_the_later_client_lacks_or_does_not_offer_refuses
	# ================
	def test_a_monster_the_later_client_lacks_or_does_not_offer_refuses(self):
		later = {code: later_row(code) for code in quest_monsters.MONSTERS}
		del later["MOB_QT_EU_SOLDIER_01_1"]
		with self.assertRaises(ValueError):
			quest_monsters.select_monsters(later, set())
		later = {code: later_row(code) for code in quest_monsters.MONSTERS}
		later["MOB_QT_EU_SOLDIER_01_1"] = later_row("MOB_QT_EU_SOLDIER_01_1", service="0")
		with self.assertRaises(ValueError):
			quest_monsters.select_monsters(later, set())

	# ================
	# test_removal_keeps_the_native_placeholder_rows
	# ================
	def test_removal_keeps_the_native_placeholder_rows(self):
		header = effects.SECTION_HEADER
		native = "\r\n".join([header + "\tskillaniset2", "0\tplaceholder\tMSKILL_X_ATTACK01\t0", "1\tother\tMSKILL_Y_ATTACK01\t0", ""])
		owned = "1\tadded\tMSKILL_X_ATTACK01\t0"
		with_owned = native.replace("1\tother", owned + "\r\n1\tother", 1)
		got = quest_monsters.remove_records(with_owned, {"MSKILL_X_ATTACK01"})
		self.assertIn("0\tplaceholder\tMSKILL_X_ATTACK01", got)
		self.assertIn("MSKILL_Y_ATTACK01", got)
		self.assertNotIn("added", got)

	# ================
	# test_the_shards_do_not_clash_with_the_monster_importer
	# ================
	def test_the_shards_do_not_clash_with_the_monster_importer(self):
		import cap110_monsters as monsters
		self.assertNotEqual(quest_monsters.CHARACTER_SHARD, monsters.CHARACTER_SHARD)
		self.assertNotEqual(quest_monsters.SKILL_SHARD, monsters.SKILL_SHARD)
		self.assertNotEqual(quest_monsters.MARKER, monsters.MARKER)


if __name__ == "__main__":
	unittest.main()
