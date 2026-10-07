/*
===========================================================================

groundapproach_test.go - delayed collision cannot cancel a newer command

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/grounditem"
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
