/*
===========================================================================

spawnprotection_test.go - the untouchable grace after a revival

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestRevivalIsUntouchableForSixSeconds

4DF484: a revived player is body mode 2 (untouchable) for 6 s, published to
the player and its peers, then restored.
================
*/
func TestRevivalIsUntouchableForSixSeconds(t *testing.T) {
	character := rebirthTestCharacter(3, 0)
	rt, clock := newTestRuntime(character, testItems())
	result := rt.HandleLocalRebirth(testDivision, character, []byte{byte(wire.RebirthAtPresentPoint)})
	if character.NativeBodyStatus != untouchableBodyStatus {
		t.Fatalf("revived body status = %d, want untouchable", character.NativeBodyStatus)
	}
	published := func(frames []wire.Frame) bool {
		for _, frame := range frames {
			if frame.Opcode == wire.OpObjectStateRefresh {
				return true
			}
		}
		return false
	}
	if !published(result.Frames) || !published(result.Broadcast) {
		t.Fatalf("untouchable not published: %+v / %+v", result.Frames, result.Broadcast)
	}
	clock.Advance(reviveUntouchableMs*time.Millisecond - time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if character.NativeBodyStatus != untouchableBodyStatus {
		t.Fatal("untouchable ended early")
	}
	clock.Advance(time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if character.NativeBodyStatus != 0 {
		t.Fatalf("body status after the grace = %d, want restored", character.NativeBodyStatus)
	}
}
