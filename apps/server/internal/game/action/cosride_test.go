/*
===========================================================================

cosride_test.go - ride authority, native range and detached vehicle position

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestCosRideSharesMountRangeAndPreservesDismountedVehicle
================
*/
func TestCosRideSharesMountRangeAndPreservesDismountedVehicle(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 100, Summoned: true}
	rt.BindPetSession(testDivision, c, 1)
	initial := rt.PetPresentation(testDivision, c.Name).World.Spawn
	key := simulation.WorldKey(testDivision, c.Name)
	mount := wire.NewWriter(5).U8(1).U32(gid).Payload()
	for _, pose := range []simulation.Spawn{
		{RegionID: initial.RegionID ^ 0x8000, X: initial.X, Y: initial.Y, Z: initial.Z},
		{RegionID: initial.RegionID, X: initial.X + 31, Y: initial.Y, Z: initial.Z},
		{RegionID: initial.RegionID, X: initial.X, Y: initial.Y + 31, Z: initial.Z},
	} {
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn = pose })
		if result := rt.HandleCosRide(testDivision, c, mount); len(result.Frames) != 0 || c.ActiveCOS.Mounted {
			t.Fatal("out-of-range mount accepted", pose, result)
		}
	}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) {
		w.Spawn = initial
		w.Spawn.X += cosMountRange
	})
	assertOpcodes(t, rt.HandleCosRide(testDivision, c, mount).Frames, wire.OpCosRideState)
	if !c.ActiveCOS.Mounted {
		t.Fatal("native inclusive range boundary rejected")
	}
	if result := rt.HandleCosRide(testDivision, c, mount); len(result.Frames) != 0 {
		t.Fatal("repeated mount changed state", result)
	}
	dismount := wire.NewWriter(5).U8(0).U32(gid).Payload()
	result := rt.HandleCosRide(testDivision, c, dismount)
	if c.ActiveCOS.Mounted || len(result.Frames) == 0 || !reflect.DeepEqual(result.Frames, result.Broadcast) {
		t.Fatal("dismount did not publish the committed state", result)
	}
	parked := rt.PetPresentation(testDivision, c.Name).World.Spawn
	if parked.X != initial.X+cosMountRange {
		t.Fatal("dismount reverted transport to its summon position", parked)
	}
	rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X += 200 })
	if after := rt.PetPresentation(testDivision, c.Name).World.Spawn; after != parked {
		t.Fatal("parked vehicle followed player", after, parked)
	}
	before := c.Snapshot()
	for _, bad := range [][]byte{{}, {1}, append(mount, 0), wire.NewWriter(5).U8(2).U32(gid).Payload(), wire.NewWriter(5).U8(1).U32(gid + 1).Payload()} {
		rt.HandleCosRide(testDivision, c, bad)
		if !reflect.DeepEqual(before, c.Snapshot()) {
			t.Fatal("malformed or foreign request mutated character", bad)
		}
	}
}
