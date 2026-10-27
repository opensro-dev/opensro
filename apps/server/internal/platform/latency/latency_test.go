/*
===========================================================================

latency_test.go - the shared histogram's buckets, maximum and percentiles

The transport's handler and tick-phase histograms and the store's lock
timing all read these: a sample lands in the bucket whose bound holds it,
Add reports a new maximum exactly when one is set (the store keeps its
longest commit on that report), and a percentile reads as its bucket's
bound, or the maximum in the open last bucket.

===========================================================================
*/

package latency

import (
	"testing"
	"time"
)

/*
================
TestBucketBoundsAreInclusive
================
*/
func TestBucketBoundsAreInclusive(t *testing.T) {
	for _, tc := range []struct {
		ms   float64
		want int
	}{
		{0, 0}, {1, 0}, {1.0001, 1}, {16, 4}, {16.5, 5}, {1000, len(BoundsMs) - 1}, {1000.5, Buckets - 1}, {1e9, Buckets - 1},
	} {
		if got := Bucket(tc.ms); got != tc.want {
			t.Errorf("Bucket(%v) = %d, want %d", tc.ms, got, tc.want)
		}
	}
}

/*
================
TestAddReportsOnlyANewMaximum
================
*/
func TestAddReportsOnlyANewMaximum(t *testing.T) {
	var h Live
	for _, step := range []struct {
		ms   int
		want bool
	}{
		{5, true}, {3, false}, {5, false}, {7, true}, {1, false},
	} {
		if got := h.Add(time.Duration(step.ms) * time.Millisecond); got != step.want {
			t.Errorf("Add(%d ms) reported a new maximum = %v, want %v", step.ms, got, step.want)
		}
	}
	if got := h.Snapshot().MaxMs; got != 7 {
		t.Fatalf("max %v ms, want 7", got)
	}
}

/*
================
TestPercentilesReadAsBucketBoundsOrTheMaximum
================
*/
func TestPercentilesReadAsBucketBoundsOrTheMaximum(t *testing.T) {
	var h Live
	if snap := h.Snapshot(); snap.P50Ms != 0 || snap.P99Ms != 0 || snap.MaxMs != 0 {
		t.Fatalf("empty histogram %+v, want zeros", snap)
	}
	// 98 samples of 3 ms (bucket <= 5), one of 40 ms (<= 50), one of 2 s
	// (the open last bucket).
	for range 98 {
		h.Add(3 * time.Millisecond)
	}
	h.Add(40 * time.Millisecond)
	h.Add(2 * time.Second)
	snap := h.Snapshot()
	if snap.P50Ms != 5 {
		t.Errorf("p50 %v, want the 5 ms bound", snap.P50Ms)
	}
	if snap.P99Ms != 50 {
		t.Errorf("p99 %v, want the 50 ms bound", snap.P99Ms)
	}
	if snap.P999Ms != 2000 || snap.MaxMs != 2000 {
		t.Errorf("p99.9 %v max %v, want the 2000 ms maximum from the open bucket", snap.P999Ms, snap.MaxMs)
	}
	var total uint64
	for _, n := range snap.Buckets {
		total += n
	}
	if total != 100 || len(snap.Buckets) != Buckets || len(snap.BoundsMs) != len(BoundsMs) {
		t.Fatalf("snapshot %d samples in %d buckets (%d bounds), want 100 in %d (%d)", total, len(snap.Buckets), len(snap.BoundsMs), Buckets, len(BoundsMs))
	}
}
