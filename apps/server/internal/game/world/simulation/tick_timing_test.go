/*
===========================================================================

tick_timing_test.go - the ticker reports phase durations and names slow hooks

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
slowTickHook

Busy-waits past the slow threshold (the test lint forbids sleeping).
================
*/
func slowTickHook(int64) []DivisionFrames {
	for started := time.Now(); time.Since(started) < SlowHookThreshold+5*time.Millisecond; {
	}
	return nil
}

/*
================
TestTickReportsPhasesAndNamesSlowHooks
================
*/
func TestTickReportsPhasesAndNamesSlowHooks(t *testing.T) {
	var got []TickTiming
	fast := func(int64) []DivisionFrames { return nil }
	ticker := &Ticker{
		Source: emptySessionSource{}, Push: discardPusher{},
		Hooks:         []TickHook{fast, slowTickHook},
		PhaseObserver: func(timing TickTiming) { got = append(got, timing) },
	}
	ticker.RunTick(0)
	ticker.Hooks = []TickHook{fast}
	ticker.RunTick(100)
	if len(got) != 2 {
		t.Fatalf("observer saw %d ticks, want 2", len(got))
	}
	first := got[0]
	if len(first.SlowHooks) != 1 || !strings.Contains(first.SlowHooks[0].Name, "slowTickHook") ||
		first.SlowHooks[0].Elapsed < SlowHookThreshold {
		t.Fatalf("slow hooks = %+v", first.SlowHooks)
	}
	if first.Hooks < SlowHookThreshold || first.Total < first.Hooks || first.Total < first.BeforeHooks+first.Divisions {
		t.Fatalf("phases do not add up: %+v", first)
	}
	if len(got[1].SlowHooks) != 0 || got[1].Total >= SlowHookThreshold {
		t.Fatalf("a fast tick reported %+v", got[1])
	}
}
