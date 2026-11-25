/*
===========================================================================

gmwarp_mounted_test.go - a GM warps while riding a vehicle (#4422)

The native GM coordinate move (the v1.188 server's coordinate branch)
moves the actor through its movement object with no transport test, and
the port's relocation already carries every companion and re-enters a
mounted transport with its ride state. A GM on a Mall vehicle therefore
warps mounted and arrives mounted.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/staff"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGMWarpCarriesTheRiddenVehicle

The warp is admitted, the rider lands at the destination, and the
re-entry re-applies the ride onto the same vehicle.
================
*/
func TestGMWarpCarriesTheRiddenVehicle(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100)
	equipCombatTestPet(t, rt, c, 1)
	c.ActiveCOS.Mounted = true
	c.GMPrivilege, c.GMCapabilities = true, uint32(staff.All)
	deps := rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(_ *enterworld.Character, r uint16) bool { return r == 25416 }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	var sent []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, f []wire.Frame) { sent = append(sent, f...) }
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}

	out := gmcommand.HandleGmCommand(deps, nil, testDivision, c, warpFrame(), rt)
	if !bytes.Equal(out.Ack, []byte{1, 16}) {
		t.Fatalf("mounted warp refused: %+v", out)
	}
	got := missionSpawnFromWorld(c.World.Spawn, simulation.Spawn{})
	if got.RegionID != 25416 || got.X != 703 || got.Z != 1575 {
		t.Fatalf("rider landed at %+v", got)
	}
	if c.ActiveCOS == nil || !c.ActiveCOS.Summoned || !c.ActiveCOS.Mounted {
		t.Fatalf("vehicle left behind: %+v", c.ActiveCOS)
	}
	want := wire.EncodeCosRideState(enterworld.ObjectIDForCharacter(c), true, c.ActiveCOS.GID)
	for _, f := range sent {
		if f.Opcode == wire.OpCosRideState && bytes.Equal(f.Payload, want) {
			return
		}
	}
	t.Fatal("re-entry did not re-apply the ride")
}
