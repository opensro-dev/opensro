/*
===========================================================================

moveevent_test.go - an accepted move ends the move-cancelled effects

CGObjChar_HandleMoveCommand (4B0EA0) raises event bit 1 through
CSkillManager_RetireEffectsForEventMask (5A16C0) once a move is accepted.

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestMoveEventEndsOnlyMoveCancelledEffects

Two casts of White Hawk Summon's buff: one whose skc event mask holds the
move bit, one whose mask does not. The move ends the first alone.
================
*/
func TestMoveEventEndsOnlyMoveCancelledEffects(t *testing.T) {
	for _, mask := range []uint8{effectEventMove, effectEventSkillCast} {
		rt, c, _, _, _ := arrowFixture(t)
		skill := shippedOffense(t, "SKILL_CH_BOW_CALL_A_01")
		skill.Replacement.EventCancelMask = mask
		rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
		c.Skills = append(c.Skills, skill.ID)
		c.CurrentMP = testInt64(10000)
		rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
		holds := func() bool {
			for _, effect := range rt.effects.Snapshot(testDivision, c.Name) {
				if effect.SkillID == skill.ID {
					return true
				}
			}
			return false
		}
		if !holds() {
			t.Fatalf("mask %d: the buff was not installed", mask)
		}
		rt.RetireMoveEffects(testDivision, c.Name, rt.Now().UnixMilli())
		if want := mask != effectEventMove; holds() != want {
			t.Fatalf("mask %d: after the move the buff stands = %v, want %v", mask, holds(), want)
		}
	}
}
