/*
===========================================================================

healbonus_test.go - tests for healbonus.go

===========================================================================
*/

package combat

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// Expected values are float32 evaluations of the native weapon install,
// heal formula and heal-bonus sequences, computed outside this package.
func TestWeaponHealBonusNativeSequence(t *testing.T) {
	ref := &enterworld.ItemRef{Combat: &enterworld.ItemCombatRef{
		MagicalAttack: enterworld.ItemAttackRange{
			Minimum: enterworld.ItemStatRange{Min: 100, Max: 120, PerPlus: 4.5},
			Maximum: enterworld.ItemStatRange{Min: 150, Max: 180, PerPlus: 4.5},
		},
	}}
	full := uint64(31) << 25 // variance index 5
	if lo, hi := WeaponMagicalAttack(ref, full, 3); lo != 133.5 || hi != 193.5 {
		t.Fatalf("full variance %v %v", lo, hi)
	}
	if lo, hi := WeaponMagicalAttack(ref, uint64(10)<<25, 3); lo != float32(119.95161437988281) || hi != float32(173.1774139404297) {
		t.Fatalf("variance 10 %v %v", lo, hi)
	}
	if r := AbsorptionRatio(90, 400); r != float32(1.0309277772903442) {
		t.Fatalf("ratio %v", r)
	}
	if r := AbsorptionRatio(90, 500); r != float32(1.2) {
		t.Fatalf("cap %v", r)
	}
	if r := AbsorptionRatio(90, -1); r != 0 {
		t.Fatalf("floor %v", r)
	}
	if got := WeaponHealBonus(133.5, 193.5, AbsorptionRatio(90, 400), 105); got != 176 {
		t.Fatalf("bonus %d", got)
	}
}
