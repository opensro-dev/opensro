/*
===========================================================================

transportfollow_test.go - tests for transportfollow.go

Both settings of SRO_TRANSPORT_FOLLOW: off, the dismounted trade transport
parks and tethers its trader (native); on, it walks after the trader with
no tether, and the job monsters still find it where it walks.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
dismountedTransportApart

A caravanFixture trader steps off the transport at parked and then stands
walked well away from it; returns the trader's new pose.
================
*/
func dismountedTransportApart(rt *Runtime, c *enterworld.Character) (parked, trader simulation.Spawn) {
	parked = simulation.Spawn{RegionID: caravanBattlefield, X: 960, Y: 20, Z: 960}
	trader = simulation.Spawn{RegionID: caravanBattlefield, X: 1260, Y: 20, Z: 960}
	// An open field: every step lands where it aims.
	rt.ConstrainMovement = func(_ string, _, to simulation.Spawn) (simulation.Spawn, *simulation.MoveError) { return to, nil }
	c.ActiveCOS.Mounted = false
	rt.rememberTransportCOS(testDivision, c, parked)
	rideCaravanTo(rt, c, trader)
	return parked, trader
}

/*
================
advanceTransportTicks
================
*/
func advanceTransportTicks(rt *Runtime, clock *fakeClock, ticks int) {
	for i := 0; i < ticks; i++ {
		clock.Advance(500 * time.Millisecond)
		rt.advancePets(clock.NowMs())
	}
}

/*
================
TestTransportFollowFromEnv
================
*/
func TestTransportFollowFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "TRUE": true, "1": true} {
		t.Setenv(EnvTransportFollow, value)
		if got := TransportFollowFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}

/*
================
TestDismountedTransportParksWhenFollowIsOff

Native: the transport stays where its trader stepped off and holds the
trader by the tether.
================
*/
func TestDismountedTransportParksWhenFollowIsOff(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	parked, _ := dismountedTransportApart(rt, c)
	advanceTransportTicks(rt, clock, 10)
	if _, held := rt.Worlds.TetherOf(simulation.WorldKey(testDivision, c.Name)); !held {
		t.Fatal("the parked transport did not tether its trader")
	}
	if at := rt.cosLiveSpawn(testDivision, c, clock.NowMs()); simulation.WorldDistance2D(at, parked) > 0.01 {
		t.Fatalf("the parked transport moved to %+v", at)
	}
}

/*
================
TestDismountedTransportFollowsItsTrader

With the flag on the transport sets out from where the trader left it,
closes on the trader, and never tethers it.
================
*/
func TestDismountedTransportFollowsItsTrader(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	rt.TransportFollow = true
	parked, trader := dismountedTransportApart(rt, c)
	rt.advancePets(clock.NowMs())
	if at := rt.cosLiveSpawn(testDivision, c, clock.NowMs()); simulation.WorldDistance2D(at, parked) > 0.01 {
		t.Fatalf("the transport set out from %+v, not where it was left (%+v)", at, parked)
	}
	advanceTransportTicks(rt, clock, 20)
	if _, held := rt.Worlds.TetherOf(simulation.WorldKey(testDivision, c.Name)); held {
		t.Fatal("a following transport tethered its trader")
	}
	at := rt.cosLiveSpawn(testDivision, c, clock.NowMs())
	if before, after := simulation.WorldDistance2D(parked, trader), simulation.WorldDistance2D(at, trader); after > before/2 {
		t.Fatalf("the transport stands %.0f from its trader after 10 s, was %.0f", after, before)
	}
}

/*
================
TestJobMonstersTargetAFollowingTransport

A thief or hunter monster gathers its companion targets from the
presentation: the following transport is offered where it walks, not where
it was parked, so thieves still attack it.
================
*/
func TestJobMonstersTargetAFollowingTransport(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	rt.TransportFollow = true
	parked, _ := dismountedTransportApart(rt, c)
	advanceTransportTicks(rt, clock, 20)
	at := rt.cosLiveSpawn(testDivision, c, clock.NowMs())
	if simulation.WorldDistance2D(at, parked) < 50 {
		t.Fatalf("the transport did not leave its parking place: %+v", at)
	}
	for _, target := range rt.CompanionTargets(testDivision, enterworld.ObjectIDForCharacter(c), clock.NowMs()) {
		if target.Gid != c.ActiveCOS.GID {
			continue
		}
		if simulation.WorldDistance2D(target.Pose, at) > 1 {
			t.Fatalf("the transport is offered at %+v, walks at %+v", target.Pose, at)
		}
		return
	}
	t.Fatal("the following transport is not a companion target")
}
