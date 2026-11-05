/*
===========================================================================

report_test.go - the tick window math, against the #2003 reader's own
fixtures

scripts/analysis/tick_acceptance.py pins its arithmetic with a
self-test over synthetic snapshots and one real served window. Those
fixtures are copied here: the Go window must reproduce the Python
reader's percentiles bucket for bucket, or the staircase report and
an acceptance re-measure of the same run would disagree.
===========================================================================
*/

package main

import (
	"encoding/json"
	"testing"
)

/*
================
TestPercentileBoundedBucketKeepsBound

A bounded bucket never takes the spike max: p99 in (50, 100] reads
100 even when the journal's worst is quieter (the reader's rule).
================
*/
func TestPercentileBoundedBucketKeepsBound(t *testing.T) {
	got := percentileFromBuckets([]float64{10, 25, 50, 100}, []uint64{900, 50, 30, 20, 0}, 0.99, 30)
	if got != 100 {
		t.Fatalf("percentile = %v, want 100", got)
	}
	if got := percentileFromBuckets([]float64{10, 25, 50, 100}, nil, 0.99, 700); got != 0 {
		t.Fatalf("percentile of no samples = %v, want 0", got)
	}
}

/*
================
TestComputeTickWindowSynthetic

The reader's synthetic window: buckets [1800, 140, 40, 16, 4] over
2,000 ticks. p50 and p90 land in the first bucket, p99 in the third,
p99.9 in the open bucket, read as the window's spike max. (The Python
self-test passes its wall-clock-filtered bursts explicitly; the Go
window binds the mirror's bursts by tick_count, which for this boot's
bursts is the same set.)
================
*/
func TestComputeTickWindowSynthetic(t *testing.T) {
	before := metricsSnapshot{TickCount: 1000, TickMeanMs: 40, TickOverruns: 10, LiveSessions: 3}
	before.TickMsHist.BoundsMs = []float64{10, 25, 50, 100}
	before.TickMsHist.Counts = []uint64{900, 80, 15, 4, 1}
	before.SlowHooks = map[string]slowCount{"advanceReturnScrolls": {Count: 5, MaxMs: 300}}

	after := metricsSnapshot{TickCount: 3000, TickMeanMs: 50, TickOverruns: 12, LiveSessions: 5}
	after.TickMsHist.BoundsMs = []float64{10, 25, 50, 100}
	after.TickMsHist.Counts = []uint64{2700, 220, 55, 20, 5}
	after.SlowHooks = map[string]slowCount{
		"advanceReturnScrolls": {Count: 5, MaxMs: 300},
		"advancePets":          {Count: 2, MaxMs: 150},
	}
	after.SlowDivisions = map[string]slowCount{"7": {Count: 1, MaxMs: 120}}
	after.TickSpikes = []spikeRow{
		{TickCountEnd: 900, MaxMs: 900},  // before the window: excluded
		{TickCountEnd: 1500, MaxMs: 700}, // in window
		{TickCountEnd: 2500, MaxMs: 800}, // in window, the worst
		{TickCountEnd: 3500, MaxMs: 950}, // after the window: excluded
	}
	after.Runtime.AllocatedBytes = 5 << 30
	after.Runtime.HeapLiveBytes = 900 << 20

	w := computeTickWindow(before, after)
	if w.Error != "" {
		t.Fatalf("window error: %s", w.Error)
	}
	if w.Ticks != 2000 || w.Samples != 2000 {
		t.Fatalf("ticks/samples = %d/%d, want 2000/2000", w.Ticks, w.Samples)
	}
	if w.MeanMs != 55 {
		t.Fatalf("mean = %v, want 55 ((50*3000-40*1000)/2000)", w.MeanMs)
	}
	if w.P50Ms != 10 || w.P90Ms != 10 || w.P99Ms != 50 {
		t.Fatalf("p50/p90/p99 = %v/%v/%v, want 10/10/50", w.P50Ms, w.P90Ms, w.P99Ms)
	}
	if w.P999Ms != 800 || w.SpikeMaxMs != 800 {
		t.Fatalf("p999/spikeMax = %v/%v, want 800/800 (open bucket reads the window spike max)", w.P999Ms, w.SpikeMaxMs)
	}
	if w.Overruns != 2 || w.OverrunsPer10k != 10 {
		t.Fatalf("overruns = %d (%.2f per 10k), want 2 (10)", w.Overruns, w.OverrunsPer10k)
	}
	if len(w.SlowHooks) != 1 || w.SlowHooks[0].Name != "advancePets" || w.SlowHooks[0].Hits != 2 {
		t.Fatalf("slow hooks = %+v, want advancePets with 2 hits only", w.SlowHooks)
	}
	if len(w.SlowDivisions) != 1 || w.SlowDivisions[0].Hits != 1 {
		t.Fatalf("slow divisions = %+v, want one hit", w.SlowDivisions)
	}
	if w.AllocatedBytes != 5<<30 || w.HeapLiveBytes != 900<<20 {
		t.Fatalf("allocation read = %d/%d, want %d/%d", w.AllocatedBytes, w.HeapLiveBytes, 5<<30, 900<<20)
	}
	if w.LiveSessionsBefore != 3 || w.LiveSessionsAfter != 5 {
		t.Fatalf("live sessions = %d -> %d, want 3 -> 5", w.LiveSessionsBefore, w.LiveSessionsAfter)
	}
}

