/*
===========================================================================

entryplacement_test.go - committing the stand enter-world published

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestAdoptEntrySpawnCommitsLiveWorldAndRecord

The live world and the record both move to the published stand, and the
seeded pre-entry position does not survive in either.
================
*/
func TestAdoptEntrySpawnCommitsLiveWorldAndRecord(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	key := simulation.WorldKey(testDivision, c.Name)
	rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(c) })
	stand := simulation.Spawn{RegionID: 0x679B, X: 905, Y: -42.9, Z: 81}
	rt.AdoptEntrySpawn(testDivision, c.Name, stand)
	live := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(c) })
	if live.Spawn.RegionID != stand.RegionID || live.Spawn.X != stand.X || live.MoveSegment != nil {
		t.Fatalf("live world not moved: %+v", live.Spawn)
	}
	saved := c.World.Spawn
	if saved == nil || *saved.RegionID != int64(stand.RegionID) || *saved.X != stand.X || *saved.Z != stand.Z || !c.World.SpawnSet {
		t.Fatalf("record not moved: %+v", c.World)
	}
	rt.AdoptEntrySpawn(testDivision, "nobody", stand)
}
