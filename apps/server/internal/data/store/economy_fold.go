/*
===========================================================================

economy_fold.go - the old ledger rows fold into the buckets and go
(#3187)

The rows history already holds (26M on production, 99.5% of them the
two bulk reasons) cannot be deleted until their sums live in the
hourly buckets, and cannot be folded under the store's lock 26M times.
So the fold reads: a separate read connection (the SnapshotTo posture -
a WAL reader never blocks a door) runs the grouping aggregates over
one bounded seq window of the unmarked rows past the recorded
watermark (a marked row was summed by its own commit; an unmarked
one - history's, or an older binary's from a rollback window - has
not been), and one small writer transaction per window upserts its
buckets together with the watermark. Deletes
are then plain range deletes below the watermark, in paced batches
whose lock hold stays under economyFoldMaxHoldMs: each batch is one
DELETE ... LIMIT inside the store's write lock, measured by the same
clock the lock records, and the pass waits out any window where the
tick's own holds are already at the bound. When no bulk rows remain
above or below the watermark, the pass drops the now-empty at_ms
index and the shrink at the next boot (vacuumIfFreelistDominates)
reclaims the pages.

The lane is store-owned like the WAL checkpointer: started by
adoptDB, stopped by Close before the store lock is taken (its
batches hold that lock), idle at an hourly probe when there is
nothing to fold.

===========================================================================
*/
package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/latency"
)

// Meta keys of the fold's watermarks (the meta table, db.go).
const (
	metaKeyEconomyFoldSeq   = "economyFoldSeq"
	metaKeyEconomyDeleteSeq = "economyDeleteSeq"
	metaKeyEconomyFoldDone  = "economyFoldDone"
)

const (
	// economyFoldFirstDelay: the first pass runs minutes after boot,
	// never on the boot path.
	economyFoldFirstDelay = time.Minute
	// economyFoldInterval: between passes once one has run.
	economyFoldInterval = time.Hour
	// economyFoldDeleteBatchRows bounds one delete batch's rows; the
	// batch is sized so its measured hold stays under the bound on
	// the production copy (#3187: 10 ms, reported on the issue).
	economyFoldDeleteBatchRows = 1000
	// A fold window's write hold grows with the window's distinct
	// buckets, so the lane folds a bounded seq window at a time and
	// adapts the window by the measured holds (foldOneWindow):
	// economyFoldWindowRows is where a lane starts, between the
	// floor and the ceiling.
	economyFoldWindowRows    = 4096
	economyFoldWindowMinRows = 256
	economyFoldWindowMaxRows = 65536
	// economyFoldMaxHoldMs is the lock-hold bound a fold or delete
	// batch must stay under, and the tick hold at which the pass
	// waits instead of batching.
	economyFoldMaxHoldMs = 10.0
	// economyFoldPace is the sleep between delete batches, so doors
	// always win the lock back between two batches.
	economyFoldPace = 250 * time.Millisecond
	// economyFoldBusyShare: the store is busy when more than one in
	// this many of its recent write holds reached the bound.
	// economyFoldBusyPaces: how many paces the deletes wait for a
	// quieter window before the next batch runs regardless.
	economyFoldBusyShare = 2
	economyFoldBusyPaces = 8
	// economyVacuumFreelistShare: the boot VACUUM of
	// vacuumIfFreelistDominates fires when the freelist is more than
	// this share of the file's pages.
	economyVacuumFreelistShare = 2
)

/*
================
economyFoldStats

The lane's report, served in the store lock snapshot (lock_timing.go):
the watermarks, the batches run and their measured holds.
================
*/
type economyFoldStats struct {
	FoldSeq   int64
	DeleteSeq int64
	Done      bool
	Batches   int64
	HoldMs    latency.Histogram
}

/*
================
economyFoldLane

The fold/delete lane: its loop, its measured write holds and its
watermarks as last applied.
================
*/
type economyFoldLane struct {
	store   *Store
	done    chan struct{}
	stopped chan struct{}

	// windowRows is the current fold window (foldOneWindow), moved
	// by adaptWindow between passes of this lane's single loop.
	windowRows int64
	// holdsSeen is the store's write-hold histogram as the lane last
	// read it (storeBusy): the next read judges only the holds since.
	holdsSeen latency.Histogram
	hold      latency.Live
	batches   atomic.Int64
	foldSeq   atomic.Int64
	delSeq    atomic.Int64
	fin       atomic.Bool
}

