/*
===========================================================================

basicattack_release_test.go - attack release and movement ownership regressions

Exercise the production action owner and its native packet lifecycle.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestBasicAttackReleasedBoundaryLetsMovementSupersedeRetainedEngage
================
*/
func TestBasicAttackReleasedBoundaryLetsMovementSupersedeRetainedEngage(t *testing.T) {
	rt, clock, character, target := newCombatTestRuntime(t, 100)
	first := rt.HandleTargetInteract(testDivision, character,
		wire.BasicAttackEngage{TargetGid: target.Gid}.Encode())
	firstToken, _, _ := assertSkillDamageOpen(
		t, first.Frames, 2, enterworld.ObjectIDForCharacter(character), target.Gid,
	)

	releaseAt := clock.At(testBasicAttackActionDuration).UnixMilli()
	assertSkillCastClose(t, rt.TickHook()(releaseAt), testDivision, firstToken)

	// movement.HandleMove owns this callback in the composition root and calls
	// it before decoding 0x7738. Model that exact ownership transfer inside
	// the observable B505 -> next-tick admission window.
	rt.ClearCombatIntent(testDivision, character.Name)
	if routed := assertAndSeparateActionReleases(t, rt.TickHook()(releaseAt+1)); len(routed) != 0 {
		t.Fatalf("movement-superseded engage reacquired an action bracket: %+v", routed)
	}
	if intents := rt.combatIntentSnapshot(); len(intents) != 0 {
		t.Fatalf("movement release retained combat intent: %+v", intents)
	}
}
