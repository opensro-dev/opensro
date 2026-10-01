/*
===========================================================================

spawnregion_test.go - native region-clamp ordering and lifecycle integration

===========================================================================
*/
package simulation

import "testing"

/*
================
TestGeneratedSpawnRegionUsesEightAlternativesThenOrigin
================
*/
func TestGeneratedSpawnRegionUsesEightAlternativesThenOrigin(t *testing.T) {
	origin := Spawn{RegionID: 0x62aa, X: 1915, Y: 20, Z: 200}
	generated := Spawn{RegionID: 0x62ab, X: 5, Y: 20, Z: 200}
	queries := 0
	got := ClampGeneratedSpawnRegion(origin, generated, func(region uint16) bool {
		queries++
		return region == origin.RegionID
	})
	// Attempts 0,1,2 remain in the unavailable eastern region. Attempt 3
	// rotates around generated X=5 by the original 10-unit separation.
	if queries != 5 || got.RegionID != origin.RegionID || got.X <= 1917 || got.X >= 1918 || got.Z <= 207 || got.Z >= 208 {
		t.Fatal("native first available rotated position differs", queries, got)
	}
	queries = 0
	got = ClampGeneratedSpawnRegion(origin, generated, func(uint16) bool { queries++; return false })
	if got != origin || queries != 9 {
		t.Fatal("exhaustion did not test exactly eight alternatives before fallback", queries, got)
	}
	generated.RegionID = 0x8001
	got = ClampGeneratedSpawnRegion(origin, generated, func(uint16) bool { t.Fatal("cross-plane position reached region lookup"); return true })
	if got != origin {
		t.Fatal("spawn crossed the outdoor/dungeon boundary")
	}
}

/*
================
TestNestCreationUsesSharedGeneratedRegionClamp
================
*/
func TestNestCreationUsesSharedGeneratedRegionClamp(t *testing.T) {
	nest := lifecycleNest(100)
	nest.X, nest.Z, nest.GenerateRadius = 1915, 200, 30
	w := newLifecycleWorld(t, lifecycleRef(1), nest)
	w.s.SetRandomSource(constantWord(0))
	queries := 0
	w.s.SetSpawnRegionAvailability(func(region uint16) bool { queries++; return region == lifecycleRegion })
	live := w.expectLive(0, 1)
	if queries < 2 || live[0].Spawn.RegionID != lifecycleRegion {
		t.Fatal("monster creation bypassed the shared region clamp", queries, live[0].Spawn)
	}
}