/*
================
startEconomyFold

Starts the lane for an adopted store (adoptDB).
================
*/
func startEconomyFold(s *Store) *economyFoldLane {
	lane := &economyFoldLane{store: s, done: make(chan struct{}), stopped: make(chan struct{})}
	go lane.run()
	return lane
}

/*
================
stop

Ends the lane and waits for its loop: called before Close takes the
store lock, since a batch holds that lock briefly.
================
*/
func (l *economyFoldLane) stop() {
	close(l.done)
	<-l.stopped
}

/*
================
run

The lane's loop: a first pass after economyFoldFirstDelay, then one
an hour. A pass with nothing to do is one watermark probe.
================
*/
func (l *economyFoldLane) run() {
	defer close(l.stopped)
	delay := economyFoldFirstDelay
	for {
		timer := time.NewTimer(delay)
		select {
		case <-l.done:
			timer.Stop()
			return
		case <-timer.C:
		}
		if err := l.pass(); err != nil {
			log.Warnf("economy fold: %v", err)
		}
		delay = economyFoldInterval
	}
}

/*
================
stats

The lane's report for the lock snapshot.
================
*/
func (l *economyFoldLane) stats() economyFoldStats {
	return economyFoldStats{
		FoldSeq:   l.foldSeq.Load(),
		DeleteSeq: l.delSeq.Load(),
		Done:      l.fin.Load(),
		Batches:   l.batches.Load(),
		HoldMs:    l.hold.Snapshot(),
	}
}

/*
================
watermarks

The recorded fold and delete watermarks (0 before the first pass).
================
*/
func (l *economyFoldLane) watermarks() (foldSeq, deleteSeq int64, err error) {
	s := l.store
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return 0, 0, fmt.Errorf("no database open")
	}
	if foldSeq, err = readMetaInt64(s.db, metaKeyEconomyFoldSeq, 0); err != nil {
		return 0, 0, err
	}
	if deleteSeq, err = readMetaInt64(s.db, metaKeyEconomyDeleteSeq, 0); err != nil {
		return 0, 0, err
	}
	return foldSeq, deleteSeq, nil
}

/*
================
pass

One pass: fold the bulk rows past the watermark, delete the folded
rows in paced batches, and finish (drop the empty index, record
fold-complete) when no bulk rows remain anywhere.
================
*/
func (l *economyFoldLane) pass() error {
	select {
	case <-l.done:
		return nil
	default:
	}
	folded, err := l.foldNewRows()
	if err != nil {
		return err
	}
	if folded {
		// The fold just landed: let the tick have the lock back
		// before the deletes start taking it in batches.
		l.pace()
	}
	for {
		n, err := l.deleteBatch()
		if err != nil {
			return err
		}
		if n == 0 {
			break
		}
		if !l.pace() || !l.waitForQuiet() {
			return nil
		}
	}
	return l.finishIfDrained()
}

/*
================
pace

Sleeps between batches; false when the lane is stopping.
================
*/
func (l *economyFoldLane) pace() bool {
	timer := time.NewTimer(economyFoldPace)
	defer timer.Stop()
	select {
	case <-l.done:
		return false
	case <-timer.C:
		return true
	}
}

/*
================
holdsOver

How many samples of h lie in buckets wholly at or above boundMs, and
how many samples it holds in all.
================
*/
func holdsOver(h latency.Histogram, boundMs float64) (over, all uint64) {
	for i, n := range h.Buckets {
		all += n
		if i > 0 && h.BoundsMs[i-1] >= boundMs {
			over += n
		}
	}
	return over, all
}

