/*
===========================================================================

transporttether_test.go - a parked trade transport holds its trader

The pet tick publishes the parked transport's position as its trader's
tether (simulation/tether.go); riding, sending it home or its death lets
the trader go on the next tick.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestParkedTransportTethersItsTrader
================
*/
func TestParkedTransportTethersItsTrader(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	key := simulation.WorldKey(testDivision, c.Name)
	rt.advancePets(clock.Now().UnixMilli())
	if _, held := rt.Worlds.TetherOf(key); held {
		t.Fatal("a ridden transport tethered its rider")
	}

	parked := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	c.ActiveCOS.Mounted = false
	rt.rememberTransportCOS(testDivision, c, parked)
	rt.advancePets(clock.Now().UnixMilli())
	tether, held := rt.Worlds.TetherOf(key)
	if !held || tether.Anchor.RegionID != parked.RegionID || tether.Anchor.X != parked.X || tether.Anchor.Z != parked.Z ||
		tether.Range != simulation.TradeTransportTetherRange || tether.Reason != simulation.TetherTradeTransport {
		t.Fatalf("parked transport tether = %+v held %v, want 1000 around %+v", tether, held, parked)
	}

	c.ActiveCOS.CurrentHP = 0
	rt.advancePets(clock.Now().UnixMilli())
	if _, held := rt.Worlds.TetherOf(key); held {
		t.Fatal("a dead transport kept its trader tethered")
	}
}

/*
================
TestRelocatedTraderLeavesTheOldAnchor

A return scroll, portal, rebirth or GM move carries the parked transport
with its trader (relocateReturningPet): the old anchor stops holding at
once, and the next tick tethers the trader to the transport where it
arrived, so the trader is never stuck past the range.
================
*/
func TestRelocatedTraderLeavesTheOldAnchor(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	key := simulation.WorldKey(testDivision, c.Name)
	parked := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	c.ActiveCOS.Mounted = false
	rt.rememberTransportCOS(testDivision, c, parked)
	rt.advancePets(clock.Now().UnixMilli())
	if _, held := rt.Worlds.TetherOf(key); !held {
		t.Fatal("the parked transport did not tether its trader")
	}

	town := simulation.Spawn{RegionID: 25000, X: 1000, Y: 0, Z: 500}
	rt.relocateReturningPet(testDivision, c, town)
	if _, held := rt.Worlds.TetherOf(key); held {
		t.Fatal("the old anchor still holds a relocated trader")
	}
	rt.advancePets(clock.Now().UnixMilli())
	tether, held := rt.Worlds.TetherOf(key)
	if !held || tether.Anchor.RegionID != town.RegionID || simulation.WorldDistance2D(tether.Anchor, town) > 1 {
		t.Fatalf("after relocation the tether is %+v held %v, want the transport at the destination %+v", tether, held, town)
	}
}

/*
================
TestTravelledTransportArrivesAtTheDestination

BR-261121-2209-96DB (issue #4474): with the job-goods rule a gate
carries the trader's summoned transport with them (the travelled-pet
halves are held by petteleportfollow_test.go and petarrival_test.go;
this holds the transport's). After the carry the transport stands at
the destination, the owner is shown its arrival with sub-state 1,
and the peer lane presents the same arrival - the transport is with
its trader in Hotan, not stuck where it was parked.
================
*/
func TestTravelledTransportArrivesAtTheDestination(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	parked := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	c.ActiveCOS.Mounted = false
	rt.rememberTransportCOS(testDivision, c, parked)
	rt.advancePets(clock.Now().UnixMilli())

	// Settled where it was parked: no arrival is pending.
	clock.Advance(2 * time.Second)
	if frames := rt.petArrivalFrames(testDivision, c); len(frames) != 0 {
		t.Fatal("a parked transport that never travelled produced arrival frames")
	}

	// The gate carries the trader to Hotan (the report's region).
	hotan := simulation.Spawn{RegionID: 0x5c87, X: 1169, Y: 244, Z: 223}
	rt.relocateReturningPet(testDivision, c, hotan)

	pose := rt.companionLiveSpawn(testDivision, c, c.ActiveCOS, clock.Now().UnixMilli())
	if pose.RegionID != hotan.RegionID || simulation.WorldDistance2D(pose, hotan) > 1 {
		t.Fatalf("the travelled transport stands at %+v, want beside its trader at %+v", pose, hotan)
	}

	states := arrivalSpawnStates(t, rt.petArrivalFrames(testDivision, c))
	if states[c.ActiveCOS.GID] != 1 {
		t.Fatalf("transport arrival sub-state = %d, want 1 (states %v)", states[c.ActiveCOS.GID], states)
	}

	views := rt.CompanionPresentations(testDivision, c.Name)
	found := false
	for _, view := range views {
		spawn := view.SpawnFrames(clock.NowMs())
		wireSpawn := make([]wire.Frame, len(spawn))
		for i, frame := range spawn {
			wireSpawn[i] = wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload}
		}
		peerStates := arrivalSpawnStates(t, wireSpawn)
		if _, mine := peerStates[c.ActiveCOS.GID]; !mine {
			continue
		}
		found = true
		if !view.Fresh || peerStates[c.ActiveCOS.GID] != 1 {
			t.Fatalf("peer presentation of the travelled transport: fresh %v, sub-state %d, want an arrival with 1",
				view.Fresh, peerStates[c.ActiveCOS.GID])
		}
	}
	if !found {
		t.Fatalf("no peer presentation for the travelled transport among %d views", len(views))
	}
}

