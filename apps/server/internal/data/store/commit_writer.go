/*
===========================================================================

commit_writer.go - the asynchronous SQLite writer behind SRO_STORE_ASYNC_COMMIT

Port-only, not native: the v1.150 client and the v1.188 server say nothing
about how the port persists its authority. With the flag off (the default)
every commit applies on the caller's goroutine under Store.mu, as before
#2802. With it on, a commit is built under the lock (commit_batch.go) and
handed to this writer, which applies the batches one at a time, in order,
on its own goroutine: a slow disk write no longer stalls the tick or any
reader waiting on Store.mu.

The durability rule (owner-approved #2802 design):

  - A door answers once its batch is queued. The batch reaches the WAL a
    few milliseconds later.
  - Batches apply strictly in sequence, one SQLite transaction each, so
    after any crash the disk holds a prefix of the batch sequence, never
    a gap (PostgreSQL's asynchronous-commit rule). A trade is one batch:
    lost whole or kept whole, never duplicated. The running server never
    reloads from disk, so no relog can read a stale row.
  - The queue holds at most maxQueuedCommits batches; at the cap the next
    commit waits for a slot, so a stalled disk degrades the store to the
    synchronous door instead of growing memory.
  - A failed batch is retried in place and blocks the batches behind it,
    keeping the prefix. The failure is reported to the store's Health at
    its next commit (commitCheckedLocked), fail-open and loud as before.
  - Anything that reads or writes the database outside a batch drains the
    queue first (Store.syncDBLocked), so it sees every queued write and
    lands after them.

The writer never takes Store.mu: a commit holding the lock may be waiting
on a queue slot, and the writer must be free to make one.

===========================================================================
*/

package store

import (
	"database/sql"
	"fmt"
	"os"
	"sync"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/latency"
)

// EnvAsyncCommit turns the asynchronous writer on ("1"). Off, the native
// default, every commit applies under the store lock. Port-only, not native.
const EnvAsyncCommit = "SRO_STORE_ASYNC_COMMIT"

const (
	// maxQueuedCommits bounds the batches queued ahead of the disk. It is
	// also the loss window's worst case at process death during a stall.
	maxQueuedCommits = 64
	// commitRetryFirst and commitRetryMax pace the retries of a failing
	// batch: quick for a transient lock, then no faster than once a second.
	commitRetryFirst = 50 * time.Millisecond
	commitRetryMax   = time.Second
	// commitStopAttempts is how many more tries a failing batch gets once
	// Close asked the writer to stop, before the queue is dropped loudly.
	commitStopAttempts = 3
)

/*
================
asyncCommitEnabled
================
*/
func asyncCommitEnabled() bool {
	return os.Getenv(EnvAsyncCommit) == "1"
}

/*
================
queuedCommit

One batch waiting for the disk, with the test seam that was set when it
was built: the writer must not read Store fields, which Store.mu owns.
================
*/
type queuedCommit struct {
	batch        *commitBatch
	beforeCommit func()
	// queuedAt is when the batch entered the queue, for the lag.
	queuedAt time.Time
}

/*
================
commitWriterFailure

The writer's report of a failing batch: how many attempts failed in a
row, the last error and when the run of failures began. Zero when the
last attempt succeeded.
================
*/
type commitWriterFailure struct {
	attempts int
	err      error
	since    time.Time
}

/*
================
commitWriter

The writer's state. mu guards everything below it; changed is signalled
whenever the queue, the applying flag or the stop request moves.
================
*/
type commitWriter struct {
	db  *sql.DB
	now func() time.Time

	mu       sync.Mutex
	changed  *sync.Cond
	queue    []queuedCommit
	applying bool
	stopping bool
	failure  commitWriterFailure
	// queued and applied count batches since start; a batch's sequence
	// number is queued at the moment it was enqueued.
	queued  uint64
	applied uint64
	// failedTotal counts every failed attempt since start; it never
	// resets, so a drain can tell a fresh failure from an old one.
	failedTotal uint64
	done        chan struct{}

	// depthMax is the deepest the queue has stood since start. lag is
	// each batch's time from queued to committed (the loss window the
	// durability rule names), slotWait each enqueue's wait for a free
	// slot (the back-pressure a full queue puts on the door holding
	// Store.mu). Recording takes no lock; the snapshot reads them bare.
	depthMax int
	lag      latency.Live
	slotWait latency.Live
}

