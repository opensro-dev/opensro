/*
===========================================================================

tick_commit.go - the tick's one batched authority commit

The owner chose one batched commit per tick (2026-10-13). The ticker opens
the authority's batch before the tick's first phase and flushes it after
its last hook, so every character row a tick-only path changed lands in
one transaction (store tickbatch.go). The flush runs even when the tick is
cancelled or panics: the defer sits inside the tick's recover boundary.

===========================================================================
*/

package simulation

import "time"

/*
================
TickCommit

The authority's batch, as the composition root wires it. The zero value
batches nothing: every door commits at once, as before.
================
*/
type TickCommit struct {
	Begin func()
	Flush func() TickCommitStats
}

/*
================
TickCommitStats

One tick's flush: the deferred door calls it covered, whether a
transaction ran (an immediate commit may already have written every
deferred row), the character rows it wrote, how long it took and whether
it failed.
================
*/
type TickCommitStats struct {
	Deferred int
	Rows     int
	Elapsed  time.Duration
	Flushed  bool
	Failed   bool
}

/*
================
Ticker.beginCommit
================
*/
func (t *Ticker) beginCommit() {
	if t.Commit.Begin != nil {
		t.Commit.Begin()
	}
}

/*
================
Ticker.flushCommit

Kept on the tick clock for this tick's TickTiming.
================
*/
func (t *Ticker) flushCommit() {
	if t.Commit.Flush != nil {
		t.clock.commit = t.Commit.Flush()
	}
}
