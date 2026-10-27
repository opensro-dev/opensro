/*
===========================================================================

fertile_temple_deck_test.go - a short step off an open deck edge lands below

The Fertile Temple deck (region 0x6687, RN_TK_TEMPLE, object 1) leaves to the floor
through an open outline edge (flags 0). The native movement query ends a
continuation run once the leg's start is within 5 units of the goal and
returns that leg's own result - an outline exit (client 403FB0, 4044CE..4044FE)
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

// fertileTempleRegion is the Fertile Temple's region (textzonename 26247); its floor below the
// deck's north-west edge stands at about -40.40.
const (
	fertileTempleRegion = 0x6687
	fertileTempleFloorY = -40.40
)

/*
================
TestShortStepOffTheFertileTempleDeckLandsOnTheFloor

The live reports' two edge points, each stepped less than 5 units across
the open edge at deck height: the clip arrives at the exit point on the
floor, owned by terrain, with the continue bit and no block, and the
ground walker's clip accepts it there.
================
*/
func TestShortStepOffTheFertileTempleDeckLandsOnTheFloor(t *testing.T) {
	v := realAuthorityValidator(t)
	clip := &ClientClip{Mode: ClipApply, Validator: v}
	for _, step := range []struct{ fromX, fromZ, toX, toZ float64 }{
		{284.08, 483.24, 283.97, 483.82},
		{323.19, 501.42, 323.11, 502.14},
	} {
		from := simulation.Spawn{RegionID: fertileTempleRegion, X: step.fromX, Y: -35.18, Z: step.fromZ}
		owner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
		if !ok || owner.Kind != world.NavOwnerObject {
			t.Fatalf("fixture: %+v does not stand on the deck (%+v)", from, owner)
		}
		to := simulation.Spawn{RegionID: fertileTempleRegion, X: step.toX, Y: from.Y, Z: step.toZ}
		got := v.ClipMovementPathFrom(from, owner, to)
		if got.Outcome != ClipArrived || got.NativeResult&nativeContinueResult == 0 || got.NativeResult&0x10000001 != 0 {
			t.Fatalf("step off the edge at %+v: %s native %#x, want an arrived continuation", from, got.Outcome, got.NativeResult)
		}
		if got.RestOwner.Kind != world.NavOwnerTerrain || math.Abs(got.Rest.Y-fertileTempleFloorY) > 0.05 {
			t.Fatalf("step off the edge rests at %+v on %+v, want the floor (%.2f) on terrain", got.Rest, got.RestOwner, fertileTempleFloorY)
		}
		pose, poseOwner, blocked := clip.processOwnedStep("deck", from, owner, to)
		if blocked || pose != got.Rest || poseOwner != got.RestOwner {
			t.Fatalf("ground step off the edge: pose %+v owner %+v blocked %v", pose, poseOwner, blocked)
		}
	}
}

/*
================
TestShortStepOffTheDeckOntoABlockedTileStaysOnTheDeck

client 403FB0 tests the terrain tile under an outline exit point before the walker
continues on the terrain (client 4038D0, 4044D9 test al,1) and fails the leg on a
blocked one (404194: 0x10000000). The same short step as above, with the
floor tile under its exit point marked blocked, must stay on the deck at the
rim: a block, not an arrived continuation (opensro3's review of #842).
================
*/
func TestShortStepOffTheDeckOntoABlockedTileStaysOnTheDeck(t *testing.T) {
	v := realAuthorityValidator(t)
	clip := &ClientClip{Mode: ClipApply, Validator: v}
	from := simulation.Spawn{RegionID: fertileTempleRegion, X: 284.08, Y: -35.18, Z: 483.24}
	owner, _, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok || owner.Kind != world.NavOwnerObject {
		t.Fatalf("fixture: %+v does not stand on the deck (%+v)", from, owner)
	}
	to := simulation.Spawn{RegionID: fertileTempleRegion, X: 283.97, Y: from.Y, Z: 483.82}
	open := v.ClipMovementPathFrom(from, owner, to)
	if open.Outcome != ClipArrived {
		t.Fatalf("fixture: the open step is %s, want arrived", open.Outcome)
	}

	// Block the floor tile under the exit point in this validator's own grid.
	exit := world.ExpandGrid(world.RegionXZ{RegionID: open.Rest.RegionID, X: open.Rest.X, Z: open.Rest.Z})
	tilesPerAxis, tileSize, ok := v.gridParamsForRegion(fertileTempleRegion)
	if !ok {
		t.Fatal("fixture: the temple region has no walkability grid")
	}
	tile := globalTile{x: int(math.Floor(exit.X / tileSize)), z: int(math.Floor(exit.Z / tileSize))}
	grid := v.gridForSectors(tile.x/tilesPerAxis, tile.z/tilesPerAxis, tilesPerAxis)
	index := (tile.z%tilesPerAxis)*tilesPerAxis + tile.x%tilesPerAxis
	if grid == nil || !grid.tileWalkable(index) {
		t.Fatalf("fixture: the exit tile %+v is not a walkable floor tile", tile)
	}
	grid.walkable = append([]uint64(nil), grid.walkable...)
	grid.walkable[index/64] &^= uint64(1) << uint(index%64)

	got := v.ClipMovementPathFrom(from, owner, to)
	if got.Outcome != ClipBlocked || got.NativeResult != 0x10000000 || got.BlockedTileX != tile.x || got.BlockedTileZ != tile.z {
		t.Fatalf("step onto a blocked exit tile: %s native %#x tile (%d,%d), want blocked 0x10000000 at %+v",
			got.Outcome, got.NativeResult, got.BlockedTileX, got.BlockedTileZ, tile)
	}
	if got.RestOwner.Kind != world.NavOwnerObject || math.Abs(got.Rest.Y-from.Y) > 0.5 {
		t.Fatalf("step onto a blocked exit tile rests at %+v on %+v, want the deck rim", got.Rest, got.RestOwner)
	}
	pose, poseOwner, blocked := clip.processOwnedStep("deck", from, owner, to)
	if !blocked || poseOwner.Kind != world.NavOwnerObject {
		t.Fatalf("ground step onto a blocked exit tile: pose %+v owner %+v blocked %v, want held on the deck", pose, poseOwner, blocked)
	}
}
