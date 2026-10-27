/*
===========================================================================

lock_timing_test.go - a slow commit is seen by the reader that waits on it

The real store, with its commit's sqlite phase held by the test seam: a
reader entering the read door during that commit must wait for it, and
the lock timing must show the wait, the hold and the phase that took it,
under the door's label.

===========================================================================
*/

package store

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
)

const (
	lockTimingSlowCommit = 150 * time.Millisecond
	lockTimingSeen       = 100 // ms: what the histograms must report at least
	lockTimingSlowLabel  = "slow-door"
)

/*
================
TestSlowCommitShowsInAConcurrentReadersWait
================
*/
func TestSlowCommitShowsInAConcurrentReadersWait(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := &enterworld.Character{Name: "timed"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	inCommit := make(chan struct{})
	s.beforeSQLiteCommit = func() {
		close(inCommit)
		time.Sleep(lockTimingSlowCommit)
	}
	written := make(chan struct{})
	go func() {
		defer close(written)
		s.MutateCharacter(c, lockTimingSlowLabel, func() {})
	}()

	<-inCommit
	start := time.Now()
	s.ReadState(func() {})
	waited := time.Since(start)
	<-written
	s.beforeSQLiteCommit = nil

	if waited < lockTimingSeen*time.Millisecond {
		t.Fatalf("the reader waited %v; the slow commit did not hold it", waited)
	}
	timing := s.LockTiming()
	for name, got := range map[string]float64{
		"read wait":    timing.ReadWaitMs.MaxMs,
		"write hold":   timing.WriteHoldMs.MaxMs,
		"sqlite phase": timing.CommitMs["sqlite"].MaxMs,
		"commit total": timing.CommitMs["total"].MaxMs,
	} {
		if got < lockTimingSeen {
			t.Errorf("%s max %.1f ms, want at least %d ms", name, got, lockTimingSeen)
		}
	}
	longest := timing.LongestCommit
	if longest == nil || longest.Label != lockTimingSlowLabel || longest.PhaseMs[commitPhaseSQLite] < lockTimingSeen {
		t.Fatalf("longest commit %+v, want %q with its sqlite phase", longest, lockTimingSlowLabel)
	}
}

/*
================
TestLockTimingRecordsWithoutAllocating

The read door is the hot path: timing it must not allocate.
================
*/
func TestLockTimingRecordsWithoutAllocating(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	read := func() {}
	if allocs := testing.AllocsPerRun(1000, func() { s.ReadState(read) }); allocs != 0 {
		t.Fatalf("a timed read allocates %.1f times", allocs)
	}
}
