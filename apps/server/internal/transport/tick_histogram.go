/*
===========================================================================

tick_histogram.go - the whole-tick duration distribution

tickStats keeps last/mean/max/overruns; those cannot tell "every tick is a
little slow" from "one tick in five is very slow". This fixed-bound
histogram, dense around the 100 ms budget, gives the percentiles a lag
investigation needs from production and local runs alike. Recording is one
array increment under the tick mutex the caller already holds.

===========================================================================
*/
package transport

import "time"

// tickHistogramBoundsMs are the inclusive upper bounds of the whole-tick
// buckets; one more bucket holds anything slower. Dense from 50 to 200 ms
// so a percentile can be placed against the 100 ms budget.
var tickHistogramBoundsMs = [...]float64{
	2, 5, 10, 20, 30, 40, 50, 60, 70, 80, 90, 100, 110, 120, 140, 160, 180, 200,
	250, 300, 400, 500, 750, 1000, 2000,
}

const tickHistogramBuckets = len(tickHistogramBoundsMs) + 1

/*
================
TickHistogram

The served form: the bucket bounds, the counts (the last count is above
the last bound) and the percentiles read as their bucket's upper bound.
A percentile in the open last bucket reads as the largest tick seen.
================
*/
type TickHistogram struct {
	BoundsMs []float64 `json:"bounds_ms"`
	Counts   []uint64  `json:"counts"`
	P50Ms    float64   `json:"p50_ms"`
	P90Ms    float64   `json:"p90_ms"`
	P99Ms    float64   `json:"p99_ms"`
	P999Ms   float64   `json:"p999_ms"`
}

/*
================
tickBucket
================
*/
func tickBucket(elapsed time.Duration) int {
	ms := float64(elapsed) / float64(time.Millisecond)
	for i, bound := range tickHistogramBoundsMs {
		if ms <= bound {
			return i
		}
	}
	return len(tickHistogramBoundsMs)
}

/*
================
tickPercentile

The upper bound of the bucket holding the q-th fraction of count ticks.
================
*/
func tickPercentile(counts *[tickHistogramBuckets]uint64, count uint64, q float64, max time.Duration) float64 {
	if count == 0 {
		return 0
	}
	// The rank of the q-th tick, 1-based, rounded up: p99 of 100 ticks is
	// the 99th, never the 100th.
	rank := uint64(q*float64(count) + 0.999999999)
	if rank < 1 {
		rank = 1
	}
	seen := uint64(0)
	for i, n := range counts {
		seen += n
		if seen >= rank {
			if i < len(tickHistogramBoundsMs) {
				return tickHistogramBoundsMs[i]
			}
			break
		}
	}
	return float64(max) / float64(time.Millisecond)
}

/*
================
tickHistogramSnapshot
================
*/
func tickHistogramSnapshot(counts *[tickHistogramBuckets]uint64, count uint64, max time.Duration) TickHistogram {
	return TickHistogram{
		BoundsMs: append([]float64(nil), tickHistogramBoundsMs[:]...),
		Counts:   append([]uint64(nil), counts[:]...),
		P50Ms:    tickPercentile(counts, count, 0.50, max),
		P90Ms:    tickPercentile(counts, count, 0.90, max),
		P99Ms:    tickPercentile(counts, count, 0.99, max),
		P999Ms:   tickPercentile(counts, count, 0.999, max),
	}
}
