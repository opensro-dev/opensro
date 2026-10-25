"""
===========================================================================

test_compiled_quest_prerequisites.py - registered and projected quest chains

Explicit metadata admits a hand-authored prerequisite without pretending its
custom class can be projected. The same closure must still withhold quests
whose required contracts are absent. These tests use decoded metadata and
synthetic QuestSpec rows; they read no Go source or licensed game data.

===========================================================================
"""

import copy
import pathlib
import sys
import unittest

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parents[2] / "build"))
from generate_compiled_quests import parse_authored_codenames, resolve_prerequisites


RAHID_SIX = "QNO_RM_OLDWOMAN_6"
RAHID_SEVEN = "QNO_RM_OLDWOMAN_7"


# ================
# compiled_spec
#
# Only the prerequisite closure's public QuestSpec fields are needed.
# ================
def compiled_spec(code, required=(), **conditions):
	return {"Codename": code, "RequiredQuests": list(required), **conditions}


# ================
# CompiledQuestPrerequisiteTests
# ================
class CompiledQuestPrerequisiteTests(unittest.TestCase):
	# ================
	# test_metadata_keeps_each_registered_codename
	# ================
	def test_metadata_keeps_each_registered_codename(self):
		self.assertEqual(
			parse_authored_codenames([RAHID_SIX, "QNO_FIXTURE_OTHER"]),
			{RAHID_SIX, "QNO_FIXTURE_OTHER"},
		)

	# ================
	# test_metadata_rejects_non_array_documents
	# ================
	def test_metadata_rejects_non_array_documents(self):
		for value in (None, False, 6, RAHID_SIX, {"quests": [RAHID_SIX]}, (RAHID_SIX,)):
			with self.subTest(value=value):
				with self.assertRaises(ValueError):
					parse_authored_codenames(value)

	# ================
	# test_metadata_rejects_an_empty_registration
	# ================
	def test_metadata_rejects_an_empty_registration(self):
		with self.assertRaises(ValueError):
			parse_authored_codenames([])

	# ================
	# test_metadata_rejects_malformed_members_in_a_valid_document
	#
	# A valid neighbour cannot hide a bad registration or silently trim it.
	# ================
	def test_metadata_rejects_malformed_members_in_a_valid_document(self):
		invalid = (
			None, False, 6, [], {}, "", " ", "qno_fixture", "NPC_FIXTURE",
			"QNO-FIXTURE", "QNO_Ä", " QNO_FIXTURE", "QNO_FIXTURE ",
		)
		for value in invalid:
			with self.subTest(value=value):
				with self.assertRaises(ValueError):
					parse_authored_codenames([RAHID_SIX, value])

	# ================
	# test_metadata_rejects_duplicate_registrations
	# ================
	def test_metadata_rejects_duplicate_registrations(self):
		with self.assertRaises(ValueError):
			parse_authored_codenames([RAHID_SIX, "QNO_FIXTURE_OTHER", RAHID_SIX])

	# ================
	# test_registered_constant_backed_quest_unlocks_its_compiled_successor
	#
	# The registration's decoded value works independently of how Go spells
	# the authored spec's Codename expression. Rahid 6 stays unprojected.
	# ================
	def test_registered_constant_backed_quest_unlocks_its_compiled_successor(self):
		seven = compiled_spec(RAHID_SEVEN, [RAHID_SIX])
		registered = parse_authored_codenames([RAHID_SIX])
		retained, audit = resolve_prerequisites({RAHID_SEVEN: seven}, registered)
		self.assertEqual(retained, {RAHID_SEVEN: seven})
		self.assertEqual(audit, {})
		self.assertNotIn(RAHID_SIX, retained)

	# ================
	# test_absent_authored_predecessor_keeps_rahid_seven_pending
	# ================
	def test_absent_authored_predecessor_keeps_rahid_seven_pending(self):
		retained, audit = resolve_prerequisites(
			{RAHID_SEVEN: compiled_spec(RAHID_SEVEN, [RAHID_SIX])}, set(),
		)
		self.assertEqual(retained, {})
		self.assertEqual(audit, {RAHID_SEVEN: "prerequisite " + RAHID_SIX + " pending"})

	# ================
	# test_one_registration_does_not_admit_other_unsupported_custom_classes
	# ================
	def test_one_registration_does_not_admit_other_unsupported_custom_classes(self):
		seven = compiled_spec(RAHID_SEVEN, [RAHID_SIX])
		other = compiled_spec("QNO_OTHER_CHILD", ["QNO_UNPORTED_CUSTOM"])
		retained, audit = resolve_prerequisites(
			{RAHID_SEVEN: seven, "QNO_OTHER_CHILD": other}, {RAHID_SIX},
		)
		self.assertEqual(retained, {RAHID_SEVEN: seven})
		self.assertEqual(audit, {"QNO_OTHER_CHILD": "prerequisite QNO_UNPORTED_CUSTOM pending"})

	# ================
	# test_missing_transitive_chain_is_pruned_to_a_fixed_point
	#
	# Sorted child names precede their broken parent, requiring another pass.
	# ================
	def test_missing_transitive_chain_is_pruned_to_a_fixed_point(self):
		specs = {
			"QNO_A_CHILD": compiled_spec("QNO_A_CHILD", ["QNO_Z_PARENT"]),
			"QNO_B_GRANDCHILD": compiled_spec("QNO_B_GRANDCHILD", ["QNO_A_CHILD"]),
			"QNO_Z_PARENT": compiled_spec("QNO_Z_PARENT", ["QNO_MISSING"]),
		}
		retained, audit = resolve_prerequisites(specs, set())
		self.assertEqual(retained, {})
		self.assertEqual(audit, {
			"QNO_A_CHILD": "prerequisite QNO_Z_PARENT pending",
			"QNO_B_GRANDCHILD": "prerequisite QNO_A_CHILD pending",
			"QNO_Z_PARENT": "prerequisite QNO_MISSING pending",
		})

	# ================
	# test_projected_predecessors_keep_the_whole_chain
	# ================
	def test_projected_predecessors_keep_the_whole_chain(self):
		specs = {
			"QNO_A_CHILD": compiled_spec("QNO_A_CHILD", ["QNO_Z_PARENT"]),
			"QNO_Z_PARENT": compiled_spec("QNO_Z_PARENT"),
		}
		retained, audit = resolve_prerequisites(specs, set())
		self.assertEqual(retained, specs)
		self.assertEqual(audit, {})

	# ================
	# test_every_required_quest_must_be_available
	# ================
	def test_every_required_quest_must_be_available(self):
		child = compiled_spec("QNO_CHILD", [RAHID_SIX, "QNO_MISSING"])
		retained, audit = resolve_prerequisites({"QNO_CHILD": child}, {RAHID_SIX})
		self.assertEqual(retained, {})
		self.assertEqual(audit, {"QNO_CHILD": "prerequisite QNO_MISSING pending"})

	# ================
	# test_active_ended_and_quest_end_effects_require_real_contracts
	# ================
	def test_active_ended_and_quest_end_effects_require_real_contracts(self):
		for field in ("RequiredActiveQuests", "RequiredEndedQuests", "AcceptEndsQuests", "RefuseEndsQuests"):
			with self.subTest(field=field):
				child = compiled_spec("QNO_CHILD", **{field: ["QNO_EXTERNAL"]})
				retained, audit = resolve_prerequisites({"QNO_CHILD": child}, set())
				self.assertEqual(retained, {})
				self.assertEqual(audit, {"QNO_CHILD": "prerequisite QNO_EXTERNAL pending"})
				retained, audit = resolve_prerequisites({"QNO_CHILD": child}, {"QNO_EXTERNAL"})
				self.assertEqual(retained, {"QNO_CHILD": child})
				self.assertEqual(audit, {})

	# ================
	# test_each_offer_branch_end_effect_requires_its_quest
	# ================
	def test_each_offer_branch_end_effect_requires_its_quest(self):
		child = compiled_spec("QNO_CHILD", OfferBranches=[
			{"ReplySymbol": "SN_REPLY_FIRST", "EndsQuests": [RAHID_SIX]},
			{"ReplySymbol": "SN_REPLY_SECOND", "EndsQuests": ["QNO_BRANCH_END"]},
		])
		retained, audit = resolve_prerequisites({"QNO_CHILD": child}, {RAHID_SIX})
		self.assertEqual(retained, {})
		self.assertEqual(audit, {"QNO_CHILD": "prerequisite QNO_BRANCH_END pending"})
		retained, audit = resolve_prerequisites({"QNO_CHILD": child}, {RAHID_SIX, "QNO_BRANCH_END"})
		self.assertEqual(retained, {"QNO_CHILD": child})
		self.assertEqual(audit, {})

	# ================
	# test_any_of_accepts_one_projected_predecessor
	# ================
	def test_any_of_accepts_one_projected_predecessor(self):
		specs = {
			"QNO_CHILD": compiled_spec("QNO_CHILD", RequiredAnyQuests=["QNO_MISSING", "QNO_PRESENT"]),
			"QNO_PRESENT": compiled_spec("QNO_PRESENT"),
		}
		retained, audit = resolve_prerequisites(specs, set())
		self.assertEqual(retained, specs)
		self.assertEqual(audit, {})

	# ================
	# test_any_of_accepts_one_registered_predecessor
	# ================
	def test_any_of_accepts_one_registered_predecessor(self):
		child = compiled_spec("QNO_CHILD", RequiredAnyQuests=["QNO_MISSING", RAHID_SIX])
		retained, audit = resolve_prerequisites({"QNO_CHILD": child}, {RAHID_SIX})
		self.assertEqual(retained, {"QNO_CHILD": child})
		self.assertEqual(audit, {})

	# ================
	# test_any_of_with_no_available_alternative_stays_pending
	# ================
	def test_any_of_with_no_available_alternative_stays_pending(self):
		child = compiled_spec("QNO_CHILD", RequiredAnyQuests=["QNO_MISSING_A", "QNO_MISSING_B"])
		retained, audit = resolve_prerequisites({"QNO_CHILD": child}, {RAHID_SIX})
		self.assertEqual(retained, {})
		self.assertEqual(audit, {"QNO_CHILD": "prerequisite QNO_MISSING_A,QNO_MISSING_B pending"})

	# ================
	# test_any_of_is_rechecked_when_its_only_candidate_is_pruned
	# ================
	def test_any_of_is_rechecked_when_its_only_candidate_is_pruned(self):
		specs = {
			"QNO_A_CHILD": compiled_spec("QNO_A_CHILD", RequiredAnyQuests=["QNO_Z_PARENT", "QNO_MISSING"]),
			"QNO_Z_PARENT": compiled_spec("QNO_Z_PARENT", ["QNO_UNPORTED"]),
		}
		retained, audit = resolve_prerequisites(specs, set())
		self.assertEqual(retained, {})
		self.assertEqual(audit, {
			"QNO_A_CHILD": "prerequisite QNO_Z_PARENT,QNO_MISSING pending",
			"QNO_Z_PARENT": "prerequisite QNO_UNPORTED pending",
		})

	# ================
	# test_empty_projection_does_not_invent_specs_from_metadata
	# ================
	def test_empty_projection_does_not_invent_specs_from_metadata(self):
		self.assertEqual(resolve_prerequisites({}, {RAHID_SIX}), ({}, {}))

	# ================
	# test_resolution_leaves_input_specs_and_registration_unchanged
	# ================
	def test_resolution_leaves_input_specs_and_registration_unchanged(self):
		specs = {
			"QNO_BROKEN": compiled_spec("QNO_BROKEN", ["QNO_MISSING"]),
			"QNO_CHILD": compiled_spec("QNO_CHILD", ["QNO_BROKEN"], OfferBranches=[
				{"ReplySymbol": "SN_REPLY", "EndsQuests": [RAHID_SIX]},
			]),
			"QNO_SURVIVOR": compiled_spec("QNO_SURVIVOR", [RAHID_SIX]),
		}
		registered = {RAHID_SIX}
		before = copy.deepcopy(specs)
		retained, audit = resolve_prerequisites(specs, registered)
		self.assertEqual(retained, {"QNO_SURVIVOR": specs["QNO_SURVIVOR"]})
		self.assertEqual(set(audit), {"QNO_BROKEN", "QNO_CHILD"})
		self.assertEqual(specs, before)
		self.assertEqual(registered, {RAHID_SIX})


if __name__ == "__main__":
	unittest.main()
