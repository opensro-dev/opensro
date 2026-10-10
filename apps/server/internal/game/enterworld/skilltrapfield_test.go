/*
===========================================================================

skilltrapfield_test.go - admission of the Rogue's Poison Trap field

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

const (
	poisonTrapDurationMs = 30000
	poisonTrapPulseMs    = 3000
	poisonTrapRadius     = 100
	poisonTrapTargets    = 5
)

/*
================
TestShippedPoisonTrapTiersAreTrapFields

Every Poison Trap tier is a pulsing field (dura, puls, efr kind 3, ps); no
tier is a combat trap, and the Fire Trap is no trap field.
================
*/
func TestShippedPoisonTrapTiersAreTrapFields(t *testing.T) {
	source := sharedShippedSkills(t)
	tiers := 0
	for tier := 1; ; tier++ {
		row, ok := source.SkillByCodename(fmt.Sprintf("SKILL_EU_ROG_POISONA_FIELD_A_%02d", tier))
		if !ok {
			break
		}
		tiers++
		f := row.TrapField
		if !f.Pinned || row.CombatTrap.Pinned || f.DurationMs != poisonTrapDurationMs || f.PulseMs != poisonTrapPulseMs ||
			f.Radius != poisonTrapRadius || f.MaxTargets != poisonTrapTargets || f.Select != statusCastSelect || !row.Abnormal.Present() {
			t.Fatalf("%s: %+v", row.Codename, f)
		}
	}
	if tiers == 0 {
		t.Fatal("no Poison Trap rows")
	}
	if row, _ := source.SkillByCodename("SKILL_EU_WIZARD_FIREA_TRAP_A_01"); row.TrapField.Pinned || !row.CombatTrap.Pinned {
		t.Fatal("Fire Trap admitted as a trap field")
	}
}
