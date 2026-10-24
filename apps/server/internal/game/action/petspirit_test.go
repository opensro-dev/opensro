/*
===========================================================================
petspirit_test.go - Resolve hit doors, HP lifetime and admitted empowerment.
===========================================================================
*/
package action

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetSpiritHitLifetimeAndTiers
================
*/
func TestPetSpiritHitLifetimeAndTiers(t *testing.T) {
	for tier := uint8(1); tier <= 3; tier++ {
		t.Run(fmt.Sprint(tier), func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			rt.AttackPetExpansion = true
			roarField(t, rt, c, clock)
			c.MissionInventory = nil
			learnPetBook(t, c, 21+tier)
			pet := c.ActiveCOS
			base, _ := rt.cosReference(pet)
			pet.CurrentHP = base.MaxHP / 2
			before := pet.CurrentHP
			block := rt.newCosAbnormalOwnerForPet(testDivision, c, pet, clock.NowMs())
			hit := cosHit{monsterSource: true, resolveHits: true, owner: c, pet: pet, ref: base, block: block,
				formulas: []combat.Result{{Damage: 1}}, displaceAt: -1}
			if !rt.applyCOSHitInDoor(testDivision, &hit, clock.NowMs()) || pet.ResolveStacks != 1 {
				t.Fatal("real hit did not grant Resolve")
			}
			next, _ := rt.cosReference(pet)
			if pet.CurrentHP != before-1+next.MaxHP-base.MaxHP || base.MaxHP == next.MaxHP {
				t.Fatal("new maximum was not filled exactly once")
			}
			for range 20 {
				rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
			}
			if pet.ResolveStacks != 10 || rt.spiritBraced(c, pet) != (tier >= 2) {
				t.Fatal("cap or full-stack tier gate")
			}
			cappedHP := pet.CurrentHP
			rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
			if pet.CurrentHP != cappedHP {
				t.Fatal("refresh at cap healed")
			}
			copy := c.Snapshot().ActiveCOS
			if copy.ResolveStacks != 10 {
				t.Fatal("authority snapshots lost transient stacks")
			}
			encoded, _ := json.Marshal(pet)
			decoded := *pet
			decoded.ResolveStacks = 0
			if err := json.Unmarshal(encoded, &decoded); err != nil || decoded.ResolveStacks != 0 {
				t.Fatal("Resolve persisted on the summoner")
			}
			key := petOwnerKey{division: testDivision, name: c.Name, gid: pet.GID}
			rt.advancePetSpirit(key, clock.NowMs()+14999)
			if pet.ResolveStacks != 10 {
				t.Fatal("premature expiry")
			}
			rt.advancePetSpirit(key, clock.NowMs()+15000)
			if pet.ResolveStacks != 0 || pet.CurrentHP > base.MaxHP || pet.CurrentHP == 0 {
				t.Fatal("expiry did not clamp safely")
			}
			rt.AttackPetExpansion = false
			pet.ResolveStacks = 10
			native, _ := rt.cosReference(pet)
			if native.MaxHP != base.MaxHP {
				t.Fatal("disabled expansion retained bonus maximum HP")
			}
			if len(rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())) != 0 {
				t.Fatal("native off changed")
			}
		})
	}
}

/*
================
TestPetSpiritLosingStacksRemovesTheBonusHP

setPetSpirit(pet, 0) is the one sink expiry, the empower spend, town
entry and the passive swap all run: a damaged bear loses the temporary
bonus HP with the maximum (floored at 1, never a kill, never a heal).
================
*/
func TestPetSpiritLosingStacksRemovesTheBonusHP(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	learnPetBook(t, c, 24)
	pet := c.ActiveCOS
	base, _ := rt.cosReference(pet)
	pet.CurrentHP = base.MaxHP / 2
	earned := pet.CurrentHP
	for range 10 {
		rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
	}
	buffed, _ := rt.cosReference(pet)
	if pet.ResolveStacks != 10 || buffed.MaxHP <= base.MaxHP || pet.CurrentHP != earned+(buffed.MaxHP-base.MaxHP) {
		t.Fatalf("stacks did not raise and fill the maximum: %d/%d", pet.CurrentHP, buffed.MaxHP)
	}
	rt.setPetSpirit(pet, 0)
	if pet.CurrentHP != earned {
		t.Fatalf("losing stacks kept bonus HP: %d, want exactly the earned %d of %d", pet.CurrentHP, earned, base.MaxHP)
	}
	// A bear damaged below the bonus amount floors at 1, never dies.
	for range 10 {
		rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
	}
	pet.CurrentHP = 3
	rt.setPetSpirit(pet, 0)
	if pet.CurrentHP != 1 {
		t.Fatalf("bonus removal killed or healed: %d, want floor 1", pet.CurrentHP)
	}
}