/*
================
storeBusy

Whether the store's write holds since the lane last looked were
mostly at or over the fold's bound. It reads the difference of two
snapshots: the histogram's own maximum and totals are since process
start, and one slow commit an hour ago says nothing about now (read
whole, the first hold past the bound would stop the deletes for the
life of the process). The lane's own batches are among the holds
counted; a batch at the bound is a reason to wait too.
================
*/
func (l *economyFoldLane) storeBusy() bool {
	now := l.store.mu.writeHold.Snapshot()
	overNow, allNow := holdsOver(now, economyFoldMaxHoldMs)
	overSeen, allSeen := holdsOver(l.holdsSeen, economyFoldMaxHoldMs)
	l.holdsSeen = now
	recent := allNow - allSeen
	return recent > 0 && (overNow-overSeen)*economyFoldBusyShare > recent
}

/*
================
waitForQuiet

After a batch: while the store is busy, wait a pace at a time for a
quieter window rather than add jitter, at most economyFoldBusyPaces
of them. Then the next batch runs whatever the holds say, so a store
that is always slow still drains its history, slowly. False when the
lane is stopping.
================
*/
func (l *economyFoldLane) waitForQuiet() bool {
	for waited := 0; waited < economyFoldBusyPaces && l.storeBusy(); waited++ {
		if !l.pace() {
			return false
		}
	}
	return true
}

/*
================
foldNewRows

Folds every unmarked row past the watermark into the buckets, one
bounded window at a time (foldOneWindow), pacing between windows so
a door always wins the lock back. True when at least one window
folded.
================
*/
func (l *economyFoldLane) foldNewRows() (bool, error) {
	// One read connection serves every window of the fold: opening
	// it per window would charge a small store an open per few
	// hundred rows when the window sits at its floor.
	read, err := connectDB(l.store.dbPath)
	if err != nil {
		return false, err
	}
	defer read.Close()
	if _, err := read.Exec("PRAGMA query_only=ON"); err != nil {
		return false, err
	}
	folded := false
	for {
		advanced, err := l.foldOneWindow(read)
		if err != nil {
			return folded, err
		}
		if !advanced {
			return folded, nil
		}
		folded = true
		// The window's sums are in: let the tick have the lock back
		// before the next window's write asks for it.
		if !l.pace() {
			return folded, nil
		}
	}
}

