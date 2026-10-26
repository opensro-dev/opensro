/*
===========================================================================

plane_lookups_test.go - the data lookups dispatch through the planes (#1676)

Parity in the #1649 shape: on the real probes (the Jangan veranda
outdoors, the Donwhang cave anchors) the shared entry point, the plane
arm it dispatches to, and the primitive the arm wraps must all agree,
and the wrong plane's arm must not answer for a region - so a swapped
dispatch fails here, not in play.

===========================================================================
*/
package movement

import (
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPlaneLookupsOutdoorArms

The outdoor entry points answer through the outdoor arm, and the arm
answers with the primitives it wraps.
================
*/
func TestPlaneLookupsOutdoorArms(t *testing.T) {
	v := realAuthorityValidator(t)
	lookups := v.validatorPlanes()
	anchors := []simulation.Spawn{
		{RegionID: planeOutdoorRegion, X: 623, Y: 3.04, Z: 1271},
		{RegionID: planeOutdoorRegion, X: 680, Y: 3.04, Z: 1271},
	}
	for _, anchor := range anchors {
		gotH, gotOK := v.WalkableSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z)
		armH, armOK := lookups.outdoor.walkableSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z)
		if gotH != armH || gotOK != armOK {
			t.Fatalf("%+v entry height (%v, %t), outdoor arm (%v, %t)", anchor, gotH, gotOK, armH, armOK)
		}
		if !gotOK {
			t.Fatalf("%+v no walkable spawn height on the veranda anchor", anchor)
		}
		if caveH, caveOK := lookups.cave.walkableSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z); caveOK {
			t.Fatalf("%+v cave arm answered an outdoor region with %v", anchor, caveH)
		}
		gotBlock, gotOK := v.MessageBlockAt(anchor)
		armBlock, armOK := lookups.outdoor.messageBlockAt(anchor)
		wantBlock, wantOK := worldgeom.OutdoorMessageBlock(
			worldgeom.RegionXZ{RegionID: anchor.RegionID, X: anchor.X, Z: anchor.Z})
		if gotBlock != armBlock || gotOK != armOK || gotBlock != wantBlock || gotOK != wantOK {
			t.Fatalf("%+v entry block (%+v, %t), arm (%+v, %t), wrapped (%+v, %t)",
				anchor, gotBlock, gotOK, armBlock, armOK, wantBlock, wantOK)
		}
	}
	if !v.SpawnRegionAvailable(planeOutdoorRegion) || !lookups.outdoor.spawnRegionAvailable(planeOutdoorRegion) {
		t.Fatal("Jangan is not an available spawn region")
	}
	if v.SpawnRegionAvailable(0) || lookups.outdoor.spawnRegionAvailable(0) {
		t.Fatal("region 0 is an available spawn region")
	}
	seen := make(map[*groundSurface]bool)
	if !lookups.preloadRegion(planeOutdoorRegion, seen) {
		t.Fatal("the outdoor plane does not count Jangan in the preload set")
	}
}

/*
================
TestPlaneLookupsCaveArms

The cave entry points answer through the cave arm, and the height arm
answers with the resident dungeon host it wraps.
================
*/
func TestPlaneLookupsCaveArms(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	lookups := v.validatorPlanes()
	anchors := []simulation.Spawn{
		{RegionID: planeCaveRegion, X: 993.197, Y: 0.00034, Z: -307.099},
		{RegionID: planeCaveRegion, X: 629, Y: -2.22, Z: 74},
	}
	for _, anchor := range anchors {
		gotH, gotOK := v.WalkableSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z)
		armH, armOK := lookups.cave.walkableSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z)
		wantH, wantOK := v.dungeonSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z)
		if gotH != armH || gotOK != armOK || gotH != wantH || gotOK != wantOK {
			t.Fatalf("%+v entry height (%v, %t), cave arm (%v, %t), wrapped (%v, %t)",
				anchor, gotH, gotOK, armH, armOK, wantH, wantOK)
		}
		if !gotOK {
			t.Fatalf("%+v no cave floor under the anchor", anchor)
		}
		if outdoorH, outdoorOK := lookups.outdoor.walkableSpawnHeightAt(anchor.RegionID, anchor.X, anchor.Y, anchor.Z); outdoorOK {
			t.Fatalf("%+v outdoor arm answered a cave region with %v", anchor, outdoorH)
		}
		gotBlock, gotOK := v.MessageBlockAt(anchor)
		armBlock, armOK := lookups.cave.messageBlockAt(anchor)
		if gotBlock != armBlock || gotOK != armOK {
			t.Fatalf("%+v entry block (%+v, %t), cave arm (%+v, %t)", anchor, gotBlock, gotOK, armBlock, armOK)
		}
		if !gotOK {
			t.Fatalf("%+v no message block on the cave anchor", anchor)
		}
	}
	if !v.SpawnRegionAvailable(planeCaveRegion) || !lookups.cave.spawnRegionAvailable(planeCaveRegion) {
		t.Fatal("the Donwhang cave is not an available spawn region")
	}
	seen := make(map[*groundSurface]bool)
	if lookups.preloadRegion(planeCaveRegion, seen) {
		t.Fatal("the cave plane counts a cave region in the preload set")
	}
	if len(seen) != 0 {
		t.Fatal("the cave preload arm warmed an outdoor surface")
	}
}
