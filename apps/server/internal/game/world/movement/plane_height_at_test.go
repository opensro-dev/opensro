/*
===========================================================================

plane_height_at_test.go - Plane.HeightAt live-pose probes (#1695)

The live-pose height reads the surface the mover stands on: on the
Jangan veranda that is the deck at 3.04 over terrain at 0 (the bare
terrain sampler's answer, the old wiring), in a stacked cave column
the floor nearest the caller's own height (floors at 0 and 280), and
on the Hotan gate deck the caller's own height selects the deck
(243.99) over the terrain sloping away beneath it (243.04).
plane_height_test.go keeps the path sampler's parity (#1675); this
file pins the live-pose arm.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPlaneHeightAtReadsTheDeckNotTheTerrainBeneath
================
*/
func TestPlaneHeightAtReadsTheDeckNotTheTerrainBeneath(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	plane := NewPlanes(&Runtime{}, v).For(planeOutdoorRegion)

	// On the deck a mover's chord height is the deck's: the deck answers.
	onDeck := simulation.Spawn{RegionID: planeOutdoorRegion, X: 623, Y: verandaDeckY, Z: 1271}
	if y, ok := plane.HeightAt(onDeck); !ok || math.Abs(y-verandaDeckY) > 0.01 {
		t.Fatalf("deck HeightAt = (%v, %v), want (%v, true)", y, ok, verandaDeckY)
	}
	// The bare terrain sampler - the old wiring - answers 0 here; pin the
	// delta so the fix cannot silently revert to it.
	if y, ok := v.TerrainHeightAt(onDeck.RegionID, onDeck.X, onDeck.Z); !ok || math.Abs(y-verandaTerrainY) > 0.01 {
		t.Fatalf("fixture: TerrainHeightAt under the deck = (%v, %v), want (%v, true)", y, ok, verandaTerrainY)
	}

	// On open ground there is no deck to stand on: the plane answers
	// with the terrain sampler exactly (terrain-only parity).
	onGround := simulation.Spawn{RegionID: planeOutdoorRegion, X: 651, Y: verandaTerrainY, Z: 1271}
	wantY, wantOK := v.TerrainHeightAt(onGround.RegionID, onGround.X, onGround.Z)
	if !wantOK {
		t.Fatalf("fixture: no terrain answer at %+v", onGround)
	}
	if y, ok := plane.HeightAt(onGround); y != wantY || ok != wantOK {
		t.Fatalf("terrain HeightAt = (%v, %v), want the terrain sample (%v, %v)", y, ok, wantY, wantOK)
	}
}

/*
================
TestPlaneHeightAtReadsTheCallersCaveFloor
================
*/
func TestPlaneHeightAtReadsTheCallersCaveFloor(t *testing.T) {
	v, _, _ := dungeonValidators(t)
	plane := NewPlanes(&Runtime{}, v).For(planeCaveRegion)

	// One XZ, two floors (0 and 280): the caller's own height selects.
	upper := simulation.Spawn{RegionID: planeCaveRegion, X: -200, Y: 280.001, Z: -460}
	if y, ok := plane.HeightAt(upper); !ok || math.Abs(y-280) > 0.01 {
		t.Fatalf("upper-floor HeightAt = (%v, %v), want (280, true)", y, ok)
	}
	lower := simulation.Spawn{RegionID: planeCaveRegion, X: -200, Y: 0.001, Z: -460}
	if y, ok := plane.HeightAt(lower); !ok || math.Abs(y-0) > 0.01 {
		t.Fatalf("lower-floor HeightAt = (%v, %v), want (0, true)", y, ok)
	}
	// The old wiring: the terrain sampler has no cave answer at all, so
	// a live pose in a cave kept whatever height it arrived with.
	if _, ok := v.TerrainHeightAt(upper.RegionID, upper.X, upper.Z); ok {
		t.Fatal("fixture: the terrain sampler unexpectedly answers in a cave")
	}
}

/*
================
TestPlaneHeightAtCallerHeightSelectsTheSurface
================
*/
func TestPlaneHeightAtCallerHeightSelectsTheSurface(t *testing.T) {
	v := realAuthorityValidator(t)
	plane := NewPlanes(&Runtime{}, v).For(0x5B87)

	// The Hotan gate deck's south edge: the deck plane at 243.99 over
	// terrain at 243.04 (the stairs_regression_test fixture). A mover
	// whose chord height is the deck's reads the deck; one at the
	// terrain's height reads the terrain - the caller's height alone
	// selects the surface.
	onDeck := simulation.Spawn{RegionID: 0x5B87, X: 1122, Y: 243.99, Z: 1300.01}
	if y, ok := plane.HeightAt(onDeck); !ok || math.Abs(y-243.99) > 0.05 {
		t.Fatalf("deck-height HeightAt = (%v, %v), want the deck 243.99", y, ok)
	}
	onTerrain := simulation.Spawn{RegionID: 0x5B87, X: 1122, Y: 243.04, Z: 1300.01}
	if y, ok := plane.HeightAt(onTerrain); !ok || math.Abs(y-243.04) > 0.05 {
		t.Fatalf("terrain-height HeightAt = (%v, %v), want the terrain 243.04", y, ok)
	}
}
