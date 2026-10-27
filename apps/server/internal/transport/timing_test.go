/*
===========================================================================

timing_test.go - request and tick phase histograms land in their buckets

===========================================================================
*/

package transport

import (
	"runtime"
	"testing"
	"time"
)

/*
================
TestTimingHistogramsBucketHandlersPhasesAndSlowHooks
================
*/
func TestTimingHistogramsBucketHandlersPhasesAndSlowHooks(t *testing.T) {
	hub := newHub(testCfg())
	hub.metrics.timing.recordHandler(0x7738, 5*time.Millisecond)
	hub.metrics.timing.recordHandler(0x7738, 120*time.Millisecond)
	hub.metrics.timing.recordHandler(0x7738, 2*time.Second)
	hub.RecordTickPhases(TickPhases{
		BeforeHooks: 2 * time.Millisecond, Divisions: 30 * time.Millisecond, Hooks: 300 * time.Millisecond,
		Total:     332 * time.Millisecond,
		SlowHooks: []SlowHook{{Name: "action.(*Runtime).TickHook.func1", Elapsed: 300 * time.Millisecond}},
	})
	m := hub.Metrics()
	move := m.HandlerMs["0x7738"]
	if !equalCounts(move.Buckets, []uint64{0, 0, 1, 0, 0, 0, 0, 0, 1, 0, 1}) || move.MaxMs != 2000 {
		t.Fatalf("handler histogram = %+v", move)
	}
	if m.TickPhaseMs["divisions"].Buckets[6] != 1 || m.TickPhaseMs["hooks"].Buckets[9] != 1 || m.TickPhaseMs["total"].MaxMs != 332 {
		t.Fatalf("tick phases = %+v", m.TickPhaseMs)
	}
	if slow := m.SlowHooks["action.(*Runtime).TickHook.func1"]; slow.Count != 1 || slow.MaxMs != 300 {
		t.Fatalf("slow hooks = %+v", m.SlowHooks)
	}
}

/*
================
TestSlowStepsRecordWhenTheyHappened

A slow step lands beside the hooks with its first and last wall time and
tick number, so a cluster right after start reads differently from a stall
that recurs.
================
*/
func TestSlowStepsRecordWhenTheyHappened(t *testing.T) {
	hub := newHub(testCfg())
	const step = "action.TickHook/AdvancePopulation"
	for range 3 {
		hub.RecordTickDuration(time.Millisecond, 100*time.Millisecond)
	}
	hub.RecordSlowStep(step, 150*time.Millisecond)
	for range 2 {
		hub.RecordTickDuration(time.Millisecond, 100*time.Millisecond)
	}
	hub.RecordSlowStep(step, 340*time.Millisecond)
	slow := hub.Metrics().SlowHooks[step]
	if slow.Count != 2 || slow.MaxMs != 340 || slow.FirstTick != 3 || slow.LastTick != 5 {
		t.Fatalf("slow step = %+v", slow)
	}
	if slow.FirstAt.IsZero() || slow.LastAt.Before(slow.FirstAt) {
		t.Fatalf("slow step times = %v .. %v", slow.FirstAt, slow.LastAt)
	}
}

/*
================
TestTickCommitsAreCounted

Only ticks that deferred or flushed count; a flush adds its rows and its
time, and a failed one counts as a failure.
================
*/
func TestTickCommitsAreCounted(t *testing.T) {
	hub := newHub(testCfg())
	hub.RecordTickPhases(TickPhases{})
	hub.RecordTickPhases(TickPhases{Commit: TickCommit{Deferred: 5, Rows: 3, Elapsed: 20 * time.Millisecond, Flushed: true}})
	hub.RecordTickPhases(TickPhases{Commit: TickCommit{Deferred: 2}})
	hub.RecordTickPhases(TickPhases{Commit: TickCommit{Deferred: 1, Rows: 7, Elapsed: 2 * time.Millisecond, Flushed: true, Failed: true}})
	m := hub.Metrics()
	want := TickCommitStats{Flushes: 2, Rows: 10, Deferred: 8, MaxRows: 7, Failures: 1}
	if m.TickCommit != want {
		t.Fatalf("tick commit = %+v, want %+v", m.TickCommit, want)
	}
	if !equalCounts(m.TickPhaseMs["commit"].Buckets, []uint64{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0}) {
		t.Fatalf("commit histogram = %+v", m.TickPhaseMs["commit"])
	}
}

