/*
===========================================================================

tick_window_test.go - percentiles of the ticks between two snapshots

===========================================================================
*/
package transport

import (
	"testing"
	"time"
)

/*
================
TestTickWindowReadsOnlyTheTicksBetweenSnapshots

A slow past does not colour a fast window: the window's percentiles and
overruns count only the ticks recorded after the earlier snapshot.
================
*/
func TestTickWindowReadsOnlyTheTicksBetweenSnapshots(t *testing.T) {
	hub := &Hub{}
	for i := 0; i < 100; i++ {
		hub.RecordTickDuration(150*time.Millisecond, 100*time.Millisecond)
	}
	earlier := hub.Metrics()
	for i := 0; i < 99; i++ {
		hub.RecordTickDuration(8*time.Millisecond, 100*time.Millisecond)
	}
	hub.RecordTickDuration(130*time.Millisecond, 100*time.Millisecond)
	window := TickWindowBetween(earlier, hub.Metrics())
	if window.Count != 100 || window.Overruns != 1 || window.P50Ms != 10 || window.P99Ms != 10 {
		t.Fatalf("window %+v, want 100 ticks, 1 overrun, p50 10, p99 10", window)
	}
	if empty := TickWindowBetween(hub.Metrics(), earlier); empty != (TickWindow{}) {
		t.Fatalf("a snapshot from before a restart gave %+v, want an empty window", empty)
	}
}
