"""
===========================================================================

test_cap110_skills.py - the optional skill tier without licensed data

Synthetic 118-column skill rows exercise codename identity, the mastery gate,
the group bijection and fresh-group allocation, chain-next and prerequisite
remapping, native preservation and refusal of an unresolved reference.

===========================================================================
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parents[2]))
import cap110_skills as skills  # noqa: E402

WIDTH = 118


# ================
# make_skill
# ================
def make_skill(id_="1", code="SKILL_CH_SWORD_A_01", group="500", level="1", chain_next="0",
		mastery="257", mastery_level="90", prereq=(0, 0, 0), sp="100", race="0", service="1"):
	row = ["0"] * WIDTH
	row[0] = service
	row[1], row[2], row[3] = id_, group, code
	row[7], row[9] = level, chain_next
	row[34], row[36] = mastery, mastery_level
	row[40], row[41], row[42] = (str(value) for value in prereq)
	row[46], row[47] = sp, race
	return row


# ================
# SkillMergeTests
# ================
class SkillMergeTests(unittest.TestCase):
	# ================
	# test_only_new_ranks_above_the_cap_are_added
	# ================
	def test_only_new_ranks_above_the_cap_are_added(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="20", code="SKILL_CH_SWORD_A_02", group="600", level="2", mastery_level="91"),
			make_skill(id_="30", code="SKILL_CH_SWORD_A_03", group="600", level="3", mastery_level="88"),
			make_skill(id_="40", code="SKILL_EVENT_X_01", group="600", mastery_level="99"),
			make_skill(id_="50", code="SKILL_CH_SWORD_A_04", group="600", level="4", mastery_level="91", service="0"),
		]
		added, report = skills.merge_skills(native, later)
		self.assertEqual(report["added"], 1)
		self.assertEqual(report["preservedNativeRows"], 1)
		self.assertEqual(added[0][skills.CODENAME], "SKILL_CH_SWORD_A_02")

	# ================
	# test_mapped_group_is_reused_from_native
	# ================
	def test_mapped_group_is_reused_from_native(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="20", code="SKILL_CH_SWORD_A_02", group="600", level="2", mastery_level="91"),
		]
		added, report = skills.merge_skills(native, later)
		self.assertEqual(added[0][skills.GROUP], "500")
		self.assertEqual(report["groupsMappedToNative"], 1)
		self.assertEqual(report["newGroupIds"], {})

	# ================
	# test_new_group_avoids_a_native_group_collision
	# ================
	def test_new_group_avoids_a_native_group_collision(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="20", code="SKILL_CH_BOW_AREA_D_01", group="500", mastery_level="91"),
		]
		added, report = skills.merge_skills(native, later)
		self.assertEqual(added[0][skills.GROUP], "601")
		self.assertEqual(report["newGroupIds"], {"500": 601})

	# ================
	# test_chain_next_is_remapped_within_the_import
	# ================
	def test_chain_next_is_remapped_within_the_import(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="30", code="SKILL_CH_SWORD_CHAIN_X_1", group="700", chain_next="31", mastery_level="95"),
			make_skill(id_="31", code="SKILL_CH_SWORD_CHAIN_X_1S", group="700", mastery_level="95"),
		]
		added, report = skills.merge_skills(native, later)
		by_code = {row[skills.CODENAME]: row for row in added}
		self.assertEqual(by_code["SKILL_CH_SWORD_CHAIN_X_1"][skills.CHAIN_NEXT],
			by_code["SKILL_CH_SWORD_CHAIN_X_1S"][skills.IDENTITY])
		self.assertEqual(report["learnableRanks"], 1)
		self.assertEqual(report["chainSubRanks"], 1)

	# ================
	# test_chain_next_to_a_native_rank_uses_the_native_id
	# ================
	def test_chain_next_to_a_native_rank_uses_the_native_id(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="30", code="SKILL_CH_SWORD_A_02", group="600", level="2", chain_next="11", mastery_level="91"),
		]
		added, _report = skills.merge_skills(native, later)
		self.assertEqual(added[0][skills.CHAIN_NEXT], "10")

	# ================
	# test_prerequisite_groups_are_remapped
	# ================
	def test_prerequisite_groups_are_remapped(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="20", code="SKILL_CH_SWORD_A_02", group="600", level="2", mastery_level="91", prereq=(600, 0, 0)),
		]
		added, _report = skills.merge_skills(native, later)
		self.assertEqual(added[0][skills.PREREQ_IDS[0]], "500")
		self.assertEqual(added[0][skills.PREREQ_IDS[1]], "0")

	# ================
	# test_published_ids_stay_stable_with_a_marker
	# ================
	def test_published_ids_stay_stable_with_a_marker(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="20", code="SKILL_CH_SWORD_A_02", group="600", level="2", mastery_level="91"),
		]
		added, _report = skills.merge_skills(
			native, later, previous_ids={"SKILL_CH_SWORD_A_02": {"source": 20, "target": 900}})
		self.assertEqual(added[0][skills.IDENTITY], "900")

	# ================
	# test_unresolved_references_are_refused
	# ================
	def test_unresolved_references_are_refused(self):
		native = [make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90")]
		unresolved_chain = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="30", code="SKILL_CH_SWORD_A_02", group="600", level="2", chain_next="999", mastery_level="91"),
		]
		with self.assertRaises(ValueError):
			skills.merge_skills(native, unresolved_chain)
		unresolved_prereq = unresolved_chain[:1] + [
			make_skill(id_="30", code="SKILL_CH_SWORD_A_02", group="600", level="2", mastery_level="91", prereq=(777, 0, 0)),
		]
		with self.assertRaises(ValueError):
			skills.merge_skills(native, unresolved_prereq)

	# ================
	# test_ambiguous_group_map_is_refused
	# ================
	def test_ambiguous_group_map_is_refused(self):
		native = [
			make_skill(id_="10", code="SKILL_CH_SWORD_A_01", group="500", mastery_level="90"),
			make_skill(id_="12", code="SKILL_CH_BOW_A_01", group="501", mastery_level="90"),
		]
		later = [
			make_skill(id_="11", code="SKILL_CH_SWORD_A_01", group="600", mastery_level="90"),
			make_skill(id_="13", code="SKILL_CH_BOW_A_01", group="600", mastery_level="90"),
		]
		with self.assertRaises(ValueError):
			skills.merge_skills(native, later)

	# ================
	# test_zero_sp_helper_is_published_unlearnable
	#
	# The later tier carries a Cleric resurrection buff helper at SP
	# cost 0: an execution helper the resurrection cast applies, never
	# a rank a player learns. The zero cost is the author's off-board
	# mark and this import keeps it - the server refuses to teach a
	# zero-SP row - so the helper is published with its cost untouched
	# rather than given an invented one that would put it on the board.
	# ================
	def test_zero_sp_helper_is_published_unlearnable(self):
		native = [make_skill(id_="10", code="SKILL_EU_CLERIC_REBIRTHA_A_01", group="790", mastery="518", mastery_level="90")]
		later = [
			make_skill(id_="11", code="SKILL_EU_CLERIC_REBIRTHA_A_01", group="791", mastery="518", mastery_level="90"),
			make_skill(id_="20", code="SKILL_EU_CLERIC_REBIRTHA_A_02", group="791", level="2", mastery="518", mastery_level="95", sp="120"),
			make_skill(id_="30", code="SKILL_EU_CLERIC_REBIRTHA_SPECIAL_A_BUFF_01", group="791", mastery="518", mastery_level="95", sp="0"),
		]
		added, report = skills.merge_skills(native, later)
		by_code = {row[skills.CODENAME]: row for row in added}
		self.assertEqual(report["added"], 2)
		self.assertEqual(by_code["SKILL_EU_CLERIC_REBIRTHA_SPECIAL_A_BUFF_01"][skills.SP], "0")
		self.assertEqual(by_code["SKILL_EU_CLERIC_REBIRTHA_A_02"][skills.SP], "120")

	# ================
	# test_row_width_and_identity_are_validated
	# ================
	def test_row_width_and_identity_are_validated(self):
		with self.assertRaises(ValueError):
			skills.index_skills([["1", "1", "1", "SKILL_CH_SWORD_A_01"]])
		duplicate = [make_skill(id_="10"), make_skill(id_="11", code="SKILL_CH_SWORD_A_01")]
		with self.assertRaises(ValueError):
			skills.index_skills(duplicate)


if __name__ == "__main__":
	unittest.main()