/*
================
TestRiddenTransportGetsNoOwnerArrival

A trader riding the transport through the gate arrives on it: the
re-entry places the rider on the mount, so the owner is sent no
arrival spawn that would replace the mount under them.
================
*/
func TestRiddenTransportGetsNoOwnerArrival(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	if !c.ActiveCOS.Mounted {
		t.Fatal("the fixture's trader should ride the transport")
	}
	rt.advancePets(clock.Now().UnixMilli())
	clock.Advance(2 * time.Second)
	hotan := simulation.Spawn{RegionID: 0x5c87, X: 1169, Y: 244, Z: 223}
	rt.relocateReturningPet(testDivision, c, hotan)
	if states := arrivalSpawnStates(t, rt.petArrivalFrames(testDivision, c)); len(states) != 0 {
		t.Fatalf("the ridden transport was sent an owner arrival: %v", states)
	}
}

/*
================
TestTownReviveLeavesTheParkedTransport

A trader who dies beside a parked transport and revives at the town's
appointed point comes back alone (relocateRevivingPets): the transport
keeps its pose in the field, and the next tick tethers the revived
trader to it there, not in town. The transport goes empty: the
fixture's goods have no item reference for the re-entry to encode, and
the relocation never touches the cargo.
================
*/
func TestTownReviveLeavesTheParkedTransport(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	key := simulation.WorldKey(testDivision, c.Name)
	parked := simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	c.ActiveCOS.Mounted = false
	c.ActiveCOS.Container.Rows = nil
	rt.rememberTransportCOS(testDivision, c, parked)
	rt.advancePets(clock.Now().UnixMilli())
	c.World.RebirthPoint = worldSpawnFromMission(simulation.ChinaStartProfile())

	zero := int64(0)
	c.CurrentHP = &zero
	rt.settlePlayerDeathInDoor(testDivision, c, deathKiller{}, clock.Now().UnixMilli())
	if out := rt.HandleLocalRebirth(testDivision, c, []byte{wire.RebirthAtSpecifiedPoint}); len(out.Frames) == 0 {
		t.Fatalf("the town revive did not run: %+v", out)
	}
	if c.ActiveCOS == nil || !c.ActiveCOS.Summoned {
		t.Fatalf("the transport left the world with its trader's revive: %+v", c.ActiveCOS)
	}
	pose := rt.companionLiveSpawn(testDivision, c, c.ActiveCOS, clock.Now().UnixMilli())
	if pose.RegionID != parked.RegionID || simulation.WorldDistance2D(pose, parked) > 1 {
		t.Fatalf("the parked transport moved to %+v on its trader's town revive, want it at %+v", pose, parked)
	}

	rt.advancePets(clock.Now().UnixMilli())
	tether, held := rt.Worlds.TetherOf(key)
	if !held || tether.Anchor.RegionID != parked.RegionID || simulation.WorldDistance2D(tether.Anchor, parked) > 1 {
		t.Fatalf("after the revive the tether is %+v held %v, want the transport where it stood %+v", tether, held, parked)
	}
}