/*
================
CommitWriterStats

The asynchronous writer's state on /transport/metrics (store_lock.
commit_writer), the design's "queue depth and hand-off lag next to
store_lock": a queue that fills, a lag that grows or a failing batch
shows here long before the cap is reached.
================
*/
type CommitWriterStats struct {
	QueueDepth    int               `json:"queue_depth"`
	QueueDepthMax int               `json:"queue_depth_max"`
	QueueCap      int               `json:"queue_cap"`
	Queued        uint64            `json:"queued"`
	Applied       uint64            `json:"applied"`
	LagMs         latency.Histogram `json:"lag_ms"`
	SlotWaitMs    latency.Histogram `json:"slot_wait_ms"`
	// FailingAttempts is the current run of failed attempts on the head
	// batch and FailingSinceMs when it began (Unix ms); both zero once
	// the head commits. FailedTotal never resets.
	FailingAttempts int    `json:"failing_attempts"`
	FailingSinceMs  int64  `json:"failing_since_ms,omitempty"`
	FailedTotal     uint64 `json:"failed_total"`
}

/*
================
startCommitWriter
================
*/
func startCommitWriter(db *sql.DB, now func() time.Time) *commitWriter {
	w := &commitWriter{db: db, now: now, done: make(chan struct{})}
	w.changed = sync.NewCond(&w.mu)
	go w.run()
	return w
}

/*
================
commitWriter::enqueue

Queues one batch and returns its sequence number. Waits while the queue
is full: the back-pressure that bounds the loss window.
================
*/
func (w *commitWriter) enqueue(batch *commitBatch, beforeCommit func()) uint64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	began := w.now()
	for len(w.queue) >= maxQueuedCommits && !w.stopping {
		w.changed.Wait()
	}
	queuedAt := w.now()
	w.slotWait.Add(queuedAt.Sub(began))
	w.queue = append(w.queue, queuedCommit{batch: batch, beforeCommit: beforeCommit, queuedAt: queuedAt})
	w.depthMax = max(w.depthMax, len(w.queue))
	w.queued++
	w.changed.Broadcast()
	return w.queued
}

/*
================
commitWriter::stats

The snapshot behind CommitWriterStats. Takes only the writer's own
mutex, never Store.mu, and never waits on the disk: the writer drops
it while it applies a batch.
================
*/
func (w *commitWriter) stats() CommitWriterStats {
	w.mu.Lock()
	out := CommitWriterStats{
		QueueDepth:      len(w.queue),
		QueueDepthMax:   w.depthMax,
		QueueCap:        maxQueuedCommits,
		Queued:          w.queued,
		Applied:         w.applied,
		FailingAttempts: w.failure.attempts,
		FailedTotal:     w.failedTotal,
	}
	if w.failure.attempts > 0 {
		out.FailingSinceMs = w.failure.since.UnixMilli()
	}
	w.mu.Unlock()
	out.LagMs = w.lag.Snapshot()
	out.SlotWaitMs = w.slotWait.Snapshot()
	return out
}

/*
================
commitWriter::drain

Waits until every queued batch is applied (or dropped by a stop), or
until the head batch fails once more after the drain began. A failing
disk must not hang the caller, which holds the store lock: the
synchronous door fails open on a dead disk (recordWriteFailureLocked),
and so does the drain, at the cost of one retry interval. An earlier
failure alone does not end the wait: the disk may have recovered, and
the next attempt then writes the queue the caller is about to read. On
a fail-open return the statement that follows runs while the queue is
still pending; on a disk that refuses writes it most likely fails too,
and Health reports the outage.
================
*/
func (w *commitWriter) drain() {
	w.mu.Lock()
	defer w.mu.Unlock()
	began := w.failedTotal
	for (len(w.queue) > 0 || w.applying) && w.failedTotal == began {
		w.changed.Wait()
	}
}

/*
================
commitWriter::waitApplied

Waits until the batch with sequence number seq is applied: the wait a
durable door takes after releasing the store lock. Like drain it ends
when the head batch fails once more after the wait began: the door then
answers fail-open, as the synchronous door does on a dead disk
(recordWriteFailureLocked), and Health reports the outage.
================
*/
func (w *commitWriter) waitApplied(seq uint64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	began := w.failedTotal
	for w.applied < seq && (len(w.queue) > 0 || w.applying) && w.failedTotal == began {
		w.changed.Wait()
	}
}

