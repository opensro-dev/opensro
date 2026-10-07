/*
===========================================================================

worldstore_seed_test.go - first-touch seeds the live store may keep

===========================================================================
*/
package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestWorldStoreDoesNotKeepAnUnboundSeed

A seed made without a character record answers once; the next touch with
the record seeds the saved stand instead of the pinned race start.
================
*/
func TestWorldStoreDoesNotKeepAnUnboundSeed(t *testing.T) {
	st := NewWorldStore()
	key := WorldKey("division", "Jalk")
	if got := st.Snapshot(key, func() WorldState { return SeedWorldState(nil) }); got.Spawn.RegionID != ChinaStartProfile().RegionID {
		t.Fatalf("nil seed: %+v", got.Spawn)
	}
	regionID, x, y, z := int64(0x679A), 640.0, -109.9, 61.5
	c := &domain.Character{Name: "Jalk", World: &domain.CharacterWorld{Spawn: &domain.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z}}}
	if got := st.Snapshot(key, func() WorldState { return SeedWorldState(c) }); got.Spawn.RegionID != 0x679A || got.Spawn.X != 640 {
		t.Fatalf("race start stayed pinned: %+v", got.Spawn)
	}
}

/*
================
TestSeedWorldStateTakesSpawnWhole

A spawn missing its region, or holding a NaN, seeds the whole race start.
================
*/
func TestSeedWorldStateTakesSpawnWhole(t *testing.T) {
	x, y, z := 973.9, -42.0, 182.7
	c := &domain.Character{Name: "Jalk", World: &domain.CharacterWorld{Spawn: &domain.WorldSpawn{X: &x, Y: &y, Z: &z}}}
	start := ChinaStartProfile()
	if got := SeedWorldState(c).Spawn; got != start {
		t.Fatalf("regionless spawn mixed with the start: %+v", got)
	}
	regionID, nan := int64(0x679A), math.NaN()
	c.World.Spawn = &domain.WorldSpawn{RegionID: &regionID, X: &nan, Y: &y, Z: &z}
	if got := SeedWorldState(c).Spawn; got != start {
		t.Fatalf("NaN spawn seeded: %+v", got)
	}
}
