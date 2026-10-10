/*
===========================================================================

skillthreatdecrease_test.go - admission of the untargeted threat decrease

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

/*
================
TestShippedMirageTiersAreCasterThreatDecreases

Every Mirage and Phantasma tier is the untargeted form: caster-centred
efr shape 1 selecting monsters, a dtnt flat word, mwdt, a prepared cast.
================
*/
func TestShippedMirageTiersAreCasterThreatDecreases(t *testing.T) {
	source := sharedShippedSkills(t)
	tiers := 0
	for _, line := range []string{"SKILL_EU_WARLOCK_CONFUSIONA_AGGROLOW_A", "SKILL_EU_WARLOCK_CONFUSIONA_AGGROLOW_B"} {
		for tier := 1; ; tier++ {
			row, ok := source.SkillByCodename(fmt.Sprintf("%s_%02d", line, tier))
			if !ok {
				break
			}
			tiers++
			th := row.Threat
			if !th.Decrease || row.TargetRequired || th.Area.Shape != decreaseCasterAreaShape || th.Area.Select != decreaseAreaSelect ||
				th.Area.Radius == 0 || th.Area.MaxTargets == 0 || th.DecreaseFlat == 0 || th.DecreaseWeaponPercent == 0 ||
				row.ActionCastingTimeMs == 0 || row.OffenseRefusal != "" {
				t.Fatalf("%s: %+v refusal %q", row.Codename, th, row.OffenseRefusal)
			}
		}
	}
	if tiers != 8 {
		t.Fatalf("%d Mirage and Phantasma tiers, want 8", tiers)
	}
}
