/*
===========================================================================
petbalance_test.go - authored survival against same-level party giants.

Retail default attacks use their normal midpoint roll, without criticals,
passives or shields. This is a balancing benchmark, not a per-hit override.
===========================================================================
*/
package action

import (
	"fmt"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"testing"
)

/*
================
TestPetPartyGiantSurvival
================
*/
func TestPetPartyGiantSurvival(t *testing.T) {
	items, skills := shippedItems(t), shippedSkills(t)
	items.ApplyAttackPetProfiles()
	for i, lv := range []uint8{10, 20, 40, 60, 80, 90} {
		names := []string{"MOB_CH_YEOHA", "MOB_CH_CHAKJI", "MOB_OA_REDSCORPION", "MOB_KK_YETI_CLON", "MOB_RM_WINGTRIBE", "MOB_RM_TAHOMET"}
		mob, ok := items.CharacterRefByCodename(names[i])
		if !ok {
			t.Fatal(names[i])
		}
		skill, ok := skills.SkillByID(mob.Parameters.DefaultSkillIDs[0])
		if !ok {
			t.Fatal("missing default attack", names[i])
		}
		attacker, err := combat.MonsterStats(mob.Parameters)
		if err != nil {
			t.Fatal(err)
		}
		for _, kind := range []struct {
			code string
			hits uint32
		}{{"BEAR", 3}, {"WOLF", 2}, {"PENGUIN", 1}, {"RAVEN", 1}} {
			name := fmt.Sprintf("COS_P_%s_%03d", kind.code, lv)
			pet, ok := items.CharacterRefByCodename(name)
			if !ok {
				t.Fatal(name)
			}
			defender, err := cosCombatStats(pet, &domain.CharacterCOS{Level: lv, Satiety: 10000}, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			result, err := combat.ResolveMonster(attacker, defender, skill.Attack, func() (uint32, error) { return 16384, nil })
			if err != nil {
				t.Fatal(err)
			}
			result = combat.FinishImpact(result, combat.ImpactTail{Percent: 100, MonsterAttacker: true, AttackerRarity: 0x14, Attack: true})
			if result.Damage == 0 {
				t.Fatal("benchmark dealt no damage")
			}
			hits := (pet.MaxHP + result.Damage - 1) / result.Damage
			if hits != kind.hits {
				t.Errorf("%s HP=%d damage=%d dies in %d hits, want %d", name, pet.MaxHP, result.Damage, hits, kind.hits)
			}
		}
	}
}

/*
================
TestPetGearHealthProjection
================
*/
func TestPetGearHealthProjection(t *testing.T) {
	items := shippedItems(t)
	items.ApplyAttackPetProfiles()
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	equipShippedPet(t, rt, c, items, "COS_P_WOLF_090")
	c.ActiveCOS.Level = 90
	rt.AttackPetExpansion = true
	bare, _ := rt.cosReference(c.ActiveCOS)
	for _, d := range companion.D.Gear {
		for i, g := range d.Items {
			c.ActiveCOS.Training.Gear[i] = domain.PetGear{RefObjID: g.ID}
		}
		geared, _ := rt.cosReference(c.ActiveCOS)
		want := bare.MaxHP * uint32(d.Degree+1)
		if geared.MaxHP != want || geared.Parameters.MaxHP != want {
			t.Fatalf("D%d max HP=%d/%d, want %d", d.Degree, geared.MaxHP, geared.Parameters.MaxHP, want)
		}
	}
	rt.AttackPetExpansion = false
	off, _ := rt.cosReference(c.ActiveCOS)
	if off.MaxHP != bare.MaxHP {
		t.Fatal("gear changed expansion-off health")
	}
}

/*
================
TestPetGearHealthChangesDoNotCreateHealing
================
*/
func TestPetGearHealthChangesDoNotCreateHealing(t *testing.T) {
	rt, _, c, _ := newPetCombatRuntime(t, 10000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	bare, _ := rt.cosReference(c.ActiveCOS)
	c.ActiveCOS.CurrentHP = bare.MaxHP / 2
	before := c.ActiveCOS.CurrentHP
	gear := companion.D.Gear[8].Items[0]
	grantGear(t, rt, c, gear.ID)
	row, _ := bagItem(c, gear.Codename)
	train(rt, c, petTrainingEquip, uint8(row.Slot))
	worn, _ := rt.cosReference(c.ActiveCOS)
	if worn.MaxHP != bare.MaxHP*4 || c.ActiveCOS.CurrentHP != before {
		t.Fatal("equipping failed to raise capacity without healing")
	}
	c.ActiveCOS.CurrentHP = worn.MaxHP
	train(rt, c, petTrainingUnequip, 0)
	if c.ActiveCOS.CurrentHP != bare.MaxHP {
		t.Fatal("removing gear left HP above its maximum")
	}
}