/*
================
durableTicket

What a durable door waits on once it has released Store.mu: the writer
and the sequence number of the last batch queued while the door held the
lock, which is the door's own commit or one queued after it. Doors whose
answer reaches outside the store take one (#2802 design rule 4,
PostgreSQL's per-transaction synchronous_commit): character creation,
which claims a name, and the deletion reservation. The doors that already
open their own transaction (silk grants, Beta Silk, mall purchases,
renames, the reap) commit synchronously through beginTxLocked's drain and
need none. With the flag off the ticket stays empty and wait returns at
once: the commit applied under the lock.

The pattern, so the wait runs after the unlock (defers run last first):

	var durable durableTicket
	defer durable.wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	defer durable.captureLocked(s)

================
*/
type durableTicket struct {
	writer *commitWriter
	seq    uint64
}

/*
================
durableTicket::captureLocked

Records the writer and the last queued sequence. Runs under Store.mu.
================
*/
func (t *durableTicket) captureLocked(s *Store) {
	if s.writer == nil {
		return
	}
	t.writer, t.seq = s.writer, s.queuedSeq
}

/*
================
durableTicket::wait

Waits until the captured batch is on disk. Never under Store.mu: the
writer must stay free to apply the queue.
================
*/
func (t *durableTicket) wait() {
	if t.writer == nil || t.seq == 0 {
		return
	}
	t.writer.waitApplied(t.seq)
}

/*
================
commitWriter::lastFailure
================
*/
func (w *commitWriter) lastFailure() commitWriterFailure {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.failure
}

/*
================
commitWriter::stop

Drains the queue and ends the goroutine. Close calls it before the final
checkpoint, so every queued write is on disk first. A batch that keeps
failing gets commitStopAttempts more tries and is then dropped with the
rest of the queue, loudly: Close must not hang on a dead disk.
================
*/
func (w *commitWriter) stop() {
	w.mu.Lock()
	w.stopping = true
	w.changed.Broadcast()
	w.mu.Unlock()
	<-w.done
}

/*
================
commitWriter::run

The writer goroutine: take the oldest batch, apply it until it commits,
drop it from the queue, repeat. The batch leaves the queue only after it
committed, so drain and waitApplied see it as pending while it runs.
================
*/
func (w *commitWriter) run() {
	defer close(w.done)
	for {
		w.mu.Lock()
		for len(w.queue) == 0 && !w.stopping {
			w.changed.Wait()
		}
		if len(w.queue) == 0 {
			w.mu.Unlock()
			return
		}
		next := w.queue[0]
		w.applying = true
		w.mu.Unlock()

		ok := w.applyWithRetry(next)

		w.mu.Lock()
		w.applying = false
		if ok {
			w.lag.Add(w.now().Sub(next.queuedAt))
			w.queue[0] = queuedCommit{}
			w.queue = w.queue[1:]
			w.applied++
		} else {
			// Only a stop gives up on a batch; the queue behind it goes
			// too, or the disk would hold a gap.
			log.Errorf("store: async writer stopping with %d unwritten commit(s) after %d failed attempt(s): %v",
				len(w.queue), w.failure.attempts, w.failure.err)
			w.applied += uint64(len(w.queue))
			w.queue = nil
		}
		w.changed.Broadcast()
		w.mu.Unlock()
	}
}

/*
================
commitWriter::applyWithRetry

Applies one batch, retrying in place on failure. Returns false only when
a stop was requested and the batch failed commitStopAttempts more times.
================
*/
func (w *commitWriter) applyWithRetry(next queuedCommit) bool {
	delay := commitRetryFirst
	stopTries := 0
	for {
		_, _, err := next.batch.apply(batchApply{db: w.db, now: w.now, beforeCommit: next.beforeCommit})
		w.mu.Lock()
		if err == nil {
			w.failure = commitWriterFailure{}
			w.mu.Unlock()
			return true
		}
		if w.failure.attempts == 0 {
			w.failure.since = w.now()
		}
		w.failure.attempts++
		w.failedTotal++
		w.failure.err = fmt.Errorf("async commit: %w", err)
		// A drain waiting on this batch gives up on a failing disk.
		w.changed.Broadcast()
		if w.stopping {
			stopTries++
			if stopTries >= commitStopAttempts {
				w.mu.Unlock()
				return false
			}
		}
		w.mu.Unlock()
		time.Sleep(delay)
		delay *= 2
		if delay > commitRetryMax {
			delay = commitRetryMax
		}
	}
}

/*
================
Store::syncDBLocked

The database handle for a statement that runs outside a commit batch: a
door's own transaction, a direct read or write. With the asynchronous
writer on, the queue is drained first, so the statement sees every
queued write and lands after them: read-your-writes and the batch
prefix both hold. Without the writer it is s.db. These doors are rare
and keep their synchronous cost.
================
*/
func (s *Store) syncDBLocked() *sql.DB {
	if s.writer != nil {
		s.writer.drain()
	}
	return s.db
}
