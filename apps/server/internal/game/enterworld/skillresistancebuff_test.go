/*
===========================================================================

skillresistancebuff_test.go - Holy Word, Holy Spell, Poison Circle and
Vein Circle admission

The four resistance buffs are dura + reat + real, the blocks Protection
authors as a passive: Holy Word on one friendly target, the other three on
the party around the caster.

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestShippedResistanceBuffsCompileAsTimedEffects
================
*/
func TestShippedResistanceBuffsCompileAsTimedEffects(t *testing.T) {
	source := sharedShippedSkills(t)
	lines := []struct {
		line     string
		tiers    int
		targeted bool
		reatMask uint32
		realMask uint32
	}{
		{"SKILL_EU_CLERIC_SAINTA_ABNORMAL_A", 8, true, 63, 0x17fafc0},
		{"SKILL_EU_CLERIC_SAINTA_ABNORMAL_B", 2, false, 63, 0x17fafc0},
		{"SKILL_EU_ROG_POISONA_GUARD_A", 5, false, 32, 0x1618600},
		{"SKILL_EU_ROG_POISONA_GUARD_B", 2, false, 32, 0x1618600},
	}
	for _, line := range lines {
		for tier := 1; tier <= line.tiers; tier++ {
			code := line.line + "_0" + string(rune('0'+tier))
			row, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatalf("missing %s", code)
			}
			e := row.TimedEffect
			if !e.Pinned || e.Targeted != line.targeted || e.Area.Present == line.targeted || e.Persistent ||
				e.Reat.Mask != line.reatMask || e.Reat.Value == 0 || e.Real.Mask != line.realMask || e.Real.Flat == 0 || e.Real.Grade == 0 {
				t.Fatalf("%s: %+v", code, e)
			}
		}
	}
}
