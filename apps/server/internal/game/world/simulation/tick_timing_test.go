/*
===========================================================================

tick_timing_test.go - phase durations and slow hooks on a supplied clock

Every test drives the ticker's Now; no wall time is measured, so scheduler
pauses cannot change a result.

===========================================================================
*/

package simulation

import (
	"context"
	"testing"
	"time"
)

/*
================
fakeTickClock
================
*/
type fakeTickClock struct{ now time.Time }

func (c *fakeTickClock) Now() time.Time                { return c.now }
func (c *fakeTickClock) advance(elapsed time.Duration) { c.now = c.now.Add(elapsed) }

/*
================
timedTicker
================
*/
func timedTicker(clock *fakeTickClock, hooks ...NamedTickHook) (*Ticker, *[]TickTiming) {
	got := &[]TickTiming{}
	ticker := &Ticker{
		Source: emptySessionSource{}, Push: discardPusher{}, Now: clock.Now, Hooks: hooks,
		PhaseObserver: func(timing TickTiming) { *got = append(*got, timing) },
	}
	return ticker, got
}

/*
================
TestTickReportsPhasesAndNamesSlowHooks
================
*/
func TestTickReportsPhasesAndNamesSlowHooks(t *testing.T) {
	clock := &fakeTickClock{now: time.UnixMilli(1000)}
	fast := func(int64) []DivisionFrames { clock.advance(time.Millisecond); return nil }
	slow := func(int64) []DivisionFrames { clock.advance(150 * time.Millisecond); return nil }
	// The slow hook is an anonymous closure on purpose: its registered
	// name, not its runtime name (<constructor>.funcN), is reported
	// (#2294).
	ticker, got := timedTicker(clock,
		NamedTickHook{Hook: fast},
		NamedTickHook{Name: "skill-objects", Hook: slow})
	ticker.RunTick(0)
	if len(*got) != 1 {
		t.Fatalf("observer saw %d ticks, want 1", len(*got))
	}
	timing := (*got)[0]
	if timing.Hooks != 151*time.Millisecond || timing.Total != 151*time.Millisecond ||
		timing.BeforeHooks != 0 || timing.Divisions != 0 {
		t.Fatalf("phases = %+v", timing)
	}
	if len(timing.SlowHooks) != 1 || timing.SlowHooks[0].Name != "skill-objects" ||
		timing.SlowHooks[0].Elapsed != 150*time.Millisecond {
		t.Fatalf("slow hooks = %+v", timing.SlowHooks)
	}
}

