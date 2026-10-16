/*
===========================================================================

petprofile_test.go - species, gear and mastery scale the actual attack block

===========================================================================
*/
package action

import (
	"encoding/binary"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"testing"
)

/*
================
TestManualPetStatusAbilitiesAndSharedCooldown
================
*/
func TestManualPetStatusAbilitiesAndSharedCooldown(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		id         uint8
		status     abnormal.Status
	}{
		{"howl", "COS_P_WOLF_040", 2, abnormal.Fear},
		{"bite", "COS_P_WOLF_040", 4, abnormal.Bleeding},
		{"nova", "COS_P_PENGUIN_040", 31, abnormal.Frostbite},
		{"freeze", "COS_P_PENGUIN_040", 34, abnormal.Freeze},
		{"mark", "COS_P_RAVEN_040", 49, abnormal.Decay},
		{"howl-III", "COS_P_WOLF_040", 3, abnormal.Fear},
		{"bite-III", "COS_P_WOLF_040", 6, abnormal.Bleeding},
		{"nova-III", "COS_P_PENGUIN_040", 33, abnormal.Frostbite},
		{"freeze-III", "COS_P_PENGUIN_040", 36, abnormal.Freeze},
		{"mark-III", "COS_P_RAVEN_040", 51, abnormal.Decay},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
			level := uint8(40)
			if tc.id%3 == 0 {
				level = 90
			}
			makeTrainedPet(rt, c, tc.code, level)
			rt.AttackPetExpansion = true
			skill := learnPetBook(t, c, tc.id)
			payload := binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, tc.id), mob.Gid)
			rt.HandleCosCommand(testDivision, c, payload)
			tickPetCombat(t, rt, clock, 200, func() bool { return !companion.RowReady(&c.ActiveCOS.Training, skill.Row, clock.NowMs()) })
			live, _ := rt.Monsters.Get(testDivision, mob.Gid)
			if live.Abnormal == nil || !live.Abnormal.Slots[tc.status].Active {
				t.Fatalf("%s status missing", tc.name)
			}
			before := c.ActiveCOS.Training.Rows[skill.Row]
			rt.HandleCosCommand(testDivision, c, payload)
			if before != c.ActiveCOS.Training.Rows[skill.Row] {
				t.Fatal("repeated cast changed row cooldown")
			}
		})
	}
}

/*
================
TestManualCrowMendHealsWithoutEnemy
================
*/
func TestManualCrowMendHealsWithoutEnemy(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 40)
	rt.AttackPetExpansion = true
	companion.UpgradeTree(c.ActiveCOS)
	if msg := companion.Learn(c.ActiveCOS, 46); msg != "" {
		t.Fatal(msg)
	}
	rt.deps.Update(c, "test-wound", func() bool { *c.CurrentHP = 10; return true })
	// makeTrainedPet replaces the fixture item source; its sword is no longer present.
	c.MissionInventory = nil
	rt.HandleCosCommand(testDivision, c, binary.LittleEndian.AppendUint32(trainingCommand(c.ActiveCOS.GID, petTrainingUse, 46), 0))
	if *c.CurrentHP <= 10 || companion.RowReady(&c.ActiveCOS.Training, 0, clock.NowMs()) {
		t.Fatalf("Mend: hp=%d row=%+v", *c.CurrentHP, c.ActiveCOS.Training.Rows[0])
	}
}

/*
================
TestPetProfileAndGearScaleActualSkillDamage
================
*/
func TestPetProfileAndGearScaleActualSkillDamage(t *testing.T) {
	rt, clock, c, mob := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_PENGUIN_040", 40)
	rt.AttackPetExpansion = true
	ref, _ := rt.cosReference(c.ActiveCOS)
	state := rt.petSessionFor(testDivision, c.Name, c.ActiveCOS.GID)
	step := petCombatStep{pet: c.ActiveCOS, snapshot: c, state: state, ref: ref, nowMs: clock.NowMs()}
	skill := enterworld.SkillRow{Attack: enterworld.SkillAttack{Flags: 4, Min: 100, Max: 200, Percent: 100}}
	target := petCombatTarget{monster: &mob}
	native, _ := rt.petTrainedDecide(step, skill, target)
	if native.Attack.Flags&8 == 0 || native.Attack.Flags&4 != 0 || native.Attack.Min != 160 {
		t.Fatalf("magic role: %+v", native.Attack)
	}
	companion.UpgradeTree(c.ActiveCOS)
	if msg := companion.Learn(c.ActiveCOS, 37); msg != "" {
		t.Fatal(msg)
	}
	c.ActiveCOS.Training.Gear[0] = domain.PetGear{RefObjID: companion.D.Gear[0].Items[0].ID}
	boosted, _ := rt.petTrainedDecide(step, skill, target)
	if boosted.Attack.Min <= native.Attack.Min {
		t.Fatal("mastery and gear did not improve actual magical skill damage")
	}
	rt.AttackPetExpansion = false
	off, trained := rt.petTrainedDecide(step, skill, target)
	if trained != nil || off.Attack != skill.Attack {
		t.Fatal("profiles changed native-off combat")
	}
}
