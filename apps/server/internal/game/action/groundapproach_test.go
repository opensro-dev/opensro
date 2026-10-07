/*
===========================================================================

groundapproach_test.go - delayed collision cannot cancel a newer command

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGroundApproachRetiresOnlyItsMovementRevision
================
*/
func TestGroundApproachRetiresOnlyItsMovementRevision(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	key := grounditem.PendingKey(testDivision, character.Name)
	intent := basicAttackIntent{
		DivisionID: testDivision, CharacterName: character.Name, TargetGid: 101,
		HasApproach: true, ApproachMovementRevision: 7,
	}
	rt.setCombatIntent(intent)
	rt.Pending.ArmGround(grounditem.Pending{
		Key: key, DivisionID: testDivision, CharacterName: character.Name,
		ItemGid: 202, ArrivesAt: clock.Now(), MovementRevision: 7,
	})
	rt.RetireGroundApproach(testDivision, character.Name, 6)
	rt.retireGroundApproaches()
	if !rt.combatIntentIsCurrent(intent) {
		t.Fatal("an old collision retired a newer combat approach")
	}
	if _, ok := rt.Pending.Peek(key); !ok {
		t.Fatal("an old collision retired a newer pickup approach")
	}
	rt.RetireGroundApproach(testDivision, character.Name, 7)
	rt.retireGroundApproaches()
	if len(rt.combatIntentSnapshot()) != 0 {
		t.Fatal("the matching blocked approach survived")
	}
	if _, ok := rt.Pending.Peek(key); ok {
		t.Fatal("the matching blocked pickup survived")
	}
	// Entering cast range clears HasApproach without replacing the target.
	// The old stop must not remove the stationary command's continuation.
	intent.HasApproach = false
	rt.setCombatIntent(intent)
	rt.RetireGroundApproach(testDivision, character.Name, 7)
	rt.retireGroundApproaches()
	if !rt.combatIntentIsCurrent(intent) {
		t.Fatal("a delayed collision retired an already stationary action")
	}
}

/*
================
TestGroundCollisionPreservesQueuedReplacementAndStationaryCommand
================
*/
func TestGroundCollisionPreservesQueuedReplacementAndStationaryCommand(t *testing.T) {
	for _, kind := range []string{"queued replacement", "new stationary command", "new pickup"} {
		t.Run(kind, func(t *testing.T) {
			c := testCharacter()
			rt, clock := newTestRuntime(c, testItems())
			key := simulation.WorldKey(testDivision, c.Name)
			front := basicAttackIntent{DivisionID: testDivision, CharacterName: c.Name, TargetGid: 101, HasApproach: true, ApproachMovementRevision: 7}
			next := basicAttackIntent{DivisionID: testDivision, CharacterName: c.Name, TargetGid: 202}
			rt.setCombatIntent(front)
			rt.actionSessions.Store(key, actionSessionPublication{division: testDivision, name: c.Name, characterID: c.ID})
			rt.RetireGroundApproach(testDivision, c.Name, 7)
			switch kind {
			case "queued replacement":
				rt.actionSessions.Store(key, actionSessionPublication{division: testDivision, name: c.Name, characterID: c.ID, queued: true, pending: &next})
			case "new stationary command":
				rt.setCombatIntent(next)
			case "new pickup":
				rt.finishCombatIntent(testDivision, c.Name)
				rt.actionSessions.Delete(key)
				rt.Pending.ArmGround(grounditem.Pending{Key: grounditem.PendingKey(testDivision, c.Name), DivisionID: testDivision, CharacterName: c.Name, ItemGid: 303, ArrivesAt: clock.Now(), MovementRevision: 8})
			}
			var frames []wire.Frame
			rt.PushCharacterFrames = func(_, _ string, published []wire.Frame) { frames = append(frames, published...) }
			rt.retireGroundApproaches()
			if kind == "queued replacement" {
				intent, exists := rt.combatIntentFor(testDivision, c.Name)
				if !exists || intent.TargetGid != next.TargetGid || len(frames) != 1 || frames[0].Opcode != wire.OpActionState || !bytes.Equal(frames[0].Payload, []byte{2, 1}) {
					t.Fatalf("queued replacement lost: intent=%+v frames=%+v", intent, frames)
				}
				if duplicate := rt.retireActionSessions(); len(duplicate) != 0 {
					t.Fatalf("collision repeated action release: %+v", duplicate)
				}
			} else {
				if len(frames) != 0 {
					t.Fatalf("old collision released newer command: %+v", frames)
				}
				if kind == "new stationary command" && !rt.combatIntentIsCurrent(next) {
					t.Fatal("stationary replacement retired")
				}
				if kind == "new pickup" {
					if pending, ok := rt.Pending.Peek(grounditem.PendingKey(testDivision, c.Name)); !ok || pending.MovementRevision != 8 {
						t.Fatal("new pickup retired")
					}
				}
			}
		})
	}
}
