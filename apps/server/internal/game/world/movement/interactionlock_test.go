/*
===========================================================================

interactionlock_test.go - a ground command at a stall or in an exchange

While the player keeps a stall, visits one or trades, its ground command
is dropped silently until the stall or exchange closes (#3433).

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestHandleMoveDropsCommandWhileInteractionLocked
================
*/
func TestHandleMoveDropsCommandWhileInteractionLocked(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	locked := true
	rt.InteractionLocked = func(string, string) bool { return locked }

	start := simulation.EuropeStartProfile()
	body := encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))
	outcome := rt.HandleMove("0", character, body)
	if outcome.Refusal == nil || outcome.Refusal.Reason != "interactionLocked" || len(outcome.Frames) != 0 {
		t.Fatalf("move at a stall = %+v, want a silent interactionLocked refusal", outcome)
	}

	locked = false
	if outcome := rt.HandleMove("0", character, body); outcome.Refusal != nil {
		t.Fatalf("move after the stall closed refused: %v", outcome.Refusal)
	}
}