/*
================
foldOneWindow

Folds one seq window (watermark, window end] of unmarked rows: the
grouping aggregates run on a separate read connection (a WAL reader
never blocks a door), then one write transaction upserts the
window's buckets and advances the watermark past the window. A
crash either side of that transaction replays or skips the whole
window - never half of it, never twice: commit-written rows carry
the commit's bucketed mark and are skipped by the aggregates, and
the watermark moves only with the buckets. False when nothing is
past the watermark.
================
*/
func (l *economyFoldLane) foldOneWindow(read *sql.DB) (bool, error) {
	foldSeq, _, err := l.watermarks()
	if err != nil {
		return false, err
	}
	if l.windowRows == 0 {
		l.windowRows = economyFoldWindowRows
	}
	var maxSeq int64
	if err := read.QueryRow("SELECT COALESCE(MAX(seq), 0) FROM economy_ledger").Scan(&maxSeq); err != nil {
		return false, err
	}
	if maxSeq <= foldSeq {
		return false, nil
	}
	endSeq := foldSeq + l.windowRows
	if endSeq > maxSeq {
		endSeq = maxSeq
	}
	global := map[ledgerBucketKey]*ledgerBucketFold{}
	chars := map[ledgerCharBucketKey]*ledgerBucketFold{}
	// The global fold takes every unmarked row, whatever its reason:
	// rows the bucketing commit path wrote are marked bucketed = 1
	// and already summed, so what remains is history and an older
	// binary's rollback-window rows, folded exactly once. The
	// per-character fold takes the bulk reasons (the only rows the
	// delete pass may remove, and never marked).
	rows, err := read.Query(`SELECT (at_ms / ?) * ?, division, reason, gold > 0, SUM(gold), MIN(gold), MAX(gold), COUNT(*)
FROM economy_ledger WHERE seq > ? AND seq <= ? AND bucketed IS NULL
GROUP BY 1, division, reason, gold > 0`, economyHourMs, economyHourMs, foldSeq, endSeq)
	if err != nil {
		return false, err
	}
	for rows.Next() {
		var key ledgerBucketKey
		var fold ledgerBucketFold
		if err := rows.Scan(&key.hourMs, &key.division, &key.reason, &key.gain, &fold.total, &fold.low, &fold.high, &fold.n); err != nil {
			rows.Close()
			return false, err
		}
		global[key] = &fold
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return false, err
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	charRows, err := read.Query(`SELECT (at_ms / ?) * ?, char_id, reason, SUM(gold), COUNT(*)
FROM economy_ledger WHERE seq > ? AND seq <= ? AND bucketed IS NULL AND reason IN ('monster_gold', 'npc_sale')
GROUP BY 1, char_id, reason`, economyHourMs, economyHourMs, foldSeq, endSeq)
	if err != nil {
		return false, err
	}
	for charRows.Next() {
		var key ledgerCharBucketKey
		var total, n int64
		if err := charRows.Scan(&key.hourMs, &key.charID, &key.reason, &total, &n); err != nil {
			charRows.Close()
			return false, err
		}
		chars[key] = &ledgerBucketFold{total: total, n: n}
	}
	if err := charRows.Err(); err != nil {
		charRows.Close()
		return false, err
	}
	if err := charRows.Close(); err != nil {
		return false, err
	}
	// The watermark advances even when nothing in the window folds
	// (a stretch of commit-marked rows only): the delete pass jumps
	// those the same way.
	held, err := l.applyWrite(func(tx *sql.Tx) error {
		if err := upsertLedgerBucketsTx(tx, global, chars); err != nil {
			return err
		}
		return upsertMetaTx(tx, metaKeyEconomyFoldSeq, strconv.FormatInt(endSeq, 10))
	})
	if err != nil {
		return false, err
	}
	l.foldSeq.Store(endSeq)
	l.adaptWindow(held)
	return true, nil
}

/*
================
adaptWindow

Sizes the next fold window by this window's measured write hold:
halve toward the floor while a hold passes the bound, double
toward the ceiling while holds sit far under it.
================
*/
func (l *economyFoldLane) adaptWindow(held time.Duration) {
	heldMs := float64(held) / float64(time.Millisecond)
	switch {
	case heldMs > economyFoldMaxHoldMs:
		l.windowRows /= 2
		if l.windowRows < economyFoldWindowMinRows {
			l.windowRows = economyFoldWindowMinRows
		}
	case heldMs < economyFoldMaxHoldMs/4:
		l.windowRows *= 2
		if l.windowRows > economyFoldWindowMaxRows {
			l.windowRows = economyFoldWindowMaxRows
		}
	}
}

/*
================
deleteBatch

Deletes one bounded batch of folded bulk rows below the watermark
and advances the delete watermark in the same transaction. Returns
how many rows went.
================
*/
func (l *economyFoldLane) deleteBatch() (int64, error) {
	foldSeq, deleteSeq, err := l.watermarks()
	if err != nil {
		return 0, err
	}
	if foldSeq <= deleteSeq {
		return 0, nil
	}
	var deleted, batchSeq int64
	_, err = l.applyWrite(func(tx *sql.Tx) error {
		if err := tx.QueryRow(`SELECT COALESCE(MAX(seq), 0) FROM (
  SELECT seq FROM economy_ledger
  WHERE seq > ? AND seq <= ? AND reason IN ('monster_gold', 'npc_sale')
  ORDER BY seq LIMIT ?
)`, deleteSeq, foldSeq, economyFoldDeleteBatchRows).Scan(&batchSeq); err != nil {
			return err
		}
		if batchSeq <= deleteSeq {
			// Only keep-reason rows in range: the delete
			// watermark jumps the fold watermark, they stay.
			batchSeq = foldSeq
		}
		res, err := tx.Exec(`DELETE FROM economy_ledger
WHERE seq > ? AND seq <= ? AND reason IN ('monster_gold', 'npc_sale')`, deleteSeq, batchSeq)
		if err != nil {
			return err
		}
		if deleted, err = res.RowsAffected(); err != nil {
			return err
		}
		return upsertMetaTx(tx, metaKeyEconomyDeleteSeq, strconv.FormatInt(batchSeq, 10))
	})
	if err != nil {
		return 0, err
	}
	l.delSeq.Store(batchSeq)
	if deleted == 0 {
		return 0, nil
	}
	l.batches.Add(1)
	return deleted, nil
}

/*
================
finishIfDrained

When no bulk rows remain on either side of the watermark: drop the
empty at_ms index and record fold-complete, once.
================
*/
func (l *economyFoldLane) finishIfDrained() error {
	s := l.store
	foldSeq, _, err := l.watermarks()
	if err != nil {
		return err
	}
	read, err := connectDB(s.dbPath)
	if err != nil {
		return err
	}
	var remaining int64
	if err := read.QueryRow(`SELECT COUNT(*) FROM economy_ledger
WHERE reason IN ('monster_gold', 'npc_sale') AND seq > ?`, foldSeq).Scan(&remaining); err != nil {
		read.Close()
		return err
	}
	read.Close()
	if remaining > 0 || l.fin.Load() {
		return nil
	}
	_, err = l.applyWrite(func(tx *sql.Tx) error {
		if _, err := tx.Exec("DROP INDEX IF EXISTS economy_ledger_at"); err != nil {
			return err
		}
		return upsertMetaTx(tx, metaKeyEconomyFoldDone, "1")
	})
	if err != nil {
		return err
	}
	l.fin.Store(true)
	log.Infof("economy fold: history folded to seq %d; the ledger's rows are buckets now", foldSeq)
	return nil
}

/*
================
applyWrite

Runs one small write transaction of the lane under the store lock,
through the store's synchronous handle (syncDBLocked: with the
asynchronous commit writer on, its queue is drained first, so the
lane never waits on SQLite's one writer while it holds the lock),
and records how long the lock was held: the lane's batches read in the store lock snapshot beside the
doors' holds (lock_timing.go), a hold past the bound is logged, and
the hold itself goes back to the caller (the fold sizes its next
window by it).
================
*/
func (l *economyFoldLane) applyWrite(write func(tx *sql.Tx) error) (time.Duration, error) {
	s := l.store
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, fmt.Errorf("no database open")
	}
	begun := s.mu.now()
	tx, err := s.syncDBLocked().Begin()
	if err != nil {
		return 0, err
	}
	if err := write(tx); err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	held := s.mu.now().Sub(begun)
	l.hold.Add(held)
	if float64(held)/float64(time.Millisecond) > economyFoldMaxHoldMs {
		log.Warnf("economy fold: a batch held the store lock %.1f ms (bound %.0f ms)",
			float64(held)/float64(time.Millisecond), economyFoldMaxHoldMs)
	}
	return held, nil
}

