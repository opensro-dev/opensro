/*
===========================================================================

lock_timing.go - how long the authority lock is waited on and held (#1820)

Every store door runs under one RWMutex, and a commit runs its whole
SQLite transaction while holding it for writing. A tick step that reads a
character through the read door therefore waits for any commit another
goroutine is running. These histograms show how long readers wait, how
long writers wait and hold, and where a commit's hold goes:

	encode       the JSON encoding of every dirty character row
	statements   the transaction's statements (rows, mailboxes, guilds...)
	sqlite       tx.Commit: the WAL append and, when SQLite's automatic
	             checkpoint runs inside it, the checkpoint and its sync
	total        the whole commit attempt

SQLite runs its automatic checkpoint inside the commit call, so the sync
is not separable from the WAL append without taking over checkpointing;
a commit whose sqlite phase is far above its peers is a checkpointing
one. Metrics only: nothing here changes what the store does. Recording is
atomic adds (platform/latency); the longest commit's label and phases are
kept only when it sets a new maximum.

===========================================================================
*/

package store

import (
	"sync"
	"sync/atomic"
	"time"

	"opensro.online/server/internal/platform/latency"
)

/*
================
timedRWMutex

The store's lock: a sync.RWMutex that records each reader's wait and each
writer's wait and hold. heldSince is written by the writer that holds the
lock, so it needs no lock of its own.
================
*/
type timedRWMutex struct {
	mu        sync.RWMutex
	heldSince time.Time
	readWait  latency.Live
	writeWait latency.Live
	writeHold latency.Live
}

/*
================
timedRWMutex.Lock
================
*/
func (m *timedRWMutex) Lock() {
	start := time.Now()
	m.mu.Lock()
	m.heldSince = time.Now()
	m.writeWait.Add(m.heldSince.Sub(start))
}

/*
================
timedRWMutex.Unlock
================
*/
func (m *timedRWMutex) Unlock() {
	held := time.Since(m.heldSince)
	m.mu.Unlock()
	m.writeHold.Add(held)
}

/*
================
timedRWMutex.RLock
================
*/
func (m *timedRWMutex) RLock() {
	start := time.Now()
	m.mu.RLock()
	m.readWait.Add(time.Since(start))
}

/*
================
timedRWMutex.RUnlock
================
*/
func (m *timedRWMutex) RUnlock() {
	m.mu.RUnlock()
}

// The commit phases, in the order they run; total covers the attempt.
const (
	commitPhaseEncode = iota
	commitPhaseStatements
	commitPhaseSQLite
	commitPhaseTotal
	commitPhases
)

var commitPhaseNames = [commitPhases]string{"encode", "statements", "sqlite", "total"}

/*
================
commitTiming

One commit attempt's phase durations, and the recorded histograms with
the longest attempt's label and phases.
================
*/
type commitTiming struct {
	phases  [commitPhases]latency.Live
	longest atomic.Pointer[LongestCommit]
}

/*
================
commitPhaseDurations
================
*/
type commitPhaseDurations [commitPhases]time.Duration

/*
================
commitTiming.record

Records one attempt. Only an attempt that sets a new total maximum
allocates, to keep what it was.
================
*/
func (t *commitTiming) record(label string, d commitPhaseDurations) {
	for i := range d {
		if t.phases[i].Add(d[i]) && i == commitPhaseTotal {
			longest := &LongestCommit{Label: label}
			for p := range d {
				longest.PhaseMs[p] = float64(d[p]) / float64(time.Millisecond)
			}
			t.longest.Store(longest)
		}
	}
}

/*
================
LongestCommit

The commit attempt with the largest total since start: its door label
and each phase's milliseconds, in commitPhaseNames order.
================
*/
type LongestCommit struct {
	Label   string                `json:"label"`
	PhaseMs [commitPhases]float64 `json:"phase_ms"`
}

/*
================
LockTiming

The snapshot served on /transport/metrics as store_lock.
================
*/
type LockTiming struct {
	ReadWaitMs    latency.Histogram            `json:"read_wait_ms"`
	WriteWaitMs   latency.Histogram            `json:"write_wait_ms"`
	WriteHoldMs   latency.Histogram            `json:"write_hold_ms"`
	CommitMs      map[string]latency.Histogram `json:"commit_ms"`
	LongestCommit *LongestCommit               `json:"longest_commit,omitempty"`
}

/*
================
LockTiming

Reads the histograms without taking the store lock, so a metrics read
never waits behind the commit it is measuring.
================
*/
func (s *Store) LockTiming() LockTiming {
	out := LockTiming{
		ReadWaitMs:    s.mu.readWait.Snapshot(),
		WriteWaitMs:   s.mu.writeWait.Snapshot(),
		WriteHoldMs:   s.mu.writeHold.Snapshot(),
		CommitMs:      make(map[string]latency.Histogram, commitPhases),
		LongestCommit: s.commits.longest.Load(),
	}
	for i, name := range commitPhaseNames {
		out.CommitMs[name] = s.commits.phases[i].Snapshot()
	}
	return out
}
