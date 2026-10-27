"""
===========================================================================

test_skill110_scope.py - the later client's skill tier without licensed data

Synthetic skilldata rows exercise the codename identity, the mastery gate,
trainable versus chain-sub classification, race and SP reporting and
tolerance of short rows.

===========================================================================
"""
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[2] / "analysis"))
import skill110_scope as scope  # noqa: E402

WIDTH = 48


# ================
# make_row
#
# Build one 48-column skilldata row, the way the shared reader would hand a
# real shard line to the scope functions.
# ================
def make_row(service="1", id_="1", group="1", code="SKILL_CH_SWORD_A_01", level="1",
		chain_next="0", mastery="257", mastery_level="1", sp="100", race="0"):
	row = [""] * WIDTH
	row[0], row[1], row[2], row[3] = service, id_, group, code
	row[7], row[9] = level, chain_next
	row[34], row[36] = mastery, mastery_level
	row[46], row[47] = sp, race
	return row


# ================
# ScopeTests
# ================
class ScopeTests(unittest.TestCase):
	# ================
	# test_only_codename_new_and_above_cap_enters_the_tier
	# ================
	def test_only_codename_new_and_above_cap_enters_the_tier(self):
		native = [make_row(id_="100", code="SKILL_CH_SWORD_A_01", mastery_level="90")]
		later = [
			make_row(id_="100", code="SKILL_CH_SWORD_A_01", mastery_level="90"),
			make_row(id_="200", code="SKILL_CH_SWORD_A_02", mastery_level="91"),
			make_row(id_="300", code="SKILL_CH_SWORD_A_03", mastery_level="90"),
		]
		report = scope.scope_tier(native, later)
		self.assertEqual(report["newPlayerRanks"], 2)
		self.assertEqual(report["aboveCapRanks"], 1)
		self.assertEqual(report["learnableRanks"], 1)
		self.assertEqual(report["learnable"][0]["id"], 200)

	# ================
	# test_renumbered_codename_is_not_new
	# ================
	def test_renumbered_codename_is_not_new(self):
		native = [make_row(id_="100", code="SKILL_CH_SWORD_A_01", mastery_level="90")]
		later = [make_row(id_="77", code="SKILL_CH_SWORD_A_01", mastery_level="90")]
		report = scope.scope_tier(native, later)
		self.assertEqual(report["newPlayerRanks"], 0)
		self.assertEqual(report["aboveCapRanks"], 0)

	# ================
	# test_chain_target_is_a_sub_row_not_a_rank
	# ================
	def test_chain_target_is_a_sub_row_not_a_rank(self):
		native = [make_row(id_="100", code="SKILL_CH_SWORD_A_01", mastery_level="90")]
		later = [
			make_row(id_="100", code="SKILL_CH_SWORD_A_01", mastery_level="90"),
			make_row(id_="400", code="SKILL_CH_SWORD_CHAIN_X_1S", mastery_level="95", chain_next="0"),
			make_row(id_="401", code="SKILL_CH_SWORD_CHAIN_X_1", mastery_level="95", chain_next="400"),
		]
		report = scope.scope_tier(native, later)
		self.assertEqual(report["chainSubRanks"], 1)
		self.assertEqual(report["learnableRanks"], 1)
		self.assertEqual(report["learnable"][0]["codename"], "SKILL_CH_SWORD_CHAIN_X_1")

	# ================
	# test_non_player_lines_are_ignored
	# ================
	def test_non_player_lines_are_ignored(self):
		later = [make_row(id_="900", code="SKILL_EVENT_EXP_SP_UP_100_01", mastery_level="99")]
		report = scope.scope_tier([], later)
		self.assertEqual(report["later"]["playerRanks"], 0)
		self.assertEqual(report["newPlayerRanks"], 0)

	# ================
	# test_mastery_aggregation_and_label
	# ================
	def test_mastery_aggregation_and_label(self):
		later = [
			make_row(id_="1", code="SKILL_CH_SWORD_A_10", level="10", mastery_level="91"),
			make_row(id_="2", code="SKILL_CH_SWORD_A_11", level="11", mastery_level="92"),
			make_row(id_="3", code="SKILL_EU_WARRIOR_B_01", level="1", mastery="513", mastery_level="95", race="1"),
		]
		report = scope.scope_tier([], later)
		china = next(m for m in report["masteries"] if m["id"] == 257)
		self.assertEqual(china["line"], "SWORD")
		self.assertEqual(china["race"], "china")
		self.assertEqual(china["ranks"], 2)
		self.assertEqual(china["maxLevel"], 92)
		self.assertEqual(china["maxRank"], 11)
		self.assertEqual(report["race"], {"china": 2, "europe": 1})

	# ================
	# test_zero_sp_rank_is_reported_not_hidden
	# ================
	def test_zero_sp_rank_is_reported_not_hidden(self):
		later = [make_row(id_="5", code="SKILL_CH_SWORD_A_12", mastery_level="93", sp="0")]
		report = scope.scope_tier([], later)
		self.assertEqual(report["learnableRanks"], 1)
		self.assertEqual(report["zeroSpLearnable"], 1)

	# ================
	# test_short_or_disabled_rows_are_tolerated
	# ================
	def test_short_or_disabled_rows_are_tolerated(self):
		report = scope.scope_tier([], [["1", "1", "1", "SKILL_CH_SWORD_A_01"], ["0", "2", "2", "SKILL_CH_SWORD_A_02"]])
		self.assertEqual(report["later"]["playerRanks"], 1)
		self.assertEqual(report["aboveCapRanks"], 0)
		self.assertEqual(report["masteryLevelValues"], {})
		self.assertEqual(report["masteries"], [])

	# ================
	# test_mastery_line_derivation
	# ================
	def test_mastery_line_derivation(self):
		self.assertEqual(scope.mastery_line("SKILL_CH_BOW_AREA_D_01"), "BOW")
		self.assertEqual(scope.mastery_line("SKILL"), "")
		self.assertEqual(scope.player_skill("SKILL_EU_WIZARD_A_01"), True)
		self.assertEqual(scope.player_skill("MSKILL_RM_ROC_ATTACK10"), False)


# ================
# MainPinTests
#
# The entry point refuses a client that is not the pinned VSRO one
# before any table is read (#1745).
# ================
class MainPinTests(unittest.TestCase):
	def setUp(self):
		scope.archive._verified_archives.clear()
		self.root = tempfile.TemporaryDirectory()
		self.addCleanup(self.root.cleanup)
		self.vsro = Path(self.root.name)

	def run_main(self):
		argv = ["skill110_scope.py", "--vsro-root", str(self.vsro)]
		with patch.object(sys, "argv", argv):
			scope.main()

	def test_main_refuses_a_client_that_is_not_the_pinned_one(self):
		(self.vsro / "Media.pk2").write_bytes(b"some other client")
		with self.assertRaisesRegex(SystemExit, "does not match the pinned"):
			self.run_main()

	def test_main_refuses_a_missing_archive(self):
		with self.assertRaisesRegex(SystemExit, "not found"):
			self.run_main()


if __name__ == "__main__":
	unittest.main()
