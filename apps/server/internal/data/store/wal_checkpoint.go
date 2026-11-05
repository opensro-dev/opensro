/*
===========================================================================

wal_checkpoint.go - the WAL's background checkpoint (#2142 slice 0)

SQLite's automatic checkpoint runs inside whichever commit crosses its
1000-page threshold, so one door's tx.Commit() pays a multi-megabyte
copy and fsync the others never see coming (the 1,126 ms commit in one
skill's store update). configureDB now sets wal_autocheckpoint=0 on the
writer handle, and this goroutine copies the frames instead: every few
seconds it runs wal_checkpoint(PASSIVE) on its own handle - never
RESTART or TRUNCATE, so a reader is never waited on and Close keeps its
final TRUNCATE. Durability is unchanged: committed transactions were
already durable in the WAL at their commit; the checkpoint only copies
pages into the main file, and a crash mid-checkpoint leaves the WAL
whole for the next boot's replay.

The counters land on /transport/metrics inside store_lock
(lock_timing.go), so a checkpoint starved by a long-lived reader (a
backup, say) reads as a growing WAL and a busy streak, not as a boot
that mysteriously replays for minutes.

===========================================================================
*/

package store

import (
	"os"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
)

const (
	// walCheckpointInterval is how often the background cycle runs.
	walCheckpointInterval = 5 * time.Second
	// walCheckpointPassesPerCycle caps one cycle's PASSIVE calls: a
	// cycle keeps copying until SQLite reports nothing left, but never
	// loops forever against a reader that keeps the WAL pinned.
	walCheckpointPassesPerCycle = 4
	// walCheckpointStopTimeout bounds Close's wait for the goroutine.
	// A cycle is a handful of bounded SQLite calls; passing this means
	// something below the store is wedged, and Close proceeds anyway -
	// the dying process takes the handle with it.
	walCheckpointStopTimeout = 10 * time.Second
	// walJournalSizeLimitBytes bounds the WAL file a completed
	// checkpoint leaves behind for reuse: four of the old 1000-page /
	// 4 KiB autocheckpoint volumes. configureDB sets it on the writer.
	walJournalSizeLimitBytes = 16 << 20
	// walStarvationBytes is the WAL size that names a checkpoint
	// starved whatever the busy counters say: boot replays every byte
	// of it.
	walStarvationBytes = 64 << 20
	// walBusyStreakToLog is the run of busy-ended cycles that names
	// the starvation in the log; the counters carry it from the first.
	walBusyStreakToLog = 3
	// walStarvationLogInterval rate-limits the starvation line while
	// the condition holds.
	walStarvationLogInterval = time.Minute
)

/*
================
walCheckpointer

One store's background checkpoint: a goroutine, its counters, and no
shared state beyond them. It opens a fresh handle per cycle through
connectDB (busy_timeout=5000, like every store handle) and closes it
at the cycle's end, so between cycles it holds no file handle at
all. It takes no store door: the WAL is SQLite's to arbitrate.
================
*/
type walCheckpointer struct {
	dbPath  string
	done    chan struct{}
	stopped chan struct{}

	cycles      atomic.Int64
	busyCycles  atomic.Int64
	busyStreak  atomic.Int64
	walBytes    atomic.Int64
	walBytesMax atomic.Int64

	lastStarvationLogMs atomic.Int64
}

/*
================
startWALCheckpointer

Starts the goroutine for dbPath. Between Open's claim and Close there
is exactly one store per database, so exactly one checkpointer.
================
*/
func startWALCheckpointer(dbPath string) *walCheckpointer {
	if dbPath == "" {
		return nil
	}
	c := &walCheckpointer{
		dbPath:  dbPath,
		done:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go c.run()
	return c
}

/*
================
walCheckpointer.run

The goroutine: one cycle per interval until stop closes done. The
first cycle waits a full interval - boot has just loaded through a
checkpointed image, so there is nothing to copy yet.
================
*/
func (c *walCheckpointer) run() {
	defer close(c.stopped)
	ticker := time.NewTicker(walCheckpointInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-ticker.C:
			c.cycle()
		}
	}
}

