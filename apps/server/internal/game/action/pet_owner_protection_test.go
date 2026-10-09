/*
===========================================================================

pet_owner_protection_test.go - owner protection retires attacks on companions

Exercise the combat tick after admission, independent of command routing.
Protection on the victim owner must close a prepared companion cast.

===========================================================================
*/
package action

import (
	"bytes"
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCompanionOwnerProtectionRetiresPreparedPetAttack
================
*/
func TestCompanionOwnerProtectionRetiresPreparedPetAttack(t *testing.T) {
	for _, body := range []uint8{0, 1, 2, 3, 4, 5} {
		t.Run(fmt.Sprintf("body=%d", body), func(t *testing.T) {
			rt, clock, owner, victim := newPvpPair(t)
			ref := equipCombatTestPet(t, rt, owner, attackPetBand)
			ref.Parameters.DefaultSkillIDs[0] = 2
			rt.CombatRoll = func() (uint32, error) { return 0, nil }
			rt.BindPetSession(testDivision, owner, 1)
			pet := *owner.ActiveCOS
			var valid bool
			pet.GID, valid = enterworld.CosObjectIDForCharacter(victim)
			if !valid {
				t.Fatal("victim has no companion identity")
			}
			victim.ActiveCOS = &pet
			rt.BindPetSession(testDivision, victim, 1)
			gid := owner.ActiveCOS.GID
			state := rt.petSessionFor(testDivision, owner.Name, gid)
			at := rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs())
			state.follower = simulation.NewPetFollower(gid, at)
			// Seed an admitted AI target: this fixture owns ongoing validation,
			// including targets supplied by acquisition rather than a command.
			state.combat = &petCombatIntent{target: pet.GID, nextAttackMs: clock.NowMs()}
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
				t.Fatal("control did not prepare an attack against the unprotected companion")
			}
			token, hp := state.combat.castToken, pet.CurrentHP
			victim.NativeBodyStatus = body
			step.nowMs += 100
			_, handled := rt.advancePetCombat(step)
			if body < mercenaryBodyProtectedFirst || body > mercenaryBodyProtectedLast {
				if !handled || state.combat == nil || state.combat.castToken != token {
					t.Fatal("unprotected owner cancelled the prepared companion cast")
				}
				return
			}
			if handled || state.combat != nil || pet.CurrentHP != hp {
				t.Fatalf("owner protection left handled=%t combat=%+v HP=%d -> %d", handled, state.combat, hp, pet.CurrentHP)
			}
			step.nowMs += 1000
			rt.advancePetCombat(step)
			closed := 0
			for _, batch := range rt.drainSkillFinalizes(step.nowMs) {
				for _, frame := range batch.Frames {
					if frame.Opcode == wire.OpSkillEffectControl && bytes.Equal(frame.Payload, wire.SkillCastFinalizeFrame(token).Payload) {
						closed++
					}
				}
			}
			if closed != 1 || pet.CurrentHP != hp {
				t.Fatalf("finalizes=%d companion HP=%d -> %d", closed, hp, pet.CurrentHP)
			}
		})
	}
}
