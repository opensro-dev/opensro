/*
===========================================================================

skillhostiledebuff_test.go - admission of the enemy-targeted timed buff

===========================================================================
*/

package enterworld

import (
	"fmt"
	"testing"
)

/*
================
TestShippedVitalSpotTiersAreHostileDebuffs

Every Vital Spot tier compiles: Muscle (_A) lowers evasion, Spirit (_B)
hit rate, each for its authored duration with tant aggression, admitted
as an immediate direct offense.
================
*/
func TestShippedVitalSpotTiersAreHostileDebuffs(t *testing.T) {
	source := sharedShippedSkills(t)
	tiers := 0
	for _, line := range []string{"SKILL_CH_WATER_CANCEL_A", "SKILL_CH_WATER_CANCEL_B"} {
		for tier := 1; ; tier++ {
			row, ok := source.SkillByCodename(fmt.Sprintf("%s_%02d", line, tier))
			if !ok {
				break
			}
			tiers++
			d := row.HostileDebuff
			evasion := line == "SKILL_CH_WATER_CANCEL_A"
			if !d.Pinned || d.DurationMs != row.EffectDurationMs || d.ThreatFlat == 0 ||
				evasion != (d.Evasion != 0) || evasion == (d.HitRate != 0) ||
				!row.DirectOffensePinned || row.Attack.ImpactCount != 1 || row.OffenseRefusal != "" {
				t.Fatalf("%s: %+v refusal %q", row.Codename, d, row.OffenseRefusal)
			}
			tag, value := d.Word()
			if evasion && (tag != tagEvasionDecrease || value != d.Evasion) || !evasion && (tag != tagHitRateDecrease || value != d.HitRate) {
				t.Fatalf("%s word %x=%d", row.Codename, tag, value)
			}
		}
	}
	if tiers != 15 {
		t.Fatalf("%d Vital Spot tiers, want 15", tiers)
	}
}
