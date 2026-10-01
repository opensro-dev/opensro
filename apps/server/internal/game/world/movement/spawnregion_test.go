/*
===========================================================================

spawnregion_test.go - creation uses native collision results and map residency

===========================================================================
*/
package movement

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCompanionSpawnKeepsClippedEndpointAndFallsBackWithoutCoverage
================
*/
func TestCompanionSpawnKeepsClippedEndpointAndFallsBackWithoutCoverage(t *testing.T) {
	v := NewWaterValidator(syntheticHeightRoot(t))
	origin, candidate := spawnAt(0x6B4F, 30, 110), spawnAt(0x6B4F, 110, 110)
	report := v.ClipMovementPath(origin, candidate)
	if report.Outcome != ClipBlocked || report.NativeResult != monster.NavResultClipped {
		t.Fatal("fixture did not reach a native clipped contact", report)
	}
	got := v.ConstrainCompanionSpawn(origin, candidate)
	if got.X != report.Rest.X || got.Z != report.Rest.Z || got == origin {
		t.Fatal("creation replaced a clipped endpoint with the player-request refusal", got, report.Rest)
	}
	if !v.SpawnRegionAvailable(origin.RegionID) || v.SpawnRegionAvailable(0) {
		t.Fatal("region availability diverged from the authoritative catalog")
	}
	candidate.RegionID = 0
	if got = v.ConstrainCompanionSpawn(origin, candidate); got != origin {
		t.Fatal("missing coverage admitted an unverified spawn", got)
	}
}

/*
================
TestCompanionSpawnDistinguishesBlockedDungeonContactFromClipping
================
*/
func TestCompanionSpawnDistinguishesBlockedDungeonContactFromClipping(t *testing.T) {
	blocks := []dungeonSpawnBlock{
		{ordinal: 0, meshes: []*objectNavMesh{topologyMesh(false)}},
		{ordinal: 1, x: 100, meshes: []*objectNavMesh{topologyMesh(true)}},
	}
	surface := &dungeonSpawnSurface{blocks: blocks, objects: resolveDungeonLinks(blocks)}
	v := &WaterValidator{dungeonSpawnSurfaces: map[uint16]*dungeonSpawnSurface{0x8001: surface}}
	v.dungeonSpawnOnce.Do(func() {})
	origin := simulation.Spawn{RegionID: 0x8001, X: 50, Y: 10, Z: 50}
	candidate := origin
	candidate.X = 150
	if !v.SpawnRegionAvailable(origin.RegionID) || v.SpawnRegionAvailable(0x8002) {
		t.Fatal("dungeon residency did not use the loaded navigation host")
	}
	if got := v.ConstrainCompanionSpawn(origin, candidate); got != origin {
		t.Fatal("blocked COS creation failed to fall back to the owner", got)
	}
	blocks[0].connected, blocks[1].connected = []int{1}, []int{0}
	surface.objects = resolveDungeonLinks(blocks)
	if got := v.ConstrainCompanionSpawn(origin, candidate); got != candidate {
		t.Fatal("linked dungeon path was refused", got)
	}
}
