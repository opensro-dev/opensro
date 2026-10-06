/*
===========================================================================

skillcrossbow_test.go - projectile admission refuses malformed bolt programs

Catalog coverage lives with action execution. These synthetic programs isolate
the equipment, count, range, and complete-graph boundaries without game assets.

===========================================================================
*/

package enterworld

import "testing"

/*
================
TestCrossbowProgramAdmission

Linked stage admission does not grant root execution: OffensiveSequence owns
the complete graph. Positive bolt counts fit the wire's equipment stack word.
================
*/
func TestCrossbowProgramAdmission(t *testing.T) {
	for _, mode := range []string{"single", "area-count", "linked", "zero", "overflow", "arrow", "weapon", "range-negative", "range-overflow", "no-ammo", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			fields := make([]string, 118)
			for i := range fields {
				fields[i] = "0"
			}
			fields[0], fields[16] = "1", "400"
			copy(fields[69:], []string{"6386804", "6", "51", "34", "42", "1", "1668182893", "4", "2", "1", "29301", "150"})
			row := SkillRow{CombatPinned: true, TimingPinned: true, ActionRangePinned: true, TargetRequired: true,
				ProjectileSpeed: 400, ActionHandler: SkillActionProjectile, RequiredWeaponKinds: [2]uint8{12, 255}, Attack: SkillAttack{Present: true, ImpactCount: 1}}
			switch mode {
			case "area-count":
				fields[78] = "3"
			case "linked":
				row.ChainSub = true
			case "zero":
				fields[78] = "0"
			case "overflow":
				fields[78] = "65536"
			case "arrow":
				fields[77] = "1"
			case "weapon":
				row.RequiredWeaponKinds[0] = 13
			case "range-negative":
				fields[80] = "-1"
			case "range-overflow":
				fields[80] = "4294967296"
			case "no-ammo":
				for i := 75; i <= 80; i++ {
					fields[i] = "0"
				}
			case "unknown":
				fields[81] = "99999"
			}
			parseSkillOffense(fields, &row)
			want := mode == "single" || mode == "area-count" || mode == "linked"
			if row.OffensiveStagePinned != want {
				t.Fatalf("admitted=%v, want %v; refusal=%s", row.OffensiveStagePinned, want, row.OffenseRefusal)
			}
		})
	}
}
