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
