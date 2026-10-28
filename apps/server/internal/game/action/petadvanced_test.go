/*
===========================================================================

petadvanced_test.go - advanced passive books through combat and healing owners

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
)

/*
================
learnPetBook

Use the real rank gates and automatic passive slot selection in the fixture.
================
*/
func learnPetBook(tb testing.TB, c *enterworld.Character, id uint8) companion.SkillDef {
	tb.Helper()
	companion.UpgradeTree(c.ActiveCOS)
	selected, ok := companion.PetSkill(c.ActiveCOS, id)
	if !ok {
		tb.Fatal("missing book")
	}
	for _, s := range companion.SkillsForPet(c.ActiveCOS) {
		if s.Row != selected.Row || s.Option > selected.Option {
			continue
		}
		ranks := int(s.MaxLevel)
		if s.ID == id {
			ranks = 1
		}
		for range ranks {
			if msg := companion.Learn(c.ActiveCOS, s.ID); msg != "" {
				tb.Fatal(msg)
			}
		}
	}
	return selected
}

/*
================
TestAdvancedPetPassivesCommitTheirStatuses

Real attack orders, simulation ticks and native status resolution, rather
than merely checking that the skill builder contains an abnormal parameter.
================
*/
func TestAdvancedPetPassivesCommitTheirStatuses(t *testing.T) {
	rt, clock, c, mob, key := tailwindFixture(t, 1)
	learnPetBook(t, c, 60)
	rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, mob.Gid))
	tickPetCombat(t, rt, clock, 2000, func() bool {
		return rt.petSessionFor(key.division, key.name, key.gid).crow.precision[enterworld.ObjectIDForCharacter(c)].stacks == 5
	})
}

/*
================
TestGuardianReplacementAndPlumeMagnitude
================
*/
func TestGuardianReplacementAndPlumeMagnitude(t *testing.T) {
	t.Run("guardian-does-not-retain-old-defense-bonus", func(t *testing.T) {
		rt, _, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_BEAR_040", 90)
		rt.AttackPetExpansion = true
		learnPetBook(t, c, 27)
		pet := c.ActiveCOS
		pet.CurrentHP = 400
		above := rt.petTrainingDefense(c, pet, 1000, combat.Stats{PhysicalDefense: 100, MagicalDefense: 100})
		pet.CurrentHP = 399
		below := rt.petTrainingDefense(c, pet, 1000, combat.Stats{PhysicalDefense: 100, MagicalDefense: 100})
		if above.PhysicalDefense != 100 || below.PhysicalDefense != 100 || below.MagicalDefense != 100 {
			t.Fatal("Guardian retained the replaced Last Stand defense bonus")
		}
		rt.AttackPetExpansion = false
		off := rt.petTrainingDefense(c, pet, 1000, combat.Stats{PhysicalDefense: 100})
		if off.PhysicalDefense != 100 {
			t.Fatal("Guardian changed native-off defense")
		}
	})
	t.Run("plume-II-initial-half", func(t *testing.T) {
		rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		roarField(t, rt, c, clock)
		skill := learnPetBook(t, c, 47)
		c.MissionInventory = nil
		*c.CurrentHP = 1
		stats, _, err := rt.playerCombatStats(testDivision, c)
		if err != nil {
			t.Fatal(err)
		}
		maxHP, _, _, _ := keeperVitalsFrom(c, stats, nil)
		expected := int64(1 + float64(maxHP)*skill.ValueAt(1)*0.5/100)
		rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, skill.ID), 0))
		if *c.CurrentHP != expected {
			t.Fatalf("healed to%d, want%d", *c.CurrentHP, expected)
		}
	})
}

/*
================
TestAdvancedPetStatPassivesReachCombat
================
*/
func TestAdvancedPetStatPassivesReachCombat(t *testing.T) {
	for _, tc := range []struct {
		code, kind string
		id         uint8
	}{
		{"COS_P_RAVEN_040", "speed", 57},
	} {
		t.Run(tc.code+tc.kind, func(t *testing.T) {
			rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, tc.code, 90)
			rt.AttackPetExpansion = true
			ref, _ := rt.cosReference(c.ActiveCOS)
			step := petCombatStep{pet: c.ActiveCOS, snapshot: c, ref: ref, state: rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID), nowMs: clock.NowMs()}
			attack := enterworld.SkillRow{Attack: enterworld.SkillAttack{Flags: 4, Min: 100, Max: 200, Percent: 100}}
			before, _ := rt.petTrainedDecide(step, attack, petCombatTarget{monster: &mob})
			selected := learnPetBook(t, c, tc.id)
			slotted, ok := companion.SlottedPassive(c.ActiveCOS, selected.Row)
			if !ok || slotted.ID != selected.ID {
				t.Fatal("highest learned book was not slotted")
			}
			switch tc.kind {
			case "defense":
				got := rt.petTrainingDefense(c, c.ActiveCOS, ref.MaxHP, combat.Stats{PhysicalDefense: 100, MagicalDefense: 100})
				if got.PhysicalDefense <= 100 || got.MagicalDefense <= 100 {
					t.Fatal("passive did not improve combat defense")
				}
			case "magic":
				after, _ := rt.petTrainedDecide(step, attack, petCombatTarget{monster: &mob})
				if after.Attack.Min <= before.Attack.Min {
					t.Fatal("mastery did not improve actual magical damage")
				}
			case "speed":
				if rt.petAttackSpeed(step) != 1 {
					t.Fatal("Tailwind changed the crow's attack interval")
				}
			}
		})
	}
}

/*
================
TestAdvancedExecutePreservesUniqueImmunity
================
*/
func TestAdvancedExecutePreservesUniqueImmunity(t *testing.T) {
	rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_WOLF_040", 90)
	rt.AttackPetExpansion = true
	learnPetBook(t, c, 9)
	ref, _ := rt.cosReference(c.ActiveCOS)
	step := petCombatStep{pet: c.ActiveCOS, snapshot: c, ref: ref, state: rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID), nowMs: clock.NowMs()}
	_, strike := rt.petTrainedDecide(step, enterworld.SkillRow{}, petCombatTarget{monster: &mob})
	mob.CurrentHP = 1000
	hit := []combat.Result{{Damage: 1}}
	rt.petTrainedDamage(strike, mob, hit)
	if hit[0].Damage != 1000 {
		t.Fatal("advanced Execute did not finish weak target")
	}
	mob.Ref.MonsterType = 3
	mob.Nest.HasRarityOverride = false
	hit = []combat.Result{{Damage: 1}}
	rt.petTrainedDamage(strike, mob, hit)
	if hit[0].Damage != 1 {
		t.Fatal("Execute bypassed unique immunity")
	}
}