/*
================
TestCancelledScheduledTickHasNoNegativePhase

A tick cancelled before its hooks closes the phases it never entered at
its end instead of reporting a negative division time.
================
*/
func TestCancelledScheduledTickHasNoNegativePhase(t *testing.T) {
	clock := &fakeTickClock{now: time.UnixMilli(1000)}
	before := func(int64) []DivisionFrames { clock.advance(20 * time.Millisecond); return nil }
	ticker, got := timedTicker(clock)
	ticker.BeforeHooks = []NamedTickHook{{Hook: before}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ticker.runScheduledTick(ctx, 0, nil)
	if len(*got) != 1 {
		t.Fatalf("observer saw %d ticks, want 1", len(*got))
	}
	timing := (*got)[0]
	if timing.BeforeHooks != 20*time.Millisecond || timing.Divisions < 0 || timing.Hooks != 0 ||
		timing.Total != timing.BeforeHooks+timing.Divisions+timing.Hooks {
		t.Fatalf("cancelled tick phases = %+v", timing)
	}
}

/*
================
TestPanickingSlowHookIsStillCounted
================
*/
func TestPanickingSlowHookIsStillCounted(t *testing.T) {
	clock := &fakeTickClock{now: time.UnixMilli(1000)}
	panics := func(int64) []DivisionFrames {
		clock.advance(200 * time.Millisecond)
		panic("hook failure")
	}
	ticker, got := timedTicker(clock, NamedTickHook{Hook: panics})
	ticker.RunTick(0)
	if len(*got) != 1 || len((*got)[0].SlowHooks) != 1 || (*got)[0].SlowHooks[0].Elapsed != 200*time.Millisecond {
		t.Fatalf("panicking slow hook lost: %+v", *got)
	}
}

/*
================
TestSlowDivisionIsNamed
================
*/
func TestSlowDivisionIsNamed(t *testing.T) {
	var c tickClock
	c.begin(time.UnixMilli(1000))
	c.timeDivision("global-official", 30*time.Millisecond)
	c.timeDivision("dungeon-1", 120*time.Millisecond)
	timing := c.timing(time.UnixMilli(1200))
	if len(timing.SlowDivisions) != 1 || timing.SlowDivisions[0].Name != "dungeon-1" {
		t.Fatalf("slow divisions = %+v", timing.SlowDivisions)
	}
}

/*
================
TestStepTimerNamesSlowSteps

Only steps at or above the threshold are reported, a panicking step is
still reported, and the zero value just runs the step.
================
*/
func TestStepTimerNamesSlowSteps(t *testing.T) {
	clock := time.UnixMilli(0)
	advance := time.Duration(0)
	var slow []SlowHookTiming
	steps := StepTimer{
		Now: func() time.Time { return clock },
		Slow: func(name string, elapsed time.Duration) {
			slow = append(slow, SlowHookTiming{Name: name, Elapsed: elapsed})
		},
	}
	run := func(name string, elapsed time.Duration) {
		advance = elapsed
		steps.Time(name, func() { clock = clock.Add(advance) })
	}
	run("fast", 10*time.Millisecond)
	run("slow", 150*time.Millisecond)
	func() {
		defer func() { _ = recover() }()
		steps.Time("panics", func() {
			clock = clock.Add(SlowHookThreshold)
			panic("step failed")
		})
	}()
	if len(slow) != 2 || slow[0] != (SlowHookTiming{Name: "slow", Elapsed: 150 * time.Millisecond}) ||
		slow[1].Name != "panics" {
		t.Fatalf("slow steps = %+v", slow)
	}
	ran := false
	var zero StepTimer
	zero.Time("unwired", func() { ran = true })
	if !ran {
		t.Fatal("the zero StepTimer did not run its step")
	}
}

/*
================
TestStepTimerBeginEndMatchesTime

The span must record exactly what Time records - the total, the slow
names and the zone's open and close around them - for a fast step, a
slow step and a panicking one.
================
*/
func TestStepTimerBeginEndMatchesTime(t *testing.T) {
	record := func(spans bool) []string {
		clock := time.UnixMilli(0)
		var events []string
		steps := StepTimer{
			Now: func() time.Time { return clock },
			Total: func(name string, elapsed time.Duration) {
				events = append(events, "total:"+name+":"+elapsed.String())
			},
			Slow: func(name string, elapsed time.Duration) {
				events = append(events, "slow:"+name+":"+elapsed.String())
			},
			Zone: func(name string) func() {
				events = append(events, "open:"+name)
				return func() { events = append(events, "close:"+name) }
			},
		}
		step := func(name string, body func()) {
			if spans {
				span := steps.Begin(name)
				defer span.End()
				body()
				return
			}
			steps.Time(name, body)
		}
		step("fast", func() { clock = clock.Add(10 * time.Millisecond) })
		step("slow", func() { clock = clock.Add(150 * time.Millisecond) })
		func() {
			defer func() { _ = recover() }()
			step("panics", func() {
				clock = clock.Add(SlowHookThreshold)
				panic("step failed")
			})
		}()
		return events
	}
	viaTime, viaSpans := record(false), record(true)
	if len(viaTime) != len(viaSpans) {
		t.Fatalf("span events %v, Time events %v", viaSpans, viaTime)
	}
	for i := range viaTime {
		if viaTime[i] != viaSpans[i] {
			t.Fatalf("event %d: span %q, Time %q (span %v, Time %v)", i, viaSpans[i], viaTime[i], viaSpans, viaTime)
		}
	}
	var zero StepTimer
	zero.Begin("unwired").End()
}

// spanAllocTimer / spanAllocSink keep TestStepSpanBeginEndAllocatesNothing's
// measured pair free of capture boxes of its own.
var spanAllocTimer = &StepTimer{
	Now:   func() time.Time { return time.UnixMilli(0) },
	Total: func(string, time.Duration) {},
}

var spanAllocSink StepSpan

/*
================
TestStepSpanBeginEndAllocatesNothing

Retiring Time's per-step closure box on the tick path is the span's
whole reason to exist (#2030); pin the pair at zero allocations.
================
*/
func TestStepSpanBeginEndAllocatesNothing(t *testing.T) {
	allocs := testing.AllocsPerRun(100, func() {
		spanAllocSink = spanAllocTimer.Begin("step")
		spanAllocSink.End()
	})
	if allocs != 0 {
		t.Fatalf("Begin/End allocated %v per run, want 0", allocs)
	}
}
