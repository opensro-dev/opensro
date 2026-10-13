/*
===========================================================================

jupiter_deck_test.go - a short step off an open deck edge lands below

The Jupiter temple deck (region 0x6687, object 1) leaves to the floor
through an open outline edge (flags 0). QueryMovement (98B300) ends a
continuation run once the leg's start is within 5 units of the goal and
returns that leg's own result - an outline exit (403FB0, 4044CE..4044FE)
continues, it does not fail - so the ground walker's per-tick steps across
the edge must land on the floor, not stop at the rim (#831).

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// jupiterTempleRegion is the Jupiter temple's region; its floor below the
// deck's north-west edge stands at about -40.40.
const (
	jupiterTempleRegion = 0x6687
	jupiterFloorY       = -40.40
)

/*
================
TestShortStepOffTheJupiterDeckLandsOnTheFloor

The live reports' two edge points, each stepped less than 5 units across
the open edge at deck height: the clip arrives at the exit point on the
floor, owned by terrain, with the continue bit and no block, and the
ground walker's clip accepts it there.
================
*/
func TestShortStepOffTheJupiterDeckLandsOnTheFloor(t *testing.T) {
	v := realAuthorityValidator(t)
	clip := &ClientClip{Mode: ClipApply, Validator: v}
	for _, step := range []struct{ fromX, fromZ, toX, toZ float64 }{
		{284.08, 483.24, 283.97, 483.82},
		{323.19, 501.42, 323.11, 502.14},
	} {
		from := simulation.Spawn{RegionID: jupiterTempleRegion, X: step.fromX, Y: -35.18, Z: step.fromZ}
		owner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
		if !ok || owner.Kind != world.NavOwnerObject {
			t.Fatalf("fixture: %+v does not stand on the deck (%+v)", from, owner)
		}
		to := simulation.Spawn{RegionID: jupiterTempleRegion, X: step.toX, Y: from.Y, Z: step.toZ}
		got := v.ClipMovementPathFrom(from, owner, to)
		if got.Outcome != ClipArrived || got.NativeResult&nativeContinueResult == 0 || got.NativeResult&0x10000001 != 0 {
			t.Fatalf("step off the edge at %+v: %s native %#x, want an arrived continuation", from, got.Outcome, got.NativeResult)
		}
		if got.RestOwner.Kind != world.NavOwnerTerrain || math.Abs(got.Rest.Y-jupiterFloorY) > 0.05 {
			t.Fatalf("step off the edge rests at %+v on %+v, want the floor (%.2f) on terrain", got.Rest, got.RestOwner, jupiterFloorY)
		}
		pose, poseOwner, blocked := clip.processOwnedStep("deck", from, owner, to)
		if blocked || pose != got.Rest || poseOwner != got.RestOwner {
			t.Fatalf("ground step off the edge: pose %+v owner %+v blocked %v", pose, poseOwner, blocked)
		}
	}
}