/*
================
vacuumIfFreelistDominates

The shrink (#3187): at boot, after the database loads and before the
store serves, when the freelist is most of the file, one in-place
VACUUM rewrites the live content only. It runs on the live file, so
no commit can be lost to it, and once it has run the freelist is
near zero and the condition never fires again.

It returns nothing on purpose. The caller's ladder reads any error
from the open as a damaged database and quarantines the file; a
shrink that could not run (no room for the rewrite, a busy file, a
stop that arrived) found nothing damaged. It says why at Warn and
the next boot tries again. The context is the open's: a stop ends
the rewrite, and SQLite leaves the file as it was.
================
*/
func vacuumIfFreelistDominates(ctx context.Context, db *sql.DB) {
	var pages, freelist int64
	if err := db.QueryRowContext(ctx, "PRAGMA page_count").Scan(&pages); err != nil {
		log.Warnf("economy shrink: reading the page count failed: %v", err)
		return
	}
	if err := db.QueryRowContext(ctx, "PRAGMA freelist_count").Scan(&freelist); err != nil {
		log.Warnf("economy shrink: reading the freelist failed: %v", err)
		return
	}
	if pages == 0 || freelist*economyVacuumFreelistShare <= pages {
		return
	}
	started := time.Now()
	if _, err := db.ExecContext(ctx, "VACUUM"); err != nil {
		log.Warnf("economy shrink: VACUUM failed after %s (freelist %d of %d pages); the file stays as it is and the next boot tries again: %v",
			time.Since(started).Round(time.Millisecond), freelist, pages, err)
		return
	}
	log.Infof("economy shrink: vacuumed the authority in %s (freelist was %d of %d pages)",
		time.Since(started).Round(time.Millisecond), freelist, pages)
}
