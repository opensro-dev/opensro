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
tomb population stamped. The server spends the rate on equipment alone
(monsterloot.go's equipment gate, prepareUniqueDrops' count), so a tomb
elite (grade 6) or unique (grade 8) publishes its ordinary passes at the
native count with the rate on its own field, and a tomb unique's prepass
equipment counts times the rate. Gold heaps (one per native pass) and
consumable families read the native passes and never see the rate.
Every other grade, and the native rate 1, publish the native numbers.
================
*/
func TestPharaohGearRateScalesTheTombDrops(t *testing.T) {
	ref := func(grade uint8, rate uint8) monster.MonsterRef {
		return monster.MonsterRef{Codename: "MOB_TOMB_TEST", Level: 100, MonsterType: grade,
			PharaohGearRate: rate}
	}
	variant := func(grade uint8, rate uint8) MonsterVariant {
		variants := monsterVariants(ref(grade, rate), false)
		if len(variants) != 1 {
			t.Fatalf("grade %d: %d variants, want 1", grade, len(variants))
		}
		return variants[0]
	}
	for _, grade := range []uint8{6, 8} {
		geared, native := variant(grade, 5), variant(grade, 1)
		if geared.Passes != native.Passes {
			t.Errorf("grade %d passes at rate 5 = %d, want the native %d (the rate is equipment-only; gold heaps and consumables read these passes)",
				grade, geared.Passes, native.Passes)
		}
		if geared.GearRate != 5 {
			t.Errorf("grade %d gearRate at rate 5 = %d, want 5 published on its own field", grade, geared.GearRate)
		}
		if native.GearRate != 0 {
			t.Errorf("grade %d gearRate at rate 1 = %d, want the field absent at the native rate", grade, native.GearRate)
		}
	}
	if got := variant(3, 5); got.GearRate != 0 || got.Passes != variant(3, 1).Passes {
		t.Errorf("grade 3 at rate 5 = passes %d gearRate %d, want the native passes and no gear rate (the rate takes grades 6 and 8 only)",
			got.Passes, got.GearRate)
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
	// The runtime reads the grade's low nibble (Instance.Rarity), so a
	// party nibble on the authored byte changes nothing it rolls; the
	// published bonus must read the same nibble.
	if flagged := uniqueBonus(ref(0x18, 3)); flagged == nil || *flagged != *geared {
		t.Errorf("party-flagged unique bonus %+v, want the grade-8 bonus %+v (the runtime reads the low nibble)",
			flagged, geared)
	}
	if bonus := uniqueBonus(ref(6, 3)); bonus != nil {
		t.Errorf("grade 6 published a unique bonus %+v; it runs no prepass", bonus)
	}
	if bonus := uniqueBonus(ref(3, 3)); bonus == nil || bonus.EquipmentMax != uniqueBonus(ref(3, 1)).EquipmentMax {
		t.Error("grade 3 equipment counts moved with the rate; it takes grades 6 and 8 only")
	}
}
