/*
===========================================================================

hold_breakdown_test.go - a slow gate hold names its parts; a calm one nothing

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
stepClock

A clock the test advances by hand.
================
*/
type stepClock struct{ at time.Time }

func (c *stepClock) now() time.Time { return c.at }

func (c *stepClock) advance(d time.Duration) { c.at = c.at.Add(d) }

/*
================
holdSink

Collects what a breakdown reports, by label.
================
*/
type holdSink map[string]time.Duration

func (s holdSink) record(label string, elapsed time.Duration) { s[label] = elapsed }

/*
================
TestHoldBreakdownNamesTheSlowHoldsParts

A 160 ms hold reports each part of 1 ms or more under
"<label>.<part>", and leaves out the sub-millisecond one.
================
*/
func TestHoldBreakdownNamesTheSlowHoldsParts(t *testing.T) {
	clock := &stepClock{at: time.Unix(1_700_000_000, 0)}
	hold := newHoldBreakdown("returnArrival", clock.now)
	clock.advance(10 * time.Millisecond)
	hold.mark("door")
	clock.advance(140 * time.Millisecond)
	hold.mark("reentry")
	clock.advance(500 * time.Microsecond)
	hold.mark("frames")
	clock.advance(9 * time.Millisecond)
	hold.mark("push")
	got := holdSink{}
	hold.report(got.record)
	want := holdSink{
		"returnArrival.door":    10 * time.Millisecond,
		"returnArrival.reentry": 140 * time.Millisecond,
		"returnArrival.push":    9 * time.Millisecond,
	}
	if len(got) != len(want) {
		t.Fatalf("reported %v, want %v", got, want)
	}
	for label, elapsed := range want {
		if got[label] != elapsed {
			t.Fatalf("%s = %v, want %v (all %v)", label, got[label], elapsed, got)
		}
	}
}

/*
================
TestHoldBreakdownStaysQuietUnderTheThreshold

A 99 ms hold reports nothing, however its parts split.
================
*/
func TestHoldBreakdownStaysQuietUnderTheThreshold(t *testing.T) {
	clock := &stepClock{at: time.Unix(1_700_000_000, 0)}
	hold := newHoldBreakdown("returnArrival", clock.now)
	clock.advance(60 * time.Millisecond)
	hold.mark("reentry")
	clock.advance(39 * time.Millisecond)
	hold.mark("push")
	got := holdSink{}
	hold.report(got.record)
	if len(got) != 0 {
		t.Fatalf("a calm hold reported %v", got)
	}
}

/*
================
TestHoldBreakdownFoldsPartsPastItsRoom

Marks past maxHoldParts add to the last part, so the parts still sum to
the hold.
================
*/
func TestHoldBreakdownFoldsPartsPastItsRoom(t *testing.T) {
	clock := &stepClock{at: time.Unix(1_700_000_000, 0)}
	hold := newHoldBreakdown("h", clock.now)
	for i := 0; i < maxHoldParts+3; i++ {
		clock.advance(20 * time.Millisecond)
		hold.mark("p")
	}
	var total time.Duration
	for _, part := range hold.parts[:hold.n] {
		total += part.elapsed
	}
	if hold.n != maxHoldParts || total != time.Duration(maxHoldParts+3)*20*time.Millisecond {
		t.Fatalf("parts %d, total %v", hold.n, total)
	}
}

/*
================
TestHoldBreakdownZeroAndNilRecordNothing

The completion's other callers pass nil, and a zero value marks and
reports nothing; neither panics.
================
*/
func TestHoldBreakdownZeroAndNilRecordNothing(t *testing.T) {
	var none *holdBreakdown
	none.mark("door")
	none.report(holdSink{}.record)
	var zero holdBreakdown
	zero.mark("door")
	got := holdSink{}
	zero.report(got.record)
	if len(got) != 0 || zero.n != 0 {
		t.Fatalf("zero breakdown recorded %v (%d parts)", got, zero.n)
	}
}

/*
================
TestHoldBreakdownCalmPathAllocatesNothing

The lane runs this on every arrival; a calm hold must cost no heap.
================
*/
func TestHoldBreakdownCalmPathAllocatesNothing(t *testing.T) {
	sink := holdSink{}.record
	allocs := testing.AllocsPerRun(100, func() {
		hold := newHoldBreakdown("returnArrival", time.Now)
		hold.mark("door")
		hold.mark("reentry")
		hold.report(sink)
	})
	if allocs != 0 {
		t.Fatalf("calm hold allocated %v times", allocs)
	}
}

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
	got := holdSink{}
	rt.PublicationHold = got.record
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
