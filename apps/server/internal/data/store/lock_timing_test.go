/*
===========================================================================

lock_timing_test.go - a slow commit is seen by the reader that waits on it

The real store, with its commit's sqlite phase held by the test seam and
the lock's clock injected: a reader entering the read door during that
commit must wait for it, and the lock timing must show the wait, the hold
and the phase that took it, under the door's label.

===========================================================================
*/

package store

import (
	"sync"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/wait"
)

const (
	lockTimingSlowCommit    = 150 * time.Millisecond
	lockTimingSeen          = 100 // ms: what the histograms must report at least
	lockTimingSlowLabel     = "slow-door"
	lockTimingReaderBlocked = 5 * time.Second
)

/*
================
TestSlowCommitShowsInAConcurrentReadersWait

The lock's clock is injected, so nothing sleeps: the commit's sqlite phase
blocks until the reader is provably waiting on the lock (the
readers_waiting gauge), the clock then moves lockTimingSlowCommit, and the
commit finishes.
================
*/
func TestSlowCommitShowsInAConcurrentReadersWait(t *testing.T) {
	// Serial, like its allocation sibling below: the assertion names the
	// longest recorded commit, and the real-clock setup commits
	// (CreateCharacter, the Open boot) sit in the same record. Under
	// parallel CPU contention a setup commit's wall time can pass the
	// injected clock's 150 ms slow door and get named instead (opensro3
	// review on #2794, reproduced 1-in-3 on a loaded box). Nothing here
	// needs a neighbour: the test builds its own store and goroutines.
	// Serial alone still leaves this test's own setup commits in the
	// record on a loaded box, so the commit record is cleared below,
	// once setup is done and before the timed writer starts: the timed
	// window then holds only the commit this test injects. The blocking
	// it pins is the synchronous door's; the asynchronous twin is in
	// commit_writer_test.go.
	s := openTestSync(t, t.TempDir(), newTestClock())
	c := &enterworld.Character{Name: "timed"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	lockClock := newTestClock()
	s.mu.clock = lockClock.Now
	s.commits = commitTiming{}
	inCommit, release := make(chan struct{}), make(chan struct{})
	var released sync.Once
	releaseCommit := func() { released.Do(func() { close(release) }) }
	// A failed assertion must not leave the writer parked holding the
	// lock: the store's Close cleanup would wait on it forever.
	t.Cleanup(releaseCommit)
	s.beforeSQLiteCommit = func() {
		close(inCommit)
		<-release
	}
	written, read := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(written)
		s.MutateCharacter(c, lockTimingSlowLabel, func() {})
	}()

	<-inCommit
	go func() {
		defer close(read)
		s.ReadState(func() {})
	}()
	wait.Eventually(t, lockTimingReaderBlocked, "the reader blocks on the committing writer", func() bool {
		return s.LockTiming().ReadersWaiting == 1
	})
	lockClock.Advance(lockTimingSlowCommit)
	releaseCommit()
	<-written
	<-read
	s.beforeSQLiteCommit = nil

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
	if timing.ReadersWaiting != 0 {
		t.Fatalf("readers waiting %d after the commit, want 0", timing.ReadersWaiting)
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
