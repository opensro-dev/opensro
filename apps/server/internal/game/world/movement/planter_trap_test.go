/*
===========================================================================

planter_trap_test.go - a deck walk stops at a closed planter's wall (#3668)

A new character walked south from Constantinople's west gate (region
0x694F) across a raised walkable plaza object (asset 1609) and went
through the wall of a closed planter beside it (asset 1639). Inside,
every wall blocks, so the character could never walk out.

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"

	"opensro.online/server/internal/game/world/simulation"
)

const (
	constantinopleWestRegion   = 26959
	constantinoplePlanterAsset = 1639
)

/*
================
TestPublishedConstantinoplePlanterWallStopsADeckWalk

The reported walk: from the gate street toward the plaza, ending inside
the planter. It must stop outside it, at a pose InsideClosedObject does
not enclose, from which walking back north arrives.
================
*/
func TestPublishedConstantinoplePlanterWallStopsADeckWalk(t *testing.T) {
	clientdata.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	start := simulation.Spawn{RegionID: constantinopleWestRegion, X: 1083, Y: 83, Z: 913}
	inside := simulation.Spawn{RegionID: constantinopleWestRegion, X: 1027, Y: 78, Z: 190}
	if _, closed := v.InsideClosedObject(inside); !closed {
		t.Fatalf("the reported end %+v is no longer inside a closed object", inside)
	}
	got := v.ClipMovementPath(start, inside)
	if got.Outcome != ClipBlocked || got.Class != ClipClassObject {
		t.Fatalf("walk into the planter: %+v", got)
	}
	// The wall is crossed near z 205: the walk must reach it, not stop
	// at an earlier terrain-cell visit.
	if got.Rest.Z < 205 || got.Rest.Z > 210 {
		t.Fatalf("walk stopped away from the planter wall: %+v", got.Rest)
	}
	if asset, closed := v.InsideClosedObject(got.Rest); closed {
		t.Fatalf("walk rests inside closed object %d at %+v", asset, got.Rest)
	}
	if back := v.ClipMovementPath(got.Rest, start); back.Outcome != ClipArrived {
		t.Fatalf("walking back out from %+v: %+v", got.Rest, back)
	}
}

/*
================
TestPublishedConstantinoplePlanterLinkedKeepsTheDeckPass

A linked placement is not a closed object: with the planter given a link
edge in a private validator, the deck walk passes it as before.
================
*/
func TestPublishedConstantinoplePlanterLinkedKeepsTheDeckPass(t *testing.T) {
	clientdata.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	set := v.objectNavSetForOffset(v.surfaceForRegion(constantinopleWestRegion), 0, 0)
	linked := 0
	for i := range set {
		if set[i].placement.assetID == constantinoplePlanterAsset {
			set[i].placement.linkEdgeCount = 1
			linked++
		}
	}
	if linked == 0 {
		t.Fatal("missing the published planter placement")
	}
	start := simulation.Spawn{RegionID: constantinopleWestRegion, X: 1083, Y: 83, Z: 913}
	inside := simulation.Spawn{RegionID: constantinopleWestRegion, X: 1027, Y: 78, Z: 190}
	if got := v.ClipMovementPath(start, inside); got.Outcome != ClipArrived {
		t.Fatalf("a linked planter blocked the deck walk: %+v", got)
	}
}
