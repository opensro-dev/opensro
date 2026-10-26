/*
===========================================================================

plane_monster_test.go - the moved monster sites match their old branches

#1764 moved four simulation sites off the dungeon region bit: the create
row's frame, the acquisition blocks, the help call's sight row and the
pet's same-floor test. Each test here runs the site's former body beside
the new dispatch over outdoor, cave and cross-plane inputs.

===========================================================================
*/
package simulation

import (
	"math"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// planeTestRegions mixes two outdoor sectors and two cave floors.
var planeTestRegions = []uint16{0x62a8, 0x62a9, 0x8001, 0x8004}

/*
================
TestSpawnFrameMatchesTheLegacyBranch
================
*/
func TestSpawnFrameMatchesTheLegacyBranch(t *testing.T) {
	legacy := func(spawn Spawn) (float64, float64) {
		x := clampFloat(spawn.X, 0, 0xffff)
		z := clampFloat(spawn.Z, 0, 0xffff)
		if worldgeom.IsDungeonRegion(spawn.RegionID) {
			x, z = spawn.X, spawn.Z
		}
		return x, z
	}
	for _, region := range planeTestRegions {
		for _, v := range []float64{-300.5, 0, 12.25, 1919.9, 70000} {
			spawn := Spawn{RegionID: region, X: v, Y: 1, Z: -v}
			wantX, wantZ := legacy(spawn)
			gotX, gotZ := spawnFrameXZ(spawn)
			if math.Float64bits(gotX) != math.Float64bits(wantX) || math.Float64bits(gotZ) != math.Float64bits(wantZ) {
				t.Errorf("%#x (%v, %v): got (%v, %v), want (%v, %v)", region, spawn.X, spawn.Z, gotX, gotZ, wantX, wantZ)
			}
		}
	}
}

/*
================
TestAcquisitionBlocksMatchTheLegacyBranch
================
*/
func TestAcquisitionBlocksMatchTheLegacyBranch(t *testing.T) {
	legacy := func(from monster.Pose, sight uint32) acquisitionBlockSet {
		if worldgeom.IsDungeonRegion(from.RegionID) {
			return dungeonAcquisitionBlocks(from)
		}
		return outdoorAcquisitionBlocks(from, sight)
	}
	for _, region := range planeTestRegions {
		for _, sight := range []uint32{0, 40, 400} {
			from := monster.Pose{RegionID: region, X: 960, Z: 30}
			if got, want := acquisitionBlocks(from, sight), legacy(from, sight); got != want {
				t.Errorf("%#x sight %d: got %+v, want %+v", region, sight, got, want)
			}
		}
	}
}

/*
================
TestPetSamePlaneMatchesTheLegacyBranch
================
*/
func TestPetSamePlaneMatchesTheLegacyBranch(t *testing.T) {
	legacy := func(a, b uint16) bool {
		return worldgeom.SamePlane(a, b) && (!worldgeom.IsDungeonRegion(a) || a == b)
	}
	for _, a := range planeTestRegions {
		for _, b := range planeTestRegions {
			if got, want := petSamePlane(a, b), legacy(a, b); got != want {
				t.Errorf("petSamePlane(%#x, %#x) = %v, want %v", a, b, got, want)
			}
		}
	}
}

/*
================
TestHelpCallSightFollowsTheLegacyBranch

The help call's sight test ran where the region was not a cave.
================
*/
func TestHelpCallSightFollowsTheLegacyBranch(t *testing.T) {
	for _, region := range planeTestRegions {
		if got, want := FeaturesAt(region).HelpCallSight, !worldgeom.IsDungeonRegion(region); got != want {
			t.Errorf("%#x: HelpCallSight %v, want %v", region, got, want)
		}
	}
}
