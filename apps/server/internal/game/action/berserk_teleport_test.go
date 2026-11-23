/*
===========================================================================

berserk_teleport_test.go - a teleport ends berserk on every travel path

#4145: the report's berserk outlived the teleport that should have
ended it. The retirement rides the same loading cleanups as the
transform and the party auras at each travel owner's commit.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
activateBerserk

Five points in, one press: the character is berserk with its deadline
and tick index live.
================
*/
func activateBerserk(t *testing.T, rt *Runtime, c *enterworld.Character) {
	t.Helper()
	c.BerserkPoints = 5
	rt.HandleBerserk(testDivision, c, []byte{1})
	if c.NativeBodyStatus != 1 || c.BerserkUntilMs == 0 {
		t.Fatalf("activation did not take: status %d until %d", c.NativeBodyStatus, c.BerserkUntilMs)
	}
	if _, ok := rt.berserkActors.Load(simulation.WorldKey(testDivision, c.Name)); !ok {
		t.Fatal("activation left no tick index entry")
	}
}

/*
================
TestBerserkEndsOnTeleport

Every travel owner - gate, relocation, return scroll, reverse return -
ends an active berserk at its commit: deadline cleared, tick index
dropped, body mode retired, and the change published to the character
and its peers. The old deadline never fires afterwards, and a later
activation works from the clean state.
================
*/
func TestBerserkEndsOnTeleport(t *testing.T) {
	for _, path := range []string{"gate", "relocation", "return", "reverse"} {
		t.Run(path, func(t *testing.T) {
			rt, c, clock := reverseReturnFixture(t)
			var self, peers []wire.Frame
			rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) { self = append(self, frames...) }
			rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) { peers = append(peers, frames...) }
			activateBerserk(t, rt, c)
			gid := enterworld.ObjectIDForCharacter(c)
			self, peers = nil, nil
			teleportTravel(t, rt, c, clock, path)
			rt.processReturnArrivals()
			if c.BerserkUntilMs != 0 || c.NativeBodyStatus != 0 {
				t.Fatalf("berserk survived the %s teleport: until %d status %d", path, c.BerserkUntilMs, c.NativeBodyStatus)
			}
			if _, ok := rt.berserkActors.Load(simulation.WorldKey(testDivision, c.Name)); ok {
				t.Fatal("the tick index kept the teleported berserk")
			}
			if !bodyStatusFramePresent(self, gid, 0) {
				t.Fatal("the character never saw its berserk end")
			}
			if !bodyStatusFramePresent(peers, gid, 0) {
				t.Fatal("peers never saw the berserk end")
			}
			self = nil
			if got := rt.advanceBerserk(clock.NowMs() + 120000); len(got) != 0 {
				t.Fatal("the spent deadline fired after the teleport")
			}
			if bodyStatusFramePresent(self, gid, 0) {
				t.Fatal("a second berserk end was published")
			}
			activateBerserk(t, rt, c)
		})
	}
}

/*
================
TestBerserkSurvivesInPlaceReload

A mode-3 completion reloads the actor where it stands (skin change,
job dress). It is not a teleport: the reference server keeps the
object and its state timers across it, and berserk's timer is one.
================
*/
func TestBerserkSurvivesInPlaceReload(t *testing.T) {
	rt, c, clock, _ := returnFixture(t, 30000)
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(_, _ string, frames []wire.Frame) {}
	activateBerserk(t, rt, c)
	until := c.BerserkUntilMs
	here := travelPoint{spawn: rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs()),
		world: instance.ID(domain.CharacterWorldInstance(c))}
	var reload OpResult
	started := rt.deps.UpdateDeferred(c, "test-in-place-reload", func() bool {
		return rt.startReturnCast(returnCast{division: testDivision, character: c, row: -1,
			duration: skinReloadMs, destination: &here, now: clock.NowMs(), mode: skinTeleportMode}, &reload)
	})
	if !started {
		t.Fatal("the in-place reload did not start")
	}
	clock.Advance(2 * time.Second)
	advanceAndDrainReturns(rt, clock.NowMs())
	if c.BerserkUntilMs != until || c.NativeBodyStatus != 1 {
		t.Fatalf("the in-place reload ended berserk: until %d status %d", c.BerserkUntilMs, c.NativeBodyStatus)
	}
}
