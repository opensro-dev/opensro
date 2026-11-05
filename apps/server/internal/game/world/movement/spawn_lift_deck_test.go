/*
===========================================================================

spawn_lift_deck_test.go - a lifted spawn stands on the ground floor

The GM map warp sends an unknown height below every terrain; the enter-world
lift used to stand it on the bare heightmap even under Hotan's plaza deck,
and the client then walked the terrain down under the plaza. These tests
run the lift over the real exported authority.

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// mapWarpUnknownY is the client's MAP_WARP_UNKNOWN_Y (map-teleport.ts).
const mapWarpUnknownY = -10000

/*
================
TestSpawnLiftStandsOnTheHotanPlazaDeck

A map warp into Hotan's plaza (0x5C87, 729.7, 679.4) lands on the plaza
deck at ~244.29, not the terrain at ~243.63 under it.
================
*/
func TestSpawnLiftStandsOnTheHotanPlazaDeck(t *testing.T) {
	validator := realAuthorityValidator(t)
	entry := enterworld.LocalPlayerEntry{StartProfile: enterworld.StartProfile{
		RegionID: 0x5C87, X: 729.7000122070312, Y: mapWarpUnknownY, Z: 679.4000244140625,
	}}
	terrainY, ok := validator.TerrainHeightAt(0x5C87, entry.StartProfile.X, entry.StartProfile.Z)
	if !ok || math.Abs(terrainY-243.63) > 0.05 {
		t.Fatalf("terrain under the plaza = (%v, %v), want ~243.63", terrainY, ok)
	}
	if !enterworld.LiftSpawnAboveTerrain(&entry, validator.TerrainHeightAt, validator.WalkableSpawnHeightAt) {
		t.Fatal("an unknown-height warp must lift")
	}
	if got := entry.StartProfile.Y; math.Abs(got-244.287) > 0.05 {
		t.Fatalf("lifted height = %v, want the plaza deck ~244.29 (terrain %v)", got, terrainY)
	}
}

/*
================
TestSpawnLiftKeepsTheTerrainUnderAHighDeck

The ground-floor reach never climbs to a deck far above the terrain: a warp
under the Hotan palace deck (~265.2 over terrain near -45.6) stays on the
terrain, as the terrain-only lift did.
================
*/
func TestSpawnLiftKeepsTheTerrainUnderAHighDeck(t *testing.T) {
	validator := realAuthorityValidator(t)
	entry := enterworld.LocalPlayerEntry{StartProfile: enterworld.StartProfile{
		RegionID: 0x5E87, X: 1145, Y: mapWarpUnknownY, Z: 576,
	}}
	terrainY, ok := validator.TerrainHeightAt(0x5E87, 1145, 576)
	if !ok {
		t.Fatal("no terrain under the palace deck")
	}
	enterworld.LiftSpawnAboveTerrain(&entry, validator.TerrainHeightAt, validator.WalkableSpawnHeightAt)
	if got := entry.StartProfile.Y; got != terrainY {
		t.Fatalf("lifted height = %v, want the terrain %v", got, terrainY)
	}
}