/*
================
TestComputeTickWindowServedFixture

The reader's real served window (the 2026-10-28 #2033 profile): the
window math must reproduce the server's own printed percentiles from
the counts alone - p50 40, p90 70, p99 110, p99.9 180 over 354,166
samples. The JSON literal also pins the snapshot's field names
("counts" on the tick histogram) against the served shape.
================
*/
func TestComputeTickWindowServedFixture(t *testing.T) {
	bounds := []float64{2, 5, 10, 12, 14, 16, 18, 20, 25, 30, 40, 50, 60, 70, 80, 90,
		100, 110, 120, 140, 160, 180, 200, 250, 300, 400, 500, 750, 1000, 2000}
	counts := []uint64{0, 0, 0, 3, 58, 551, 2389, 6031, 41582, 57781, 95971, 66978,
		39266, 21472, 10737, 5073, 2338, 1159, 791, 981, 566, 239,
		85, 46, 13, 15, 13, 28, 0, 0, 0}
	zeros := make([]uint64, len(counts))

	before := metricsSnapshot{TickCount: 1000}
	before.TickMsHist.BoundsMs = bounds
	before.TickMsHist.Counts = zeros
	after := metricsSnapshot{TickCount: 1000 + 354166}
	after.TickMsHist.BoundsMs = bounds
	after.TickMsHist.Counts = counts

	w := computeTickWindow(before, after)
	if w.Error != "" {
		t.Fatalf("window error: %s", w.Error)
	}
	if w.Samples != 354166 {
		t.Fatalf("samples = %d, want 354166", w.Samples)
	}
	if w.P50Ms != 40 || w.P90Ms != 70 || w.P99Ms != 110 || w.P999Ms != 180 {
		t.Fatalf("p50/p90/p99/p999 = %v/%v/%v/%v, want 40/70/110/180", w.P50Ms, w.P90Ms, w.P99Ms, w.P999Ms)
	}

	// The same window as served JSON: field names are the contract.
	raw := []byte(`{"tick_count": 355166,
		"tick_ms_histogram": {"bounds_ms": [10, 25, 50, 100], "counts": [2700, 220, 55, 20, 5]},
		"tick_phase_ms": {"monster": {"bounds_ms": [1, 5, 10], "buckets": [100, 30, 4, 1], "max_ms": 42}},
		"slow_hooks": {"advancePets": {"count": 2, "max_ms": 150}},
		"tick_spikes": [{"tick_count_end": 2000, "max_ms": 33.5}],
		"runtime": {"allocated_bytes": 12345, "heap_live_bytes": 678, "gc_cycles": 9, "goroutines": 55}}`)
	var snap metricsSnapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		t.Fatalf("decode served metrics: %v", err)
	}
	if snap.TickCount != 355166 || snap.TickMsHist.Counts[2] != 55 || snap.Runtime.Goroutines != 55 {
		t.Fatalf("decoded snapshot lost fields: %+v", snap)
	}
	if snap.TickPhaseMs["monster"].Buckets[1] != 30 || snap.TickSpikes[0].MaxMs != 33.5 {
		t.Fatalf("decoded phase/spike fields wrong: %+v", snap)
	}
}

/*
================
TestComputeTickWindowRefusals

A restarted GameWorld and an empty histogram are reported as errors,
never as a green window of zeros.
================
*/
func TestComputeTickWindowRefusals(t *testing.T) {
	before := metricsSnapshot{TickCount: 3000}
	before.TickMsHist.BoundsMs = []float64{10}
	before.TickMsHist.Counts = []uint64{5, 1}
	restarted := metricsSnapshot{TickCount: 500}
	restarted.TickMsHist.BoundsMs = []float64{10}
	restarted.TickMsHist.Counts = []uint64{5, 1}
	if w := computeTickWindow(before, restarted); w.Error == "" {
		t.Fatal("restarted window reported without error")
	}
	empty := metricsSnapshot{TickCount: 4000}
	empty.TickMsHist.BoundsMs = []float64{10}
	empty.TickMsHist.Counts = []uint64{5, 1}
	if w := computeTickWindow(before, empty); w.Error == "" {
		t.Fatal("sample-less window reported without error")
	}
}

/*
================
TestPhaseWindows

Per-phase window percentiles from bucket deltas; a phase with no
window samples is omitted, and rows sort worst-p99 first.
================
*/
func TestPhaseWindows(t *testing.T) {
	before := map[string]phaseHistogram{
		"prepare": {BoundsMs: []float64{1, 5, 10}, Buckets: []uint64{10, 2, 0, 0}},
		"quiet":   {BoundsMs: []float64{1, 5, 10}, Buckets: []uint64{3, 0, 0, 0}},
	}
	after := map[string]phaseHistogram{
		"prepare": {BoundsMs: []float64{1, 5, 10}, Buckets: []uint64{110, 32, 4, 1}, MaxMs: 42},
		"quiet":   {BoundsMs: []float64{1, 5, 10}, Buckets: []uint64{3, 0, 0, 0}},
		"commit":  {BoundsMs: []float64{1, 5, 10}, Buckets: []uint64{50, 40, 9, 1}, MaxMs: 12},
	}
	rows := phaseWindows(before, after)
	if len(rows) != 2 {
		t.Fatalf("phase rows = %+v, want prepare and commit only", rows)
	}
	// commit's p99: deltas [50, 40, 9, 1], rank 99 -> bucket 2 -> 10.
	// prepare's: deltas [100, 30, 4, 1], rank 134 -> bucket 2 -> 10.
	// Equal p99s fall back to name order.
	if rows[0].Name != "commit" || rows[1].Name != "prepare" {
		t.Fatalf("phase order = %s, %s; want commit, prepare", rows[0].Name, rows[1].Name)
	}
	if rows[0].Samples != 100 || rows[0].P50Ms != 1 || rows[0].P99Ms != 10 {
		t.Fatalf("commit row = %+v, want 100 samples, p50 1, p99 10", rows[0])
	}
	if rows[1].MaxMsSinceBoot != 42 {
		t.Fatalf("prepare max = %v, want the since-boot 42 under its own name", rows[1].MaxMsSinceBoot)
	}
}
