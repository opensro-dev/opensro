/*
===========================================================================

timedreturn_test.go - the timed-event return's revival grace (#1582)

A player a timed world sends home stands untouchable for the revival
grace of the world left - six seconds, eleven out of a siege world -
published to the player and peers, while the GM move grants nothing.
Monster targeting refuses body mode 2 through AllowsTargetStatus
(monsterareahostility.go), so the mode is the no-pick guarantee.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
)

/*
================
timedReturnBodyFrames

Counts 0x30BF body-mode refreshes on one push lane.
================
*/
func timedReturnBodyFrames(frames []wire.Frame) int {
	total := 0
	for _, frame := range frames {
		if frame.Opcode == wire.OpObjectStateRefresh {
			total++
		}
	}
	return total
}

/*
================
TestTimedEventReturnIsUntouchableForSixSeconds
================
*/
func TestTimedEventReturnIsUntouchableForSixSeconds(t *testing.T) {
	c := instanceWorldTestCharacter(11, "Returnee")
	rt, clock := newInstanceWorldRuntime(t, c)
	admitInstanceWorldCharacter(t, rt, c, 1, instanceWorldTestStart)
	pushed := map[string][]wire.Frame{}
	peers := map[string][]wire.Frame{}
	rt.PushCharacterFrames = func(_, name string, f []wire.Frame) { pushed[name] = append(pushed[name], f...) }
	rt.PushDivisionPeerFrames = func(_, name string, f []wire.Frame) { peers[name] = append(peers[name], f...) }

	lease := openInstanceWorldOrFail(t, rt, instanceWorldTestPartyDefinition, 0)
	if status := rt.TransferCharacterToInstance(testDivision, c, lease, instanceWorldTestEntry); status != instance.Success {
		t.Fatalf("transfer: status %d", status)
	}
	if c.NativeBodyStatus != 0 {
		t.Fatalf("the transfer in granted body mode %d (instance transfers grant nothing)", c.NativeBodyStatus)
	}
	pushed, peers = map[string][]wire.Frame{}, map[string][]wire.Frame{}

	if status := rt.ReturnCharacterFromInstance(testDivision, c); status != instance.Success {
		t.Fatalf("return: status %d", status)
	}
	if c.NativeBodyStatus != untouchableBodyStatus {
		t.Fatalf("returned body status = %d, want untouchable", c.NativeBodyStatus)
	}
	if timedReturnBodyFrames(pushed[c.Name]) == 0 || timedReturnBodyFrames(peers[c.Name]) == 0 {
		t.Fatal("the return's untouchable was not published to the player and peers")
	}
	clock.Advance(reviveUntouchableMs*time.Millisecond - time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if c.NativeBodyStatus != untouchableBodyStatus {
		t.Fatal("the return's untouchable ended early")
	}
	clock.Advance(time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if c.NativeBodyStatus != 0 {
		t.Fatalf("body status after the return grace = %d, want restored", c.NativeBodyStatus)
	}
}

/*
================
TestFortressExpulsionIsUntouchableForElevenSeconds
================
*/
func TestFortressExpulsionIsUntouchableForElevenSeconds(t *testing.T) {
	rt, c, clock := fortressFixtureWithClock(t, testFieldFortGate)
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	enterFortress(t, rt, c)

	now := clock.NowMs()
	rt.FortressWarChanged(testDivision, now, true)
	rt.advanceFortressPhases(now + fortressBeginDelayMs)
	inField(t, rt, c, "the war's beginning")
	if c.NativeBodyStatus != untouchableBodyStatus {
		t.Fatalf("expelled body status = %d, want untouchable", c.NativeBodyStatus)
	}
	clock.Advance(siegeReviveUntouchableMs*time.Millisecond - time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if c.NativeBodyStatus != untouchableBodyStatus {
		t.Fatal("the siege return's untouchable ended before eleven seconds")
	}
	clock.Advance(time.Millisecond)
	rt.TickHook()(clock.NowMs())
	if c.NativeBodyStatus != 0 {
		t.Fatalf("body status after the siege return grace = %d, want restored", c.NativeBodyStatus)
	}
}

/*
================
TestGMWarpGrantsNoUntouchable

The GM move calls the town-teleport routine with the granting flag
clear: even out of a siege world, no grace follows it.
================
*/
func TestGMWarpGrantsNoUntouchable(t *testing.T) {
	rt, c := fortressPortalFixture(t, testFieldFortGate)
	enterFortress(t, rt, c)
	deps := rt.deps.(*enterworld.Deps)
	deps.CanEnterWorldRegion = func(_ *enterworld.Character, r uint16) bool { return r == 25416 }
	deps.SpawnTerrainHeight = func(uint16, float64, float64) (float64, bool) { return 20, true }
	rt.PushCharacterFrames = func(_, _ string, _ []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	c.GMPrivilege = true
	if !rt.WarpGM(testDivision, c.Name, wire.Position{RegionID: 25416, X: 703, Y: 42, Z: 1575}) {
		t.Fatal("warp refused")
	}
	inField(t, rt, c, "the GM warp")
	if c.NativeBodyStatus != 0 {
		t.Fatalf("the GM warp granted body mode %d", c.NativeBodyStatus)
	}
}