/*
================
TestPetSpiritRoarReachAndCapacity
================
*/
func TestPetSpiritRoarReachAndCapacity(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	near := roarField(t, rt, c, clock)
	c.MissionInventory = nil
	learnPetBook(t, c, 24)
	learnPetBook(t, c, 18)
	pet := c.ActiveCOS
	for range 10 {
		rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
	}
	at := rt.LiveSpawnFor(testDivision, c)
	at.X += 420 // Outside ordinary tier III's 30m, inside empowered 45m.
	lease, _ := rt.casterPopulation(testDivision, c)
	rt.Monsters.SpawnGMMonsters(simulation.GMMonsterSpawn{Division: testDivision, Population: lease,
		RefObjID: near.Ref.RefObjID, Count: 10, Position: at, NowMs: clock.NowMs()})
	rt.manualPetSkill(testDivision, c, c.Snapshot(), pet.GID, 18, 0, clock.NowMs())
	clock.now = clock.now.Add(time.Second)
	state := rt.petSessionFor(testDivision, c.Name, pet.GID)
	ref, _ := rt.cosReference(pet)
	rt.advancePetRoar(petCombatStep{key: petOwnerKey{division: testDivision, name: c.Name, gid: pet.GID},
		state: state, snapshot: c, pet: pet, ref: ref, nowMs: clock.NowMs()})
	targets := 0
	for _, mob := range rt.Monsters.CombatCandidatesInPopulation(testDivision, lease, at, 500, clock.NowMs(), false) {
		if mob.Opponents[0].GID == pet.GID {
			targets++
		}
	}
	if targets != 9 {
		t.Fatalf("empowered Roar selected %d, want9 including targets beyond30m", targets)
	}
}

/*
================
TestPetSpiritShieldAndRoarAdmission
================
*/
func TestPetSpiritShieldAndRoarAdmission(t *testing.T) {
	for _, shield := range []bool{false, true} {
		t.Run(fmt.Sprint(shield), func(t *testing.T) {
			rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
			makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
			rt.AttackPetExpansion = true
			roarField(t, rt, c, clock)
			c.MissionInventory = nil
			learnPetBook(t, c, 24)
			pet := c.ActiveCOS
			for range 10 {
				rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
			}
			id := uint8(18)
			if shield {
				id = 21
			}
			skill := learnPetBook(t, c, id)
			result := rt.manualPetSkill(testDivision, c, c.Snapshot(), pet.GID, id, 0, clock.NowMs())
			if len(result.Broadcast) == 0 || pet.ResolveStacks != 0 {
				t.Fatal("admitted cast did not spend full Resolve")
			}
			if shield {
				row := rt.petShields.rows[petShieldKey{testDivision, pet.GID}]
				if row.amount != uint32(float64(row.maxHP)*skill.ValueAt(1)/100*1.5) {
					t.Fatal("shield capacity not empowered")
				}
			} else {
				state := rt.petSessionFor(testDivision, c.Name, pet.GID)
				if state.roar == nil || !state.roar.empowered || state.roar.releaseAtMs != clock.NowMs()+1000 {
					t.Fatal("empowered roar lost windup")
				}
			}
			for range 10 {
				rt.gainPetSpirit(testDivision, c, pet, clock.NowMs())
			}
			rt.manualPetSkill(testDivision, c, c.Snapshot(), pet.GID, id, 0, clock.NowMs())
			if pet.ResolveStacks != 10 {
				t.Fatal("cooldown refusal spent stacks")
			}
			clock.now = clock.now.Add(time.Second)
			block := rt.newCosAbnormalOwnerForPet(testDivision, c, pet, clock.NowMs())
			hit := cosHit{monsterSource: true, resolveHits: true, owner: c, pet: pet, block: block, displaceAt: -1,
				formulas: []combat.Result{{Blocked: true}}, records: []abnormal.Record{{Status: abnormal.Slow, Grade: 1, DurationMs: 1000}}}
			rt.applyCOSHitInDoor(testDivision, &hit, clock.NowMs())
			if block.block.Slots[abnormal.Slow].Active {
				t.Fatal("full Resolve admitted a new slow")
			}
			rt.HandleCosCommand(testDivision, c, trainingCommand(pet.GID, petTrainingSlot, 22))
			if pet.ResolveStacks != 10 {
				t.Fatal("rejected passive selection changed Resolve")
			}
			c.BattleUntilMs = 0
			rt.petSessionFor(testDivision, c.Name, pet.GID).combat = nil
			rt.HandleCosCommand(testDivision, c, trainingCommand(pet.GID, petTrainingRefund, 0))
			ref, _ := rt.cosReference(pet)
			if pet.ResolveStacks != 0 || pet.CurrentHP > ref.MaxHP {
				t.Fatal("refund retained stacks or HP above the new maximum")
			}

		})
	}
}
