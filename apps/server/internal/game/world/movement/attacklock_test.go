/*
===========================================================================

attacklock_test.go - a ground command during a cast is dropped

CGObjPC_IsMotionChangeLocked (4EF880) feeds CGObjChar_IsAttackLocked
(4AAB40) to CGObjChar_HandleMoveCommand (4B0EA0): while the casting
instance is held, 0x7021 is dropped, not queued.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestHandleMoveDropsCommandWhileAttackLocked
================
*/
func TestHandleMoveDropsCommandWhileAttackLocked(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	locked := true
	rt.AttackLocked = func(string, string) bool { return locked }

	start := simulation.EuropeStartProfile()
	body := encodeMoveBody(1, start.RegionID, int16(start.X)+200, int16(start.Y), int16(start.Z))
	outcome := rt.HandleMove("0", character, body)
	if outcome.Refusal == nil || outcome.Refusal.Reason != "attackLocked" || len(outcome.Frames) != 0 {
		t.Fatalf("move during a cast = %+v, want a silent attackLocked refusal", outcome)
	}

	locked = false
	if outcome := rt.HandleMove("0", character, body); outcome.Refusal != nil {
		t.Fatalf("move after the cast released refused: %v", outcome.Refusal)
	}
}
