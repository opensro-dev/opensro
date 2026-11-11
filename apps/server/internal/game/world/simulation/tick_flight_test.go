/*
===========================================================================

tick_flight_test.go - the flight recorder dumps once, names the step, and costs nothing

Every test drives the ticker's supplied clock (tick_timing_test.go);
no wall time is measured, so scheduler pauses cannot change a
result. The benchmark is the recorder's per-tick add, GC sample
included.

===========================================================================
*/

package simulation

import (
	"strings"
	"testing"
	"time"
)

/*
================
flightDumps
================
*/
func flightDumps(ticker *Ticker) *[]string {
	dumps := &[]string{}
	ticker.flight.emit = func(line string) { *dumps = append(*dumps, line) }
	return dumps
}

/*
================
TestFlightRecorderDumpsOverBudgetTickNamingSlowStep

A fast tick records silently; the tick that runs over budget dumps
the window once, as one line, naming the slow step and carrying
the ticks before it.
================
*/
func TestFlightRecorderDumpsOverBudgetTickNamingSlowStep(t *testing.T) {
	clock := &fakeTickClock{now: time.UnixMilli(1000)}
	fast := func(int64) []DivisionFrames { clock.advance(time.Millisecond); return nil }
	ticker, _ := timedTicker(clock, NamedTickHook{Name: "fast", Hook: fast})
	dumps := flightDumps(ticker)

	ticker.RunTick(0)
	if len(*dumps) != 0 {
		t.Fatalf("fast tick dumped: %v", *dumps)
	}

	slow := func(int64) []DivisionFrames { clock.advance(150 * time.Millisecond); return nil }
	ticker.Hooks = []NamedTickHook{{Name: "skill-objects", Hook: slow}}
	ticker.RunTick(0)
	if len(*dumps) != 1 {
		t.Fatalf("over-budget tick produced %d dumps, want exactly 1: %v", len(*dumps), *dumps)
	}
	dump := (*dumps)[0]
	if strings.Contains(dump, "\n") {
		t.Fatalf("dump is not one line: %q", dump)
	}
	for _, want := range []string{"tick #2", "total=150ms", "budget=100ms", "last 2 ticks", "#1 total=1ms", "skill-objects:150ms", "players=0", "gc="} {
		if !strings.Contains(dump, want) {
			t.Fatalf("dump missing %q: %s", want, dump)
		}
	}
}

/*
================
TestFlightRecorderRateLimitsConsecutiveSlowTicks

A storm of over-budget ticks dumps once; the next dump waits for
the rate-limit interval, then fires again.
================
*/
func TestFlightRecorderRateLimitsConsecutiveSlowTicks(t *testing.T) {
	clock := &fakeTickClock{now: time.UnixMilli(1000)}
	slow := func(int64) []DivisionFrames { clock.advance(150 * time.Millisecond); return nil }
	ticker, _ := timedTicker(clock, NamedTickHook{Name: "skill-objects", Hook: slow})
	dumps := flightDumps(ticker)

	for i := 0; i < 5; i++ {
		ticker.RunTick(0)
	}
	if len(*dumps) != 1 {
		t.Fatalf("5 consecutive slow ticks produced %d dumps, want 1", len(*dumps))
	}

	clock.advance(flightDumpMinInterval)
	ticker.RunTick(0)
	if len(*dumps) != 2 {
		t.Fatalf("slow tick after the rate-limit interval produced %d dumps, want 2", len(*dumps))
	}
}

/*
================
TestFlightRecorderKeepsOnlyTheLastWindow

The ring holds the last flightRingSize ticks: older ticks are gone
from the dump, and every kept tick carries its player count.
================
*/
func TestFlightRecorderKeepsOnlyTheLastWindow(t *testing.T) {
	var dumps []string
	recorder := &tickFlightRecorder{emit: func(line string) { dumps = append(dumps, line) }}
	now := time.UnixMilli(1000)
	for i := 0; i < flightRingSize+8; i++ {
		recorder.record(TickTiming{Total: time.Millisecond}, 3, 0, now, DefaultTickInterval)
	}
	recorder.record(TickTiming{
		Total:     150 * time.Millisecond,
		SlowHooks: []SlowHookTiming{{Name: "slow-step", Elapsed: 150 * time.Millisecond}},
	}, 3, 0, now, DefaultTickInterval)

	if len(dumps) != 1 {
		t.Fatalf("got %d dumps, want 1", len(dumps))
	}
	dump := dumps[0]
	for _, want := range []string{"last 32 ticks", "#41 total=150ms", "players=3", "slow-step:150ms"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("dump missing %q: %s", want, dump)
		}
	}
	if strings.Contains(dump, "#9 total=") {
		t.Fatalf("dump kept a tick older than the window: %s", dump)
	}
}

/*
================
BenchmarkTickFlightRecord

The recorder's whole per-tick add - the ring store and the GC
pause sample - at 0 allocs/op (#3226). The tick stays under
budget, so nothing dumps.
================
*/
func BenchmarkTickFlightRecord(b *testing.B) {
	recorder := &tickFlightRecorder{emit: func(string) {}}
	timing := TickTiming{
		BeforeHooks: time.Millisecond, Divisions: 2 * time.Millisecond,
		Hooks: 2 * time.Millisecond, Total: 5 * time.Millisecond,
	}
	now := time.UnixMilli(1_700_000_000_000)
	// Arm the GC baseline outside the measured loop.
	recorder.record(timing, 25, recorder.gcPauseDelta(), now, DefaultTickInterval)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		recorder.record(timing, 25, recorder.gcPauseDelta(), now, DefaultTickInterval)
	}
}
