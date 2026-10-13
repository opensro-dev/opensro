/*
===========================================================================

tickbatch.go - the simulation tick's one batched commit

The owner chose one batched commit per tick (2026-10-13). Character
updates made by tick-only paths go through the deferred doors below. While
the ticker holds a batch open, those doors mark their rows dirty and
return, and the tick's end commits every row in one transaction. A
deferred row's worst-case loss is the current tick.

The disk stays one consistent image of memory, because:

  - Every commit, immediate or batched, writes the whole dirty set, so an
    immediate door during the tick also commits the rows deferred before
    it, and a later in-tick update to a row wins: the commit writes the
    live record as it stands.
  - Paths that open their own transaction (storage, mall, silk, rename,
    deletion, fortress, guild war, unique kills, ownership transfer)
    commit the deferred rows first, so their transaction never lands a
    row whose deferred partner is still in memory.
  - A failed commit keeps the dirty set (the fail-open-loud rule in
    persistence.go). The next flush or immediate commit retries it, and
    health and readiness carry the failure meanwhile.

Doors not converted keep their immediate commit: everything a handler
answers is committed before the answer. A deferred door called with no
batch open commits at once.

===========================================================================
*/
package store

import (
	"database/sql"
	"time"

	"opensro.online/server/internal/domain"
)

/*
================
tickBatch

open is set between BeginTickBatch and FlushTickBatch. pending says a
deferred door left rows (or a ground revision) uncommitted; deferred
counts those door calls since the last flush.
================
*/
type tickBatch struct {
	open     bool
	pending  bool
	deferred int
}

/*
================
TickBatchStats

One flush, for the tick's metrics: the deferred door calls it covered,
whether a transaction ran, the character rows it wrote, how long it took
and whether it failed.
================
*/
type TickBatchStats struct {
	Deferred int
	Rows     int
	Duration time.Duration
	Flushed  bool
	Err      error
}

/*
================
BeginTickBatch
================
*/
func (s *Store) BeginTickBatch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batch.open = true
}

/*
================
FlushTickBatch

Closes the batch and commits what its deferred doors left. When nothing
was deferred, or an immediate commit already wrote it, no transaction
runs.
================
*/
func (s *Store) FlushTickBatch() TickBatchStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batch.open = false
	stats := TickBatchStats{Deferred: s.batch.deferred}
	s.batch.deferred = 0
	if !s.batch.pending {
		return stats
	}
	stats.Rows = len(s.changes.characters)
	stats.Flushed = true
	start := time.Now()
	stats.Err = s.commitCheckedLocked("tick-batch")
	stats.Duration = time.Since(start)
	return stats
}

/*
================
finishDoorLocked

The end of a character door: commit now, or leave the rows to the open
batch when the door is a deferred one.
================
*/
func (s *Store) finishDoorLocked(label string, deferred bool) {
	if deferred && s.batch.open {
		s.batch.pending = true
		s.batch.deferred++
		return
	}
	s.commitLocked(label)
}

/*
================
flushDeferredLocked

Commits the rows deferred doors left, before a path that is about to
write outside the dirty set.
================
*/
func (s *Store) flushDeferredLocked() error {
	if !s.batch.pending {
		return nil
	}
	return s.commitCheckedLocked("tick-batch-early")
}

/*
================
beginTxLocked

A separate transaction path's own transaction, opened only after the
deferred rows are on disk. A failed flush refuses the path: its
transaction would land rows whose deferred partners are not written.
================
*/
func (s *Store) beginTxLocked() (*sql.Tx, error) {
	if err := s.flushDeferredLocked(); err != nil {
		return nil, err
	}
	return s.db.Begin()
}

//============================================================================

/*
================
MutateCharacterDeferred
================
*/
func (s *Store) MutateCharacterDeferred(c *domain.Character, label string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutateCharacterLocked(c, label, fn, true)
}

/*
================
UpdateCharacterDeferred
================
*/
func (s *Store) UpdateCharacterDeferred(c *domain.Character, label string, update func() bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateCharacterLocked(c, label, update, true)
}

/*
================
MutateCharactersDeferred
================
*/
func (s *Store) MutateCharactersDeferred(cs []*domain.Character, label string, fn func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.mutateCharactersLocked(cs, label, fn, true)
}

/*
================
UpdateCharactersDeferred
================
*/
func (s *Store) UpdateCharactersDeferred(cs []*domain.Character, label string, update func() bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.updateCharactersLocked(cs, label, update, true)
}
