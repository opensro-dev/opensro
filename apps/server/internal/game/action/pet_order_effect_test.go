/*
===========================================================================

pet_order_effect_test.go - accepted pet orders retire the owner's effects

Repeated orders still raise the skill-cast event, even when the pet keeps
its current combat target. Rejected targets do not raise the event.

===========================================================================
*/
package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
)

/*
================
TestPetAttackOrderRetiresOwnerEffects
================
*/
func TestPetAttackOrderRetiresOwnerEffects(t *testing.T) {
	for _, repeated := range []bool{false, true} {
		for _, valid := range []bool{false, true} {
			for _, mask := range []uint8{effectEventMove, effectEventSkillCast} {
				t.Run(fmt.Sprintf("repeated=%t/valid=%t/mask=%d", repeated, valid, mask), func(t *testing.T) {
					rt, clock, owner, target := newPetCombatRuntime(t, 1000000, attackPetBand)
					gid := owner.ActiveCOS.GID
					if repeated {
						rt.HandleCosCommand(testDivision, owner, petAttackOrder(gid, target.Gid))
						if state := rt.petSessionFor(testDivision, owner.Name, gid); state.combat == nil {
							t.Fatal("initial order did not establish combat")
						}
					}
					const effectID = 900
					skill := enterworld.SkillRow{ID: effectID, Group: effectID, ReplacementPinned: true}
					skill.Replacement.Activity = 1
					skill.Replacement.EventCancelMask = mask
					rt.deps.SkillData().(staticSkillSource)[effectID] = skill
					if !rt.ApplyCharacterEffectPresentation(testDivision, owner.Name, effectID, 901, statuseffect.StateActive, false, EffectPresentation{Phase: 1}, clock.NowMs()) {
						t.Fatal("effect installation failed")
					}
					targetGID := target.Gid
					if !valid {
						targetGID = 0x7ffffff0
					}
					rt.HandleCosCommand(testDivision, owner, petAttackOrder(gid, targetGID))
					held := false
					for _, effect := range rt.effects.Snapshot(testDivision, owner.Name) {
						if effect.SkillID == effectID {
							held = true
						}
					}
					if want := !valid || mask != effectEventSkillCast; held != want {
						t.Fatalf("owner effect retained=%t, want %t after order at %d", held, want, clock.NowMs())
					}
				})
			}
		}
	}
}
