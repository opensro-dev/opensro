/*
===========================================================================

pharaoh_test.go - the catalogue models the Pharaoh Tomb gear rate

===========================================================================
*/
package catalogue

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestPharaohGearRateScalesTheTombDrops

SRO_PHARAOH_TOMB_GEAR_RATE (port-only) rides the monster reference the
tomb population stamped: a tomb elite (grade 6) or unique (grade 8)
publishes its ordinary passes times the rate, and a tomb unique's
prepass equipment counts times the rate - the two multiplications the
server applies (action.pharaohGearRate, prepareUniqueDrops). Every
other grade, and the native rate 1, publish the native numbers.
================
*/
func TestPharaohGearRateScalesTheTombDrops(t *testing.T) {
	ref := func(grade uint8, rate uint8) monster.MonsterRef {
		return monster.MonsterRef{Codename: "MOB_TOMB_TEST", Level: 100, MonsterType: grade,
			PharaohGearRate: rate}
	}
	passes := func(grade uint8, rate uint8) int {
		variants := monsterVariants(ref(grade, rate), false)
		if len(variants) != 1 {
			t.Fatalf("grade %d: %d variants, want 1", grade, len(variants))
		}
		return variants[0].Passes
	}
	for _, grade := range []uint8{6, 8} {
		if got, want := passes(grade, 3), 3*passes(grade, 1); got != want {
			t.Errorf("grade %d passes at rate 3 = %d, want %d (the native %d times the rate)", grade, got, want, passes(grade, 1))
		}
	}
	if got, want := passes(3, 3), passes(3, 1); got != want {
		t.Errorf("grade 3 passes at rate 3 = %d, want the native %d (the rate takes grades 6 and 8 only)", got, want)
	}

	native := uniqueBonus(ref(8, 1))
	geared := uniqueBonus(ref(8, 3))
	if native == nil || geared == nil {
		t.Fatal("a grade-8 tomb monster has no unique bonus")
	}
	if geared.EquipmentMin != 3*native.EquipmentMin || geared.EquipmentMax != 3*native.EquipmentMax {
		t.Errorf("unique equipment at rate 3 = %d-%d, want %d-%d (the native counts times the rate)",
			geared.EquipmentMin, geared.EquipmentMax, 3*native.EquipmentMin, 3*native.EquipmentMax)
	}
	if geared.RecipesMin != native.RecipesMin || geared.ScrollsMin != native.ScrollsMin {
		t.Error("the rate scaled the prepass recipes or scrolls; it multiplies equipment only")
	}
	if bonus := uniqueBonus(ref(6, 3)); bonus != nil {
		t.Errorf("grade 6 published a unique bonus %+v; it runs no prepass", bonus)
	}
	if bonus := uniqueBonus(ref(3, 3)); bonus == nil || bonus.EquipmentMax != uniqueBonus(ref(3, 1)).EquipmentMax {
		t.Error("grade 3 equipment counts moved with the rate; it takes grades 6 and 8 only")
	}
}
