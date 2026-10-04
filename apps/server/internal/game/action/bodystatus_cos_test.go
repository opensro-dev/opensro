/*
===========================================================================

bodystatus_cos_test.go - inherited GM body state across pet and ride lifetime

The transport retains its own location until the owner mounts within native
range; session replacement preserves the same status and actor identity.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/gmcommand"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGMStatusSummonMountAndSessionLifecycle
================
*/
func TestGMStatusSummonMountAndSessionLifecycle(t *testing.T) {
	for _, command := range []byte{gmcommand.SubInvisible, gmcommand.SubInvincible} {
		c := testCharacter()
		c.GMPrivilege = true
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: 22, RefObjID: 3905, Codename: "ITEM_COS_T_DHORSE3",
			TypeFlags: wire.PackTypeFlags(3, 3, 3, 2), StackCount: 1,
		})
		rt, clock := newTestRuntime(c, testCosSource(testItems()))
		rt.BindPetSession(testDivision, c, 101)
		out := gmcommand.HandleGmCommand(rt.deps.(*enterworld.Deps), nil, testDivision, c, []byte{command}, rt)
		if len(out.Ack) != 2 || out.Ack[0] != 1 {
			t.Fatal("GM request refused", out)
		}
		want := c.NativeBodyStatus
		result := rt.HandleItemUse(testDivision, c, []byte{22, 0xEC, 0x11})
		assertOpcodes(t, result.Frames, wire.OpItemUseResponse, wire.OpItemUseVisual, wire.OpCosRecordCreate, wire.OpSingleObjectSpawn, wire.OpCosRideState, movementSpeedOpcode)
		// Shared spawn grammar: 24 position bytes + 5 movement bytes + life,
		// motion, body. Both owner and peer packets must initialize the body.
		if c.ActiveCOS.NativeBodyStatus != want || result.Frames[3].Payload[31] != want || result.Broadcast[0].Payload[31] != want {
			t.Fatal("summon lost inherited status", c.ActiveCOS, result)
		}
		if !c.ActiveCOS.Mounted {
			t.Fatal("native summon did not automatically bind the vehicle")
		}
		rt.HandleCosRide(testDivision, c, wire.NewWriter(5).U8(0).U32(c.ActiveCOS.GID).Payload())
		first := rt.PetPresentation(testDivision, c.Name)
		if first == nil || first.NativeBodyStatus != want || first.Mounted {
			t.Fatal("missing transport presentation", first)
		}
		key := simulation.WorldKey(testDivision, c.Name)
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X += 200 })
		if next := rt.PetPresentation(testDivision, c.Name); next.World.Spawn != first.World.Spawn {
			t.Fatal("unmounted transport followed owner")
		}
		mount := rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(c.ActiveCOS.GID).U8(wire.CosCommandMountTag).Payload())
		if len(mount.Frames) != 1 || mount.Frames[0].Opcode != wire.OpCosRideState || !bytes.Equal(mount.Frames[0].Payload, []byte{2, 4}) || len(mount.Broadcast) != 0 || c.ActiveCOS.Mounted {
			t.Fatal("remote mount bypassed native distance gate", mount)
		}
		rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X = first.World.Spawn.X })
		mount = rt.HandleCosCommand(testDivision, c, wire.NewWriter(5).U32(c.ActiveCOS.GID).U8(wire.CosCommandMountTag).Payload())
		assertOpcodes(t, mount.Frames, wire.OpObjectSourceCorrection, wire.OpCosRideState, movementSpeedOpcode)
		mounted := rt.PetPresentation(testDivision, c.Name)
		if mounted == nil || !mounted.Mounted || mounted.NativeBodyStatus != want || mounted.World.LiveSpawnAt(clock.NowMs()).X != first.World.Spawn.X {
			t.Fatal("mounted presentation did not use rider authority", mounted)
		}
		// Logical-session replacement retains this actor; a delayed teardown
		// of the displaced transport must not destroy its status or COS.
		rt.BindPetSession(testDivision, c, 102)
		rt.ForgetCharacterSession(testDivision, c.Name, 101)
		if c.NativeBodyStatus != want || c.ActiveCOS.NativeBodyStatus != want || rt.PetPresentation(testDivision, c.Name) == nil {
			t.Fatal("stale close destroyed replacement actor")
		}
		rt.BindPetSession(testDivision, c, 102)
		if c.NativeBodyStatus != want {
			t.Fatal("resume cleared status")
		}
		rt.ForgetCharacterSession(testDivision, c.Name, 102)
		if c.NativeBodyStatus != 0 || c.ActiveCOS.NativeBodyStatus != 0 || rt.PetPresentation(testDivision, c.Name) != nil {
			t.Fatal("final teardown retained runtime state")
		}
		if first.NativeBodyStatus != want {
			t.Fatal("old detached COS snapshot changed")
		}
	}
}
