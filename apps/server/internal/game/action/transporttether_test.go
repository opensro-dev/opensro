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
