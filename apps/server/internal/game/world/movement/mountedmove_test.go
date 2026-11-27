/*
===========================================================================

mountedmove_test.go - a rider's ground command is ignored (U8-16)

The original's move handler ignores a player's ground command while it
rides: the ride moves by its own COS command, never the walker's. A
plain command from a character on a live mount is refused silently,
exactly like a foreign owner's mount command.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestHandleMoveDropsCommandWhileMounted
================
*/
func TestHandleMoveDropsCommandWhileMounted(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	start := simulation.EuropeStartProfile()
	body := encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))

	if outcome := rt.HandleMove("0", character, body); outcome.Refusal != nil {
		t.Fatalf("unmounted move refused: %v", outcome.Refusal)
	}

	character.ActiveCOS = &enterworld.CharacterCOS{GID: 123, Mounted: true, Summoned: true, CurrentHP: 100}
	outcome := rt.HandleMove("0", character, body)
	if outcome.Refusal == nil || outcome.Refusal.Reason != "mounted" || len(outcome.Frames) != 0 {
		t.Fatalf("mounted move = %+v, want a silent mounted refusal", outcome)
	}

	character.ActiveCOS.Mounted = false
	if outcome := rt.HandleMove("0", character, body); outcome.Refusal != nil {
		t.Fatalf("dismounted move refused: %v", outcome.Refusal)
	}
}