/*
================
equalCounts
================
*/
func equalCounts(got, want []uint64) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

/*
================
TestTimingPercentilesReadTheirBucketBound

A handler held to a few milliseconds: 990 answers at 0.8 ms and 10 at
12 ms put p50 and p99 at the 1 ms bound and p999 in the 16 ms bucket,
read as 12 ms because a bound never reads above the largest sample. One
3 s answer then lands in the open bucket and stays visible as max.
================
*/
func TestTimingPercentilesReadTheirBucketBound(t *testing.T) {
	hub := newHub(testCfg())
	for i := 0; i < 990; i++ {
		hub.metrics.timing.recordHandler(0x72cd, 800*time.Microsecond)
	}
	for i := 0; i < 10; i++ {
		hub.metrics.timing.recordHandler(0x72cd, 12*time.Millisecond)
	}
	h := hub.Metrics().HandlerMs["0x72CD"]
	if h.P50Ms != 1 || h.P99Ms != 1 || h.P999Ms != 12 {
		t.Fatalf("percentiles = p50 %v p99 %v p999 %v, want 1 1 12", h.P50Ms, h.P99Ms, h.P999Ms)
	}
	if len(h.BoundsMs) != len(h.Buckets)-1 {
		t.Fatalf("bounds %v do not describe buckets %v", h.BoundsMs, h.Buckets)
	}
	hub.metrics.timing.recordHandler(0x72cd, 3*time.Second)
	h = hub.Metrics().HandlerMs["0x72CD"]
	if h.MaxMs != 3000 || h.Buckets[len(h.Buckets)-1] != 1 {
		t.Fatalf("slow answer = %+v", h)
	}
}

/*
================
TestMetricsServeTheRuntimeStats

The runtime block reads live values: a running process has goroutines, a
live heap and a heap goal, and after a forced collection at least one GC
cycle with a measurable pause.
================
*/
func TestMetricsServeTheRuntimeStats(t *testing.T) {
	runtime.GC()
	stats := newHub(testCfg()).Metrics().Runtime
	if stats.Goroutines == 0 || stats.HeapLiveBytes == 0 || stats.HeapGoalBytes == 0 || stats.GCCycles == 0 {
		t.Fatalf("runtime stats = %+v", stats)
	}
	if stats.GCPauseMaxMs <= 0 || stats.GCPauseP99Ms <= 0 || stats.GCPauseP99Ms > stats.GCPauseMaxMs {
		t.Fatalf("gc pauses = p99 %v max %v", stats.GCPauseP99Ms, stats.GCPauseMaxMs)
	}
}

/*
================
TestPublicationHoldsRecordMaxByLabel

Each slow publication-gate hold lands under its holder's label with a
count and the longest hold, so a tick step that waited on the gate can be
matched to the work that kept it (#1820 E).
================
*/
func TestPublicationHoldsRecordMaxByLabel(t *testing.T) {
	hub := newHub(testCfg())
	hub.RecordPublicationHold("enterWorld", 300*time.Millisecond)
	hub.RecordPublicationHold("enterWorld", 150*time.Millisecond)
	hub.RecordPublicationHold("requestOp", 200*time.Millisecond)

	holds := hub.Metrics().PublicationHolds
	if got := holds["enterWorld"]; got.Count != 2 || got.MaxMs != 300 {
		t.Fatalf("enterWorld holds = %+v, want count 2, max 300 ms", got)
	}
	if got := holds["requestOp"]; got.Count != 1 || got.MaxMs != 200 {
		t.Fatalf("requestOp holds = %+v, want count 1, max 200 ms", got)
	}
}