/*
================
walCheckpointer.stop

Signals the goroutine and waits for its current cycle to finish,
bounded by walCheckpointStopTimeout. Called once, from Close, before
Close's own TRUNCATE checkpoint: the two never race.
================
*/
func (c *walCheckpointer) stop() {
	close(c.done)
	select {
	case <-c.stopped:
	case <-time.After(walCheckpointStopTimeout):
		log.Warnf("store: wal checkpointer still running after %s; closing without it", walCheckpointStopTimeout)
	}
}

/*
================
walCheckpointer.cycle

One checkpoint cycle: PASSIVE passes until every frame is
checkpointed or the pass cap runs out, and returns whether the cycle
ended starved. PASSIVE's own busy flag only says the checkpoint could
not run at all; the quieter starvation - frames it could not move
past the oldest reader, expected under a backup - reads in the
result's last two columns. Either way it counts toward the
starvation line; it is never an error worth failing on.
================
*/
func (c *walCheckpointer) cycle() (starved bool) {
	db, err := connectDB(c.dbPath)
	if err != nil {
		log.Warnf("store: wal checkpoint could not open its handle: %v", err)
		return false
	}
	defer db.Close()
	c.cycles.Add(1)

	for pass := 0; pass < walCheckpointPassesPerCycle; pass++ {
		var busyFlag, walPages, checkpointedPages int
		if err := db.QueryRow("PRAGMA wal_checkpoint(PASSIVE)").Scan(&busyFlag, &walPages, &checkpointedPages); err != nil {
			log.Warnf("store: wal_checkpoint(PASSIVE) failed: %v", err)
			return false
		}
		if busyFlag == 0 && checkpointedPages >= walPages {
			starved = false
			break
		}
		starved = true
	}

	size := walFileBytes(c.dbPath)
	c.walBytes.Store(size)
	for {
		peak := c.walBytesMax.Load()
		if size <= peak || c.walBytesMax.CompareAndSwap(peak, size) {
			break
		}
	}
	if starved {
		c.busyCycles.Add(1)
		c.busyStreak.Add(1)
	} else {
		c.busyStreak.Store(0)
	}
	if c.busyStreak.Load() >= walBusyStreakToLog || size > walStarvationBytes {
		c.logStarved(size)
	}
	return starved
}

/*
================
walCheckpointer.logStarved

The starvation line, at most once per walStarvationLogInterval while
the condition holds: a growing WAL is slower boot replay, and it must
be discovered here rather than at the next restart. Two triggers light
it - a busy streak (a reader pins the WAL) and the WAL's size alone -
and the line names the one that fired (#2296): the size trigger with a
zero streak used to print as busy starvation while every checkpoint
was completing. PASSIVE only copies frames; it never shrinks the
file, so the size line says that instead of promising a reader's exit.
================
*/
func (c *walCheckpointer) logStarved(walBytes int64) {
	nowMs := time.Now().UnixMilli()
	last := c.lastStarvationLogMs.Load()
	if nowMs-last < walStarvationLogInterval.Milliseconds() ||
		!c.lastStarvationLogMs.CompareAndSwap(last, nowMs) {
		return
	}
	streak := c.busyStreak.Load()
	switch {
	case streak >= walBusyStreakToLog && walBytes > walStarvationBytes:
		log.Warnf("store: wal checkpoint starved (busy cycles and WAL size): %d busy cycle(s) in a row, WAL at %d bytes past the %d-byte line; boot replay grows until a reader lets go",
			streak, walBytes, int64(walStarvationBytes))
	case walBytes > walStarvationBytes:
		log.Warnf("store: wal checkpoint starved (WAL size, not busy): WAL at %d bytes past the %d-byte line, %d busy cycle(s) in a row; checkpoints are completing - PASSIVE copies frames but never shrinks the file, so boot replay stays long",
			walBytes, int64(walStarvationBytes), streak)
	default:
		log.Warnf("store: wal checkpoint starved (busy cycles): %d busy cycle(s) in a row, WAL at %d bytes; boot replay grows until a reader lets go",
			streak, walBytes)
	}
}

/*
================
walFileBytes

The WAL file's current size; a missing WAL is 0, not an error.
================
*/
func walFileBytes(dbPath string) int64 {
	info, err := os.Stat(dbPath + "-wal")
	if err != nil {
		return 0
	}
	return info.Size()
}
