/*
===========================================================================

runtime_stats.go - the Go runtime's own costs on /transport/metrics

A tick or a handler can stall for reasons no hook timer names: a garbage
collector pause, a scheduler that left the tick goroutine waiting, a heap
that grew until collection runs constantly. These read the runtime's
cumulative counters (runtime/metrics) at snapshot time; nothing is
recorded per tick, so the hot path pays nothing.

===========================================================================
*/

package transport

import (
	"math"
	"runtime/metrics"
)

// The runtime/metrics series runtimeStats reads.
const (
	metricGCPauses     = "/sched/pauses/total/gc:seconds"
	metricSchedLatency = "/sched/latencies:seconds"
	metricHeapLive     = "/memory/classes/heap/objects:bytes"
	metricHeapGoal     = "/gc/heap/goal:bytes"
	metricGoroutines   = "/sched/goroutines:goroutines"
	metricGCCycles     = "/gc/cycles/total:gc-cycles"
	metricAllocs       = "/gc/heap/allocs:bytes"
)

/*
================
RuntimeStats

The served form. Pause and latency percentiles are read as their
runtime bucket's upper bound since the process started, in milliseconds.
================
*/
type RuntimeStats struct {
	GCPauseP99Ms      float64 `json:"gc_pause_p99_ms"`
	GCPauseMaxMs      float64 `json:"gc_pause_max_ms"`
	SchedLatencyP99Ms float64 `json:"sched_latency_p99_ms"`
	SchedLatencyMaxMs float64 `json:"sched_latency_max_ms"`
	HeapLiveBytes     uint64  `json:"heap_live_bytes"`
	HeapGoalBytes     uint64  `json:"heap_goal_bytes"`
	AllocatedBytes    uint64  `json:"allocated_bytes"`
	Goroutines        uint64  `json:"goroutines"`
	GCCycles          uint64  `json:"gc_cycles"`
}

/*
================
runtimeStats
================
*/
func runtimeStats() RuntimeStats {
	samples := []metrics.Sample{
		{Name: metricGCPauses}, {Name: metricSchedLatency}, {Name: metricHeapLive}, {Name: metricHeapGoal},
		{Name: metricGoroutines}, {Name: metricGCCycles}, {Name: metricAllocs},
	}
	metrics.Read(samples)
	var out RuntimeStats
	for _, s := range samples {
		switch s.Value.Kind() {
		case metrics.KindFloat64Histogram:
			p99, maxMs := runtimeHistogramMs(s.Value.Float64Histogram())
			switch s.Name {
			case metricGCPauses:
				out.GCPauseP99Ms, out.GCPauseMaxMs = p99, maxMs
			case metricSchedLatency:
				out.SchedLatencyP99Ms, out.SchedLatencyMaxMs = p99, maxMs
			}
		case metrics.KindUint64:
			switch s.Name {
			case metricHeapLive:
				out.HeapLiveBytes = s.Value.Uint64()
			case metricHeapGoal:
				out.HeapGoalBytes = s.Value.Uint64()
			case metricGoroutines:
				out.Goroutines = s.Value.Uint64()
			case metricGCCycles:
				out.GCCycles = s.Value.Uint64()
			case metricAllocs:
				out.AllocatedBytes = s.Value.Uint64()
			}
		}
	}
	return out
}

/*
================
runtimeHistogramMs

The p99 and the largest non-empty bucket of a runtime histogram, each read
as its bucket's upper bound in milliseconds. An infinite upper bound reads
as the bucket's lower bound.
================
*/
func runtimeHistogramMs(h *metrics.Float64Histogram) (p99, maxMs float64) {
	var total uint64
	for _, n := range h.Counts {
		total += n
	}
	if total == 0 {
		return 0, 0
	}
	upper := func(i int) float64 {
		if b := h.Buckets[i+1]; !math.IsInf(b, 1) {
			return b * 1000
		}
		return h.Buckets[i] * 1000
	}
	rank := uint64(math.Ceil(0.99 * float64(total)))
	var seen uint64
	p99Found := false
	for i, n := range h.Counts {
		seen += n
		if !p99Found && seen >= rank {
			p99, p99Found = upper(i), true
		}
		if n > 0 {
			maxMs = upper(i)
		}
	}
	return p99, maxMs
}
