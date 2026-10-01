/*
===========================================================================

cosride_test.go - tests for cosride.go

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
summonRidingHorse

A character standing beside a freshly summoned level-10 riding horse.
================
*/
func summonRidingHorse(t *testing.T) (*Runtime, *fakeClock, *enterworld.Character, cosTestItemSource) {
	t.Helper()
	c := testCharacter()
	level := int64(10)
	c.Level = &level
	items := testCosSource(testItems())
	ref := items.staticItemSource["ITEM_COS_C_HORSE1"]
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 23, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 1,
	})
	rt, clock := newTestRuntime(c, items)
	rt.BindPetSession(testDivision, c, 101)
	result := rt.HandleItemUse(testDivision, c, []byte{23, 0xEC, 0x11})
	assertOpcodes(t, result.Frames, wire.OpItemUseResponse, wire.OpCosRecordCreate, wire.OpSingleObjectSpawn)
	if c.ActiveCOS == nil || !c.ActiveCOS.Summoned || c.ActiveCOS.Codename != "COS_C_HORSE1" {
		t.Fatalf("riding horse was not summoned: %+v", result)
	}
	return rt, clock, c, items
}

/*
================
rideToggle
================
*/
func rideToggle(state uint8, gid uint32) []byte {
	return wire.NewWriter(5).U8(state).U32(gid).Payload()
}

/*
================
TestRidingHorseSummonsRidesAndGetsOff

A riding horse (band 1) summons with the band-1 record and spawn forms,
rides through the COS window's 0x74B5, and gets off again; both changes
are broadcast as the ride state.
================
*/
func TestRidingHorseSummonsRidesAndGetsOff(t *testing.T) {
	rt, _, c, _ := summonRidingHorse(t)
	gid := c.ActiveCOS.GID
	rider := enterworld.ObjectIDForCharacter(c)

	ride := rt.HandleCosRideToggle(testDivision, c, rideToggle(1, gid))
	assertOpcodes(t, ride.Frames, wire.OpCosRideState)
	if !bytes.Equal(ride.Frames[0].Payload, wire.EncodeCosRideState(rider, true, gid)) || len(ride.Broadcast) != 1 {
		t.Fatalf("ride = %+v", ride)
	}
	if !c.ActiveCOS.Mounted {
		t.Fatal("riding did not mount the horse")
	}
	if presented := rt.PetPresentation(testDivision, c.Name); presented == nil || !presented.Mounted {
		t.Fatal("the ridden horse is not presented as mounted")
	}
	// A ridden horse moves with the vehicle command family.
	if moved := rt.HandleCosCommand(testDivision, c, wire.NewWriter(7).U32(gid).U8(wire.CosCommandStopTag).U16(0).Payload()); moved.DiagnosticRefusal != "" {
		t.Fatalf("the vehicle command refused a riding horse: %+v", moved)
	}

	off := rt.HandleCosRideToggle(testDivision, c, rideToggle(0, gid))
	if len(off.Frames) == 0 || !bytes.Equal(off.Frames[0].Payload, wire.EncodeCosRideState(rider, false, gid)) {
		t.Fatalf("get off = %+v", off)
	}
	if c.ActiveCOS.Mounted {
		t.Fatal("getting off left the horse mounted")
	}
}

/*
================
TestRidingHorseRefusalsAnswerTheRider

Every refusal reaches the rider alone as [2][code] and changes nothing.
================
*/
func TestRidingHorseRefusalsAnswerTheRider(t *testing.T) {
	for _, row := range []struct {
		name  string
		code  uint8
		setup func(rt *Runtime, clock *fakeClock, c *enterworld.Character, items cosTestItemSource) []byte
	}{
		{"unknown state", wire.CosRideRefusedRequest, func(rt *Runtime, _ *fakeClock, c *enterworld.Character, _ cosTestItemSource) []byte {
			return rideToggle(7, c.ActiveCOS.GID)
		}},
		{"no vehicle", wire.CosRideRefusedNoTarget, func(*Runtime, *fakeClock, *enterworld.Character, cosTestItemSource) []byte {
			return rideToggle(1, 0)
		}},
		{"not my COS", wire.CosRideNotMyCOS, func(_ *Runtime, _ *fakeClock, c *enterworld.Character, _ cosTestItemSource) []byte {
			return rideToggle(1, c.ActiveCOS.GID+1)
		}},
		{"cannot ride a pet", wire.CosRideCannotRide, func(_ *Runtime, _ *fakeClock, c *enterworld.Character, items cosTestItemSource) []byte {
			items.characters["COS_C_HORSE1"].TidWord = 3<<11 | 0x1c6
			return rideToggle(1, c.ActiveCOS.GID)
		}},
		{"too far", wire.CosRideTooFar, func(rt *Runtime, _ *fakeClock, c *enterworld.Character, _ cosTestItemSource) []byte {
			key := simulation.WorldKey(testDivision, c.Name)
			rt.Worlds.Update(key, func() simulation.WorldState { return simulation.SeedWorldState(c) }, func(w *simulation.WorldState) { w.Spawn.X += cosRideReach + 1 })
			return rideToggle(1, c.ActiveCOS.GID)
		}},
		{"in battle", wire.CosRideInBattle, func(_ *Runtime, clock *fakeClock, c *enterworld.Character, _ cosTestItemSource) []byte {
			c.BattleUntilMs = clock.NowMs() + battleStateMs
			return rideToggle(1, c.ActiveCOS.GID)
		}},
		{"already riding", wire.CosRideRefusedState, func(_ *Runtime, _ *fakeClock, c *enterworld.Character, _ cosTestItemSource) []byte {
			c.ActiveCOS.Mounted = true
			return rideToggle(1, c.ActiveCOS.GID)
		}},
		{"not riding", wire.CosRideRefusedState, func(_ *Runtime, _ *fakeClock, c *enterworld.Character, _ cosTestItemSource) []byte {
			return rideToggle(0, c.ActiveCOS.GID)
		}},
	} {
		t.Run(row.name, func(t *testing.T) {
			rt, clock, c, items := summonRidingHorse(t)
			mounted := false
			request := row.setup(rt, clock, c, items)
			mounted = c.ActiveCOS.Mounted
			result := rt.HandleCosRideToggle(testDivision, c, request)
			want := wire.EncodeCosRideRefusal(row.code)
			if len(result.Frames) != 1 || result.Frames[0].Opcode != wire.OpCosRideState || !bytes.Equal(result.Frames[0].Payload, want) {
				t.Fatalf("refusal = %+v, want [% x]", result, want)
			}
			if len(result.Broadcast) != 0 || len(result.ActorPrivate) != 1 {
				t.Fatalf("a refusal left the rider: %+v", result)
			}
			if c.ActiveCOS.Mounted != mounted {
				t.Fatal("a refusal changed the ride state")
			}
		})
	}
}
