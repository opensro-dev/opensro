/*
===========================================================================

pet_protection_test.go - protected bodies retire companion combat

Exercise the command and tick owners so protection acquired during approach
or preparation cannot be bypassed by an already accepted attack order.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetOrderChecksProtectedBodies
================
*/
func TestPetOrderChecksProtectedBodies(t *testing.T) {
	for _, body := range []uint8{0, 1, 2, 3, 4} {
		for _, protectPet := range []bool{false, true} {
			t.Run(fmt.Sprintf("body=%d/pet=%t", body, protectPet), func(t *testing.T) {
				rt, _, owner, victim := newPvpPair(t)
				equipCombatTestPet(t, rt, owner, attackPetBand)
				rt.BindPetSession(testDivision, owner, 1)
				victim.NativeBodyStatus = body
				if protectPet {
					owner.ActiveCOS.NativeBodyStatus = untouchableBodyStatus
				}
				rt.HandleCosCommand(testDivision, owner, petAttackOrder(owner.ActiveCOS.GID, enterworld.ObjectIDForCharacter(victim)))
				state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
				want := !protectPet && body < 2
				if (state.combat != nil) != want {
					t.Fatalf("combat=%+v, want admitted=%t", state.combat, want)
				}
			})
		}
	}
}

/*
================
TestPetProtectionRetiresPreparedAttack
================
*/
func TestPetProtectionRetiresPreparedAttack(t *testing.T) {
	for _, band := range []uint16{attackPetBand, domain.MercenaryBand} {
		t.Run(fmt.Sprintf("band=%d", band), func(t *testing.T) {
			rt, clock, owner, target := newPetCombatRuntime(t, 1000000, band)
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.ActionCastingTimeMs, skill.ActionCastingTimePinned = 500, true
			skill.ActionRange = 1000
			skills[2] = skill
			state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
			if band == attackPetBand {
				rt.HandleCosCommand(testDivision, owner, petAttackOrder(owner.ActiveCOS.GID, target.Gid))
			}
			tickPetCombat(t, rt, clock, 30, func() bool { return state.combat != nil && state.combat.castToken != 0 })
			token := state.combat.castToken
			before, _ := rt.Monsters.Get(testDivision, target.Gid)
			owner.ActiveCOS.NativeBodyStatus = untouchableBodyStatus
			closed := 0
			for range 15 {
				clock.now = clock.now.Add(100 * time.Millisecond)
				for _, batch := range rt.TickHook()(clock.NowMs()) {
					for _, frame := range batch.Frames {
						if frame.Opcode != wire.OpSkillEffectControl {
							continue
						}
						if len(frame.Payload) >= 5 && frame.Payload[0] == 1 && binary.LittleEndian.Uint32(frame.Payload[1:]) == token {
							t.Fatal("protected companion released its prepared cast")
						}
						if bytes.Equal(frame.Payload, wire.SkillCastFinalizeFrame(token).Payload) {
							closed++
						}
					}
				}
				if state.combat != nil {
					t.Fatal("protected companion retained combat")
				}
			}
			after, _ := rt.Monsters.Get(testDivision, target.Gid)
			if closed != 1 || before.CurrentHP != after.CurrentHP {
				t.Fatalf("finalizes=%d HP=%d -> %d", closed, before.CurrentHP, after.CurrentHP)
			}
		})
	}
}

/*
================
TestPlayerProtectionRetiresPreparedPetAttack
================
*/
func TestPlayerProtectionRetiresPreparedPetAttack(t *testing.T) {
	for _, body := range []uint8{2, 3, 4} {
		t.Run(fmt.Sprintf("body=%d", body), func(t *testing.T) {
			rt, clock, owner, victim := newPvpPair(t)
			ref := equipCombatTestPet(t, rt, owner, attackPetBand)
			ref.Parameters.DefaultSkillIDs[0] = 2
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			rt.BindPetSession(testDivision, owner, 1)
			gid := owner.ActiveCOS.GID
			rt.HandleCosCommand(testDivision, owner, petAttackOrder(gid, enterworld.ObjectIDForCharacter(victim)))
			state := rt.petSessionFor(testDivision, owner.Name, gid)
			if state.combat == nil {
				t.Fatal("initial attack order refused")
			}
			at := rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs())
			state.follower = simulation.NewPetFollower(gid, at)
			skills := rt.deps.SkillData().(staticSkillSource)
			skill := skills[2]
			skill.ActionCastingTimeMs, skill.ActionCastingTimePinned = 500, true
			skill.ActionRange = 1000
			skills[2] = skill
			step := petCombatStep{
				key:   petOwnerKey{division: testDivision, name: owner.Name, gid: gid},
				state: state, snapshot: owner, pet: owner.ActiveCOS, ref: ref, run: ref.RunSpeed, nowMs: clock.NowMs(),
			}
			if _, handled := rt.advancePetCombat(step); !handled || state.combat == nil || state.combat.castToken == 0 {
				t.Fatal("control did not prepare an attack against the unprotected player")
			}
			victim.NativeBodyStatus = body
			step.nowMs += 100
			_, handled := rt.advancePetCombat(step)
			if handled || state.combat != nil || *victim.CurrentHP != 100 {
				t.Fatalf("protection left handled=%t combat=%+v HP=%d", handled, state.combat, *victim.CurrentHP)
			}
		})
	}
}
