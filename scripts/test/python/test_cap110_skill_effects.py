"""
===========================================================================

test_cap110_skill_effects.py - the animation-set merge without licensed data

Synthetic skilleffect sections exercise section-aware insertion, native
precedence, the service-column offset and the unknown-animation-state refusal.

===========================================================================
"""
import script_paths  # noqa: F401  (puts scripts/ and its build folders on sys.path, #3410)
import sys
import unittest
from pathlib import Path

import cap110_skill_effects as effects  # noqa: E402

NATIVE = (
	"#section\tskillaniset2\t\t\t\r\n"
	"1\tfoo\tSKILL_A\tANI_SKILL_1\t0\r\n"
	"1\tbar\tSKILL_B\tANI_SKILL_2\t0\r\n"
	"0\tz\tSKILL_DISABLED\tANI_SKILL_3\t0\r\n"
	"#section\tskilleffectset\t\t\r\n"
	"1\tEFFECT_X\t0\r\n"
)
LATER = (
	"#section\tskillaniset2\t\t\t\r\n"
	"1\tnew\tSKILL_C\tANI_SKILL_5\t0\r\n"
	"1\told\tSKILL_A\tANI_SKILL_1\t0\r\n"
	"#section\tskilleffectset\t\t\r\n"
	"1\tEFFECT_Y\t0\r\n"
)


# ================
# MergeTests
# ================
class MergeTests(unittest.TestCase):
	# ================
	# test_removal_preserves_npc_and_effect_sections
	# ================
	def test_removal_preserves_npc_and_effect_sections(self):
		base = NATIVE + "#section\tcharacterInfo\r\nNPC_SD_M_AREA_ARMOR\tPLAYER\t1.6\r\n"
		merged, _ = effects.merge_skill_effect_text(base, effects.select_later_rows(LATER, {"SKILL_C"}))
		self.assertEqual(effects.remove_animation_bases(merged, {"SKILL_C"}), base)

	# ================
	# test_only_absent_bases_are_inserted
	# ================
	def test_only_absent_bases_are_inserted(self):
		self.assertEqual(effects.native_bases(NATIVE), {"SKILL_A", "SKILL_B"})
		selected = effects.select_later_rows(LATER, {"SKILL_A", "SKILL_C"})
		merged, inserted = effects.merge_skill_effect_text(NATIVE, selected)
		self.assertEqual(inserted, 1)
		self.assertIn("SKILL_C", merged)
		self.assertEqual(merged.count("SKILL_A"), 1)

	# ================
	# test_rows_land_inside_their_section
	# ================
	def test_rows_land_inside_their_section(self):
		selected = effects.select_later_rows(LATER, {"SKILL_C"})
		merged, _inserted = effects.merge_skill_effect_text(NATIVE, selected)
		self.assertLess(merged.index("SKILL_C"), merged.index("skilleffectset"))
		self.assertIn("EFFECT_X", merged)

	# ================
	# test_disabled_and_unknown_service_rows_are_skipped
	# ================
	def test_disabled_and_unknown_service_rows_are_skipped(self):
		bases = {base for _section, base, _line in effects.animset_rows(LATER)}
		self.assertEqual(bases, {"SKILL_C", "SKILL_A"})
		self.assertNotIn("SKILL_DISABLED", effects.native_bases(NATIVE))

	# ================
	# test_unknown_animation_state_is_reported
	# ================
	def test_unknown_animation_state_is_reported(self):
		later_line = "1\tnew\tSKILL_C\tANI_SKILL_99\t0"
		self.assertEqual(effects.missing_animation_states([later_line], NATIVE), {"ANI_SKILL_99"})
		self.assertEqual(effects.missing_animation_states(["1\tnew\tSKILL_C\tANI_SKILL_1\t0"], NATIVE), set())


if __name__ == "__main__":
	unittest.main()
