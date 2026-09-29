/*
===========================================================================

weaponthreat_test.go - native float inputs to the taunt aggression term

Expected physical weapon values retain the same spill sequence as the magical
lane but select variance index four. Neither result is a rounded display stat.

===========================================================================
*/

package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"testing"
)

/*
================
TestPhysicalWeaponThreatInputs

Fractional variance and noninteger plus gains distinguish keeper input from
rounded equipment display values. Ratio cases exercise both native clamps.
================
*/
func TestPhysicalWeaponThreatInputs(t *testing.T) {
	ref := &enterworld.ItemRef{Combat: &enterworld.ItemCombatRef{PhysicalAttack: enterworld.ItemAttackRange{
		Minimum: enterworld.ItemStatRange{Min: 100, Max: 120, PerPlus: 4.5},
		Maximum: enterworld.ItemStatRange{Min: 150, Max: 180, PerPlus: 4.5},
	}}}
	low, high := WeaponPhysicalAttack(ref, uint64(10)<<20, 3)
	if low != float32(119.95161437988281) || high != float32(173.1774139404297) {
		t.Fatal(low, high)
	}
	if got := PhysicalAbsorptionRatio(90, 400); got != float32(1.0206185579299927) {
		t.Fatal(got)
	}
	if PhysicalAbsorptionRatio(90, -1000) != 0 || PhysicalAbsorptionRatio(90, 1000) != float32(1.2) {
		t.Fatal("ratio clamp")
	}
}
