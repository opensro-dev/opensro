/*
===========================================================================

latency.go - fixed-bucket latency histograms recorded without a lock

The transport's handler and tick-phase histograms and the authority
store's lock timing share this one recording form. Buckets are fixed and
recording is atomic adds plus a CAS on the maximum, so a hot path (every
inbound frame, every store read) can record without allocating or
locking. The bounds are fine below 16 ms (a frame at 60 Hz) and coarse up
to a second; one more bucket holds the rest.

===========================================================================
*/

package latency

import (
	"math"
	"sync/atomic"
	"time"
)

// BoundsMs are the inclusive upper bounds of the buckets; one more bucket
// holds the rest.
var BoundsMs = [...]float64{1, 2, 5, 10, 16, 25, 50, 100, 250, 1000}

// Buckets is the bucket count, the open last one included.
const Buckets = len(BoundsMs) + 1

/*
================
Histogram

Counts per bucket (the last count is above the last bound), the bounds
they use, the largest sample, and the percentiles read as their bucket's
upper bound (the largest sample when they fall in the open last bucket);
the snapshot form served on /transport/metrics.
================
*/
type Histogram struct {
	BoundsMs []float64 `json:"bounds_ms"`
	Buckets  []uint64  `json:"buckets"`
	MaxMs    float64   `json:"max_ms"`
	P50Ms    float64   `json:"p50_ms"`
	P99Ms    float64   `json:"p99_ms"`
	P999Ms   float64   `json:"p999_ms"`
}

/*
================
Live

The recording form: atomic counters, so recording takes no lock. maxBits
holds the largest sample's float64 bits. The zero value is ready.
================
*/
type Live struct {
	buckets [Buckets]atomic.Uint64
	maxBits atomic.Uint64
}

/*
================
Bucket
================
*/
func Bucket(ms float64) int {
	for i, bound := range BoundsMs {
		if ms <= bound {
			return i
		}
	}
	return Buckets - 1
}

/*
================
Live.Add

Records one sample and reports whether it is the largest so far, so a
caller can keep what made it the largest (a label) without a lock on the
common path.
================
*/
func (h *Live) Add(elapsed time.Duration) bool {
	ms := float64(elapsed) / float64(time.Millisecond)
	h.buckets[Bucket(ms)].Add(1)
	for {
		old := h.maxBits.Load()
		if ms <= math.Float64frombits(old) {
			return false
		}
		if h.maxBits.CompareAndSwap(old, math.Float64bits(ms)) {
			return true
		}
	}
}

/*
================
Live.Snapshot
================
*/
func (h *Live) Snapshot() Histogram {
	out := Histogram{BoundsMs: BoundsMs[:], Buckets: make([]uint64, Buckets)}
	for i := range h.buckets {
		out.Buckets[i] = h.buckets[i].Load()
	}
	out.MaxMs = math.Float64frombits(h.maxBits.Load())
	out.P50Ms = Percentile(out, 0.5)
	out.P99Ms = Percentile(out, 0.99)
	out.P999Ms = Percentile(out, 0.999)
	return out
}

/*
================
Percentile

The upper bound of the bucket holding the q-th sample, or the largest
sample when it falls in the open last bucket; 0 with no samples.
================
*/
func Percentile(h Histogram, q float64) float64 {
	var total uint64
	for _, n := range h.Buckets {
		total += n
	}
	if total == 0 {
		return 0
	}
	rank := uint64(math.Ceil(q * float64(total)))
	var seen uint64
	for i, n := range h.Buckets {
		seen += n
		if seen >= rank {
			if i < len(h.BoundsMs) {
				return min(h.BoundsMs[i], h.MaxMs)
			}
			return h.MaxMs
		}
	}
	return h.MaxMs
}
