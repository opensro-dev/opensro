/*
===========================================================================

returnarrival_hold_test.go - a slow return-scroll arrival names its step

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
slowReentry

Deps whose re-entry rebuild costs 150 ms on the test clock: the step a
live returnArrival hold is suspected to spend its time in (#2802).
================
*/
type slowReentry struct {
	*enterworld.Deps
	clock *fakeClock
}

func (d *slowReentry) ReentryPackets(divisionID, characterName string) ([]enterworld.Packet, bool) {
	d.clock.now = d.clock.now.Add(150 * time.Millisecond)
	return d.Deps.ReentryPackets(divisionID, characterName)
}

/*
================
TestSlowReturnArrivalNamesTheReentryStep

The arrival lane, end to end: a completion whose re-entry rebuild is
slow reports returnArrival.reentry with that cost, beside nothing for
the steps that took no time, so a live publication_holds read names the
step that kept the gate.
================
*/
func TestSlowReturnArrivalNamesTheReentryStep(t *testing.T) {
	c := rebirthTestCharacter(20, 100)
	c.World.RebirthPoint = worldSpawnFromMission(simulation.Spawn{RegionID: 25416, X: 703, Y: 42, Z: 1575})
	rt, clock := newTestRuntime(c, testItems())
	rt.deps = &slowReentry{Deps: rt.deps.(*enterworld.Deps), clock: clock}
	rt.PushCharacterFrames = func(string, string, []wire.Frame) {}
	rt.PushDivisionPeerFrames = func(string, string, []wire.Frame) {}
	got := map[string]time.Duration{}
	rt.PublicationHold = func(label string, elapsed time.Duration) { got[label] = elapsed }
	c.NativeTeleportMode = 1
	job := pendingReturn{division: testDivision, name: c.Name, character: c, due: clock.NowMs()}
	rt.returnCasts.Store(simulation.WorldKey(testDivision, c.Name), job)
	rt.runReturnArrival(job)
	if c.NativeTeleportMode != 0 {
		t.Fatal("the arrival did not complete")
	}
	if got["returnArrival.reentry"] != 150*time.Millisecond {
		t.Fatalf("reported %v, want returnArrival.reentry = 150ms", got)
	}
	for label := range got {
		if label != "returnArrival.reentry" && label != "returnArrival" {
			t.Fatalf("a step that took no test time was reported: %v", got)
		}
	}
}
