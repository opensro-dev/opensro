/*
===========================================================================

petgearstats_test.go - equipment changes actual attack and defense projections

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestPetEquipmentReachesCombat
================
*/
func TestPetEquipmentReachesCombat(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_040", 90)
	rt.AttackPetExpansion = true
	pet := c.ActiveCOS
	before := rt.petTrainingDefense(c, pet, 1000, combat.Stats{PhysicalDefense: 100, MagicalDefense: 100, EvasionRate: 10})
	for slot, g := range companion.D.Gear[8].Items {
		pet.Training.Gear[slot] = domain.PetGear{RefObjID: g.ID}
	}
	mods := companion.ModifiersFor(pet)
	after := rt.petTrainingDefense(c, pet, 1000, combat.Stats{PhysicalDefense: 100, MagicalDefense: 100, EvasionRate: 10})
	profile := companion.Profile(pet.Codename)
	if after.PhysicalDefense != before.PhysicalDefense+mods.GearStats.PhysicalDefense*profile.DefensePercent/100 {
		t.Fatal("species defense was applied twice")
	}
	if after.PhysicalDefense <= before.PhysicalDefense || after.MagicalDefense <= before.MagicalDefense || after.EvasionRate != 10+mods.GearStats.ParryRatio {
		t.Fatal("equipment defense/parry missing")
	}
	strike := &petTrainedStrike{codename: pet.Codename, mods: mods}
	attacker, _ := petTrainedStats(strike, monster.Instance{}, combat.Stats{PhysicalAttackMin: 100, MagicalAttackMin: 100, HitRate: 10}, combat.Stats{})
	phys, mag := companion.AttackScales(pet.Codename, mods)
	if attacker.PhysicalAttackMin != (100+mods.GearStats.PhysicalAttack)*phys || attacker.MagicalAttackMin != (100+mods.GearStats.MagicalAttack)*mag || attacker.HitRate != 10+mods.GearStats.HitRatio {
		t.Fatal("equipment attacks/hit missing")
	}
	rt.AttackPetExpansion = false
	if got := rt.petTrainingDefense(c, pet, 1000, combat.Stats{PhysicalDefense: 100}); got.PhysicalDefense != 100 {
		t.Fatal("gear changed native-off defense")
	}
}
