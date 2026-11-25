/*
===========================================================================

hold_breakdown_test.go - a slow gate hold names its parts; a calm one nothing

===========================================================================
*/

package simulation

import (
	"testing"
	"time"
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
	hold := NewHoldBreakdown("returnArrival", clock.now)
	clock.advance(10 * time.Millisecond)
	hold.Mark("door")
	clock.advance(140 * time.Millisecond)
	hold.Mark("reentry")
	clock.advance(500 * time.Microsecond)
	hold.Mark("frames")
	clock.advance(9 * time.Millisecond)
	hold.Mark("push")
	got := holdSink{}
	hold.Report(got.record)
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
	hold := NewHoldBreakdown("returnArrival", clock.now)
	clock.advance(60 * time.Millisecond)
	hold.Mark("reentry")
	clock.advance(39 * time.Millisecond)
	hold.Mark("push")
	got := holdSink{}
	hold.Report(got.record)
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
	hold := NewHoldBreakdown("h", clock.now)
	for i := 0; i < maxHoldParts+3; i++ {
		clock.advance(20 * time.Millisecond)
		hold.Mark("p")
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
	var none *HoldBreakdown
	none.Mark("door")
	none.Report(holdSink{}.record)
	var zero HoldBreakdown
	zero.Mark("door")
	got := holdSink{}
	zero.Report(got.record)
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
		hold := NewHoldBreakdown("returnArrival", time.Now)
		hold.Mark("door")
		hold.Mark("reentry")
		hold.Report(sink)
	})
	if allocs != 0 {
		t.Fatalf("calm hold allocated %v times", allocs)
	}
}
