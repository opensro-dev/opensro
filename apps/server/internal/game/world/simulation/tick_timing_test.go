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
	"strings"
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
func timedTicker(clock *fakeTickClock, hooks ...TickHook) (*Ticker, *[]TickTiming) {
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
	ticker, got := timedTicker(clock, fast, slow)
	ticker.RunTick(0)
	if len(*got) != 1 {
		t.Fatalf("observer saw %d ticks, want 1", len(*got))
	}
	timing := (*got)[0]
	if timing.Hooks != 151*time.Millisecond || timing.Total != 151*time.Millisecond ||
		timing.BeforeHooks != 0 || timing.Divisions != 0 {
		t.Fatalf("phases = %+v", timing)
	}
	if len(timing.SlowHooks) != 1 || !strings.Contains(timing.SlowHooks[0].Name, "TestTickReportsPhasesAndNamesSlowHooks") ||
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
	ticker.BeforeHooks = []TickHook{before}
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
	ticker, got := timedTicker(clock, panics)
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
