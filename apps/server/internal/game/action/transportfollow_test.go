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

	"opensro.online/server/internal/domain"

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
TestDismountedTransportFollowsAThief

The rule keys on the transport (a vehicle COS, TID4 2), never on the
owner's job: a thief's dismounted transport follows the thief exactly as a
trader's follows the trader, and holds no tether.
================
*/
func TestDismountedTransportFollowsAThief(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	c.Job.Type = domain.JobThief
	rt.TransportFollow = true
	parked, thief := dismountedTransportApart(rt, c)
	advanceTransportTicks(rt, clock, 20)
	if _, held := rt.Worlds.TetherOf(simulation.WorldKey(testDivision, c.Name)); held {
		t.Fatal("a thief's following transport tethered its owner")
	}
	at := rt.cosLiveSpawn(testDivision, c, clock.NowMs())
	if before, after := simulation.WorldDistance2D(parked, thief), simulation.WorldDistance2D(at, thief); after > before/2 {
		t.Fatalf("the thief's transport stands %.0f from its owner after 10 s, was %.0f", after, before)
	}
	// Off, the thief's transport parks and tethers, as natively.
	rt2, clock2, c2 := caravanFixture(t)
	c2.Job.Type = domain.JobThief
	parked2, _ := dismountedTransportApart(rt2, c2)
	advanceTransportTicks(rt2, clock2, 10)
	if _, held := rt2.Worlds.TetherOf(simulation.WorldKey(testDivision, c2.Name)); !held {
		t.Fatal("with the rule off a thief's parked transport did not tether its owner")
	}
	if at := rt2.cosLiveSpawn(testDivision, c2, clock2.NowMs()); simulation.WorldDistance2D(at, parked2) > 0.01 {
		t.Fatalf("with the rule off the thief's transport moved to %+v", at)
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

/*
================
TestFollowingTransportKeepsTheTether

With SRO_TRANSPORT_FOLLOW_TETHER on, the following transport tethers its
trader at the transport's own walking position, 1000 units with the
native trade-transport reason: past it, a step away is refused and a
step back is allowed.
================
*/
func TestFollowingTransportKeepsTheTether(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	rt.TransportFollow, rt.TransportFollowTether = true, true
	dismountedTransportApart(rt, c)
	advanceTransportTicks(rt, clock, 4)
	tether, held := rt.Worlds.TetherOf(simulation.WorldKey(testDivision, c.Name))
	if !held || tether.Range != simulation.TradeTransportTetherRange || tether.Reason != simulation.TetherTradeTransport {
		t.Fatalf("following transport tether = %+v held %v, want the 1000-unit trade tether", tether, held)
	}
	// One tick later the anchor is where the transport walked to then.
	at := rt.cosLiveSpawn(testDivision, c, clock.NowMs())
	if simulation.WorldDistance2D(tether.Anchor, at) > 10 {
		t.Fatalf("tether anchor %+v, want the walking transport near %+v", tether.Anchor, at)
	}
	far := simulation.Spawn{RegionID: tether.Anchor.RegionID, X: tether.Anchor.X + 1001, Y: tether.Anchor.Y, Z: tether.Anchor.Z}
	farther, back := far, far
	farther.X += 5
	back.X -= 5
	if !tether.Refuses(far, farther) || tether.Refuses(far, back) {
		t.Fatal("past 1000 units a step away must be refused and a step back allowed")
	}
}

/*
================
TestFollowTetherNeedsFollowing

The tether switch alone changes nothing: with following off the transport
parks and tethers at the parked spot, as natively.
================
*/
func TestFollowTetherNeedsFollowing(t *testing.T) {
	rt, clock, c := caravanFixture(t)
	rt.TransportFollowTether = true
	parked, _ := dismountedTransportApart(rt, c)
	advanceTransportTicks(rt, clock, 4)
	tether, held := rt.Worlds.TetherOf(simulation.WorldKey(testDivision, c.Name))
	if !held || simulation.WorldDistance2D(tether.Anchor, parked) > 0.01 {
		t.Fatalf("tether = %+v held %v, want the native tether at the parked transport %+v", tether, held, parked)
	}
}

/*
================
TestTransportFollowTetherFromEnv
================
*/
func TestTransportFollowTetherFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "TRUE": true, "1": true} {
		t.Setenv(EnvTransportFollowTether, value)
		if got := TransportFollowTetherFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
