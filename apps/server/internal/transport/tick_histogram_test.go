/*
===========================================================================

tick_histogram_test.go - whole-tick percentiles against the 100 ms budget

===========================================================================
*/
package transport

import (
	"testing"
	"time"
)

/*
================
recordTicks
================
*/
func recordTicks(h *Hub, n int, each time.Duration) {
	for i := 0; i < n; i++ {
		h.RecordTickDuration(each, 100*time.Millisecond)
	}
}

/*
================
TestTickHistogramPercentiles

One tick in ten past budget reads as p90 over 100 ms, which last/mean/max
cannot show; the open bucket reads as the largest tick.
================
*/
func TestTickHistogramPercentiles(t *testing.T) {
	h := &Hub{}
	recordTicks(h, 899, 30*time.Millisecond)
	recordTicks(h, 90, 150*time.Millisecond)
	recordTicks(h, 10, 600*time.Millisecond)
	recordTicks(h, 1, 3*time.Second)
	var m Metrics
	h.metrics.ticks.snapshotInto(&m)
	got := m.TickHistogram
	if got.P50Ms != 30 || got.P90Ms != 160 || got.P99Ms != 750 || got.P999Ms != 750 {
		t.Fatalf("p50/p90/p99/p999 = %v/%v/%v/%v, want 30/160/750/750", got.P50Ms, got.P90Ms, got.P99Ms, got.P999Ms)
	}
	if len(got.Counts) != len(got.BoundsMs)+1 || got.Counts[len(got.Counts)-1] != 1 {
		t.Fatalf("counts %v against %d bounds", got.Counts, len(got.BoundsMs))
	}
	total := uint64(0)
	for _, n := range got.Counts {
		total += n
	}
	if total != m.TickCount || m.TickOverruns != 101 {
		t.Fatalf("histogram holds %d of %d ticks, overruns %d", total, m.TickCount, m.TickOverruns)
	}

	// A percentile that lands in the open bucket reads as the largest tick.
	slow := &Hub{}
	recordTicks(slow, 3, 2500*time.Millisecond)
	slow.metrics.ticks.snapshotInto(&m)
	if m.TickHistogram.P50Ms != 2500 {
		t.Fatalf("open-bucket p50 = %v, want the 2500 ms maximum", m.TickHistogram.P50Ms)
	}

	// A bound is inclusive: a tick of exactly 100 ms is within budget.
	exact := &Hub{}
	recordTicks(exact, 1, 100*time.Millisecond)
	exact.metrics.ticks.snapshotInto(&m)
	if m.TickHistogram.P50Ms != 100 || m.TickOverruns != 0 {
		t.Fatalf("a 100 ms tick read p50 %v, overruns %d", m.TickHistogram.P50Ms, m.TickOverruns)
	}
}
