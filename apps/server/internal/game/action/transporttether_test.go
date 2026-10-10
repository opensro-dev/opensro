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
