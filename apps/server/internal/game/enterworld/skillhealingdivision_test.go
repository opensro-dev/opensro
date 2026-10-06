/*
===========================================================================

skillhealingdivision_test.go - Healing Division and Healing Favor admission

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

/*
================
TestShippedHealingDivisionRowsCompileAsLowestHeals

Every Healing Division (8) and Healing Favor (4) tier is an untargeted
efr(1,6,radius,cap,reduction,5) eshp heal, a lowest-ratio party heal:
Division hands 50 % to one more member, Favor 65 % to two more.
================
*/
func TestShippedHealingDivisionRowsCompileAsLowestHeals(t *testing.T) {
	source := sharedShippedSkills(t)
	for _, line := range []struct {
		line           string
		tiers          int
		cap, reduction uint32
	}{{"SKILL_EU_CLERIC_HEALA_DIVIDE_A", 8, 2, 50}, {"SKILL_EU_CLERIC_HEALA_DIVIDE_B", 4, 3, 35}} {
		for tier := 1; tier <= line.tiers; tier++ {
			code := fmt.Sprintf("%s_%02d", line.line, tier)
			row, ok := source.SkillByCodename(code)
			if !ok {
				t.Fatalf("missing %s", code)
			}
			area := row.Abnormal.EffectArea
			if row.Recovery != (SkillRecovery{LowestHealPinned: true}) || row.TargetRequired || !row.Heal.Present ||
				area.Shape != 6 || area.MaxTargets != line.cap || area.Reduction != line.reduction || area.Select != 5 {
				t.Fatalf("%s: recovery %+v area %+v", code, row.Recovery, area)
			}
		}
	}
}
