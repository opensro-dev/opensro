/*
===========================================================================

petadvanced_test.go - advanced passive books through combat and healing owners

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/abnormal"
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
func learnPetBook(t *testing.T, c *enterworld.Character, id uint8) companion.SkillDef {
	t.Helper()
	companion.UpgradeTree(c.ActiveCOS)
	selected, ok := companion.PetSkill(c.ActiveCOS, id)
	if !ok {
		t.Fatal("missing book")
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
				t.Fatal(msg)
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
	for _, tc := range []struct {
		name, code string
		id         uint8
		status     abnormal.Status
	}{
		{"static-feathers-III", "COS_P_PENGUIN_040", 42, abnormal.ElectricShock},
		{"murder-of-crows-III", "COS_P_RAVEN_040", 60, abnormal.Myopia},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, tc.code, 90)
			rt.AttackPetExpansion = true
			selected := learnPetBook(t, c, tc.id)
			rt.HandleCosCommand(testDivision, c, petAttackOrder(c.ActiveCOS.GID, mob.Gid))
			tickPetCombat(t, rt, clock, 2000, func() bool {
				live, ok := rt.Monsters.Get(testDivision, mob.Gid)
				return ok && live.Abnormal != nil && live.Abnormal.Slots[tc.status].Active
			})
			if companion.Level(c.ActiveCOS, selected) != 1 {
				t.Fatal("combat changed learned ranks")
			}
		})
	}
}

/*
================
TestAdvancedWolfConditionAndCrowKillHealing
================
*/
func TestAdvancedWolfConditionAndCrowKillHealing(t *testing.T) {
	t.Run("carrion-heals-only-on-kill", func(t *testing.T) {
		rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		learnPetBook(t, c, 54)
		c.MissionInventory = nil
		*c.CurrentHP = 1
		ref, _ := rt.cosReference(c.ActiveCOS)
		step := petCombatStep{pet: c.ActiveCOS, snapshot: c, ref: ref, state: rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID), nowMs: clock.NowMs()}
		_, strike := rt.petTrainedDecide(step, enterworld.SkillRow{}, petCombatTarget{monster: &mob})
		rt.petTrainedCommit(step, c, c.ActiveCOS, strike, 10, false, false)
		if *c.CurrentHP != 1 {
			t.Fatal("nonlethal attack healed owner")
		}
		_, strike = rt.petTrainedDecide(step, enterworld.SkillRow{}, petCombatTarget{monster: &mob})
		rt.petTrainedCommit(step, c, c.ActiveCOS, strike, 10, true, false)
		if *c.CurrentHP <= 1 {
			t.Fatal("killing hit did not heal owner")
		}
	})
}

/*
================
TestAdvancedMarkRechargesAndRecastsAfterActualAttacks
================
*/
func TestAdvancedMarkRechargesAndRecastsAfterActualAttacks(t *testing.T) {
	rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	skill := learnPetBook(t, c, 50)
	cast := binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, skill.ID), mob.Gid)
	rt.HandleCosCommand(testDivision, c, cast)
	tickPetCombat(t, rt, clock, 200, func() bool { return c.ActiveCOS.Training.Rows[1].Left > 0 })
	if c.ActiveCOS.Training.Rows[1].Left != 12 {
		t.Fatal("cast paid part of its own recharge")
	}
	tickPetCombat(t, rt, clock, 2000, func() bool { return c.ActiveCOS.Training.Rows[1].Left == 0 })
	rt.HandleCosCommand(testDivision, c, cast)
	tickPetCombat(t, rt, clock, 200, func() bool { return c.ActiveCOS.Training.Rows[1].Left > 0 })
	live, _ := rt.Monsters.Get(testDivision, mob.Gid)
	if live.Abnormal == nil || !live.Abnormal.Slots[abnormal.Decay].Active {
		t.Fatal("recharged Mark did not apply")
	}
}

/*
================
TestGuardianReplacementAndMendMagnitude
================
*/
func TestGuardianReplacementAndMendMagnitude(t *testing.T) {
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
	t.Run("mend-II-heals-inherited-extra-half", func(t *testing.T) {
		rt, _, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
		makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
		rt.AttackPetExpansion = true
		skill := learnPetBook(t, c, 47)
		c.MissionInventory = nil
		*c.CurrentHP = 1
		stats, _, err := rt.playerCombatStats(testDivision, c)
		if err != nil {
			t.Fatal(err)
		}
		maxHP, _, _, _ := keeperVitalsFrom(c, stats, nil)
		expected := int64(1 + float64(maxHP)*skill.ValueAt(1)*1.5/100)
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
		{"COS_P_PENGUIN_040", "defense", 45},
		{"COS_P_PENGUIN_040", "magic", 39},
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
				if rt.petAttackSpeed(step) <= 1 {
					t.Fatal("Swift Wings did not shorten attack intervals")
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
