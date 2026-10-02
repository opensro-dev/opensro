/*
===========================================================================

absorption_test.go - native mixed-lane absorption review probe

410BB0 prioritizes current-effect bit 4 in both damage helper calls.

===========================================================================
*/
package combat

import (
	"opensro.online/server/internal/game/enterworld"
	"testing"
)

/*
================
TestNativeMixedLaneAbsorption
================
*/
func TestNativeMixedLaneAbsorption(t *testing.T) {
	a := Stats{Level: 1, MaxLevel: 1, Strength: 32, Intellect: 32,
		PhysicalAttackMin: 100, PhysicalAttackMax: 100, MagicalAttackMin: 100, MagicalAttackMax: 100}
	d := Stats{Level: 1, MaxLevel: 1, PhysicalSkillTaken: 0.5, MagicalSkillTaken: 0.25}
	got, err := Resolve(a, d, enterworld.SkillAttack{Present: true, Flags: 14, Percent: 100},
		func() (uint32, error) { return 0, nil })
	if err != nil || got.Damage != 100 || got.MagicalDamage != 50 {
		t.Fatalf("mixed lane result = %+v, %v; native wants damage 100, magical 50", got, err)
	}
}

/*
================
TestAbsorptionRetainsAuthoredFlagsAcrossLaneMasks
================
*/
func TestAbsorptionRetainsAuthoredFlagsAcrossLaneMasks(t *testing.T) {
	a := Stats{Level: 1, MaxLevel: 1, Strength: 32, Intellect: 32,
		PhysicalAttackMin: 100, PhysicalAttackMax: 100, MagicalAttackMin: 100, MagicalAttackMax: 100}
	d := Stats{Level: 1, MaxLevel: 1, PhysicalSkillTaken: 0.5, MagicalSkillTaken: 0.25}
	roll := func() (uint32, error) { return 0, nil }
	attack := enterworld.SkillAttack{Present: true, Flags: 14, Percent: 100}
	// A chained victim suppresses physical damage, not the authored
	// physical-first absorption classification used by both helpers.
	chained, err := ResolveCalculation(a, d, AttackCalculation{
		Attack: attack, OriginalFlags: attack.Flags, Lanes: magicalAttackFlag, Player: true,
	}, roll)
	if err != nil || chained.Damage != 50 || chained.MagicalDamage != 50 {
		t.Fatalf("chained mixed attack = %+v, %v", chained, err)
	}
	// Physical wall coverage must not change the remaining magic helper's
	// classification, and character absorption must not protect the wall.
	split, err := ResolveAgainstWall(a, d, attack, enterworld.SkillWall{Mask: physicalAttackFlag}, roll, true, false)
	if err != nil || split.Absorbed != 100 || split.Defender.Damage != 50 {
		t.Fatalf("partially covered mixed attack = %+v, %v", split, err)
	}
}

/*
================
TestAbsorptionClassificationMatrix
================
*/
func TestAbsorptionClassificationMatrix(t *testing.T) {
	d := Stats{PhysicalBasicTaken: 0.1, PhysicalSkillTaken: 0.2, MagicalBasicTaken: 0.3, MagicalSkillTaken: 0.4}
	for _, tc := range []struct {
		effect, original uint32
		want             float32
	}{
		{4, 1, 0.1}, {4, 2, 0.2}, {8, 1, 0.3}, {8, 2, 0.4},
		{12, 1, 0.1}, {12, 2, 0.2}, {12, 3, 0.1},
		{0, 1, 0}, {4, 0, 0},
	} {
		got := defenseAbsorption(d, AttackCalculation{Attack: enterworld.SkillAttack{Flags: tc.effect}, OriginalFlags: tc.original})
		if got != tc.want {
			t.Fatalf("effect=%x original=%x factor=%v want=%v", tc.effect, tc.original, got, tc.want)
		}
	}
}
