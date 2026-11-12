/*
===========================================================================

wal_checkpoint.go - the WAL's background checkpoint (#2142 slice 0)

SQLite's automatic checkpoint runs inside whichever commit crosses its
1000-page threshold, so one door's tx.Commit() pays a multi-megabyte
copy and fsync the others never see coming (the 1,126 ms commit in one
skill's store update). configureDB now sets wal_autocheckpoint=0 on the
writer handle, and this goroutine copies the frames instead: every few
seconds it runs wal_checkpoint(PASSIVE) on its own handle, so a reader
is never waited on and durability is unchanged: committed transactions
were already durable in the WAL at their commit; the checkpoint only
copies pages into the main file, and a crash mid-checkpoint leaves the
WAL whole for the next boot's replay.

The log starts over only when a writer begins with every frame already
copied, and one PASSIVE pass never leaves it so beside a busy writer:
the pass copies up to the frame it met, the writer commits during the
copy, and the pass returns without syncing the main file or marking
the log done. So a cycle does not stop at its first drained pass. It
chases the tail with further passes - each has only the frames of the
previous pass's duration to copy - until one finds nothing new, which
means the pass before it ended level with the writer: SQLite synced
the main file on this lane's handle, and the writer's next transaction
restarts the log by itself and trims the file to journal_size_limit.
The passes never take the writer lock. A transaction that begins
while a pass holds the wal-index locks it works under can still wait
on them for a moment: beside the old single pass one begin waited
63 ms, beside the chase the worst of 2,011 commits took 8 ms.

Before the chase (#3143, live 2026-11-11) the log never restarted: it
grew 2.9 MB/s to the ceiling every 90 s, and the reset there holds the
writer lock while it copies the last frames and syncs the main file.
A commit that met the lock slept on SQLite's busy ladder (1, 2, 5, 10,
15, 20, 25, 25 ms and on): 127 of that day's 138 slow commits sat
nearest the ladder's sums 103 and 128 ms, a median 1.5 ms above the
sum. How the hold itself divides between copy and sync was not
measured on the server. A scratch run
beside a writer committing every 20 ms showed the same without the
chase: the file reached 232 MB in 45 s, and with the ceiling lowered
to 32 MB a reset held a commit 108 ms. With the chase,
TestWALStaysShortBesideASteadyWriter (long tier) holds what that load
must show - the log restarts, no reset runs, the file stays near
journal_size_limit - and reports the writer's worst commit.

The reset remains for what the chase cannot do. PASSIVE never shrinks
the file, and a log that grew behind a reader (2026-11-05: 9.6 GB) or
beside a writer that never pauses keeps growing. When a drained cycle
leaves the file past walCeilingBytes, the lane escalates once to
wal_checkpoint(TRUNCATE) - the reset Close already performs at
shutdown. The ceiling keeps an ordinary backup pin from ever reaching
it, a cycle that ends starved never truncates, and a reset that meets
a reader or a writer gives up at once.

The counters land on /transport/metrics inside store_lock
(lock_timing.go), so a checkpoint starved by a long-lived reader (a
backup, say) reads as a growing WAL and a busy streak, not as a boot
that mysteriously replays for minutes; log restarts, cycles that
caught up, and how long each reset kept writers out read there too.

===========================================================================
*/

package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/latency"
)

const (
	// walCheckpointInterval is how often the background cycle runs.
	walCheckpointInterval = 5 * time.Second
	// walCheckpointPassesPerCycle caps one cycle's PASSIVE calls: a
	// cycle keeps copying until it is level with the writer, but never
	// loops forever against a reader that keeps the WAL pinned or a
	// writer that never pauses. A quiet store ends in two passes; the
	// measured writer (TestWALStaysShortBesideASteadyWriter) is caught
	// within a few cycles, and a cycle that is not leaves its tail
	// to the next.
	walCheckpointPassesPerCycle = 8
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
	// walCeilingBytes is the drained WAL size past which one cycle
	// resets the file with wal_checkpoint(TRUNCATE). It stands at four
	// times the starvation line so an ordinary backup hold - the WAL
	// the 2026-11-05 backup pinned was 68 MB when the starved line
	// first fired - never reaches it; only a file stuck at its
	// high-water mark after the reader left pays the shrink.
	walCeilingBytes = 256 << 20
	// walBusyStreakToLog is the run of busy-ended cycles that names
	// the starvation in the log; the counters carry it from the first.
	walBusyStreakToLog = 3
	// walStarvationLogInterval rate-limits the starvation line while
	// the condition holds.
	walStarvationLogInterval = time.Minute
	// walSlowResetThreshold is the reset time that names itself in the
	// log: writers were locked out that long, so a slow commit beside
	// the line has its cause. Half the slow-commit line, because the
	// waiting commit adds its own busy-ladder sleep on top.
	walSlowResetThreshold = 50 * time.Millisecond
	// The WAL file header (SQLite file format, section 4.1): 32 bytes,
	// with the checkpoint sequence number big-endian at offset 12.
	// SQLite adds one each time the log starts over.
	walHeaderBytes          = 32
	walHeaderSequenceOffset = 12
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

	// ceiling is walCeilingBytes held in an atomic so the tests can
	// stand at a smaller file; production never changes it.
	ceiling atomic.Int64

	cycles      atomic.Int64
	busyCycles  atomic.Int64
	busyStreak  atomic.Int64
	resets      atomic.Int64
	resetBusy   atomic.Int64
	walBytes    atomic.Int64
	walBytesMax atomic.Int64

	// caughtUp counts the cycles that ended level with the writer and
	// restarts the times the log started over (noteRestarts);
	// lastSequence is the WAL header's sequence number plus one as last
	// read, zero before the first read. resetHold is how long each
	// reset kept writers out.
	caughtUp     atomic.Int64
	restarts     atomic.Int64
	lastSequence atomic.Int64
	resetHold    latency.Live

	// afterPass, when set, runs after each drained pass with its
	// index: the tests commit there, between two passes, where a live
	// writer commits whenever it likes. Production leaves it nil.
	afterPass func(pass int)

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
	c.ceiling.Store(walCeilingBytes)
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

One checkpoint cycle: PASSIVE passes until one drains the log, then
the chase - further passes until one meets the log where the pass
before it drained it (walPosition), so nothing was committed in
between and the log is level with the writer (the file banner says
why that matters) - or until the pass cap runs out. Returns whether
the cycle ended starved: no pass drained. PASSIVE's own busy flag only
says the checkpoint could not run at all; the quieter starvation -
frames it could not move past the oldest reader, expected under a
backup - reads in the result's last two columns. Either way it counts
toward the starvation line; it is never an error worth failing on. A
drained cycle whose file still stands past the ceiling resets it in
place (resetWAL) before the size lands on the counters.
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

	drained, caughtUp := false, false
	var drainedAt walPosition
	for pass := 0; pass < walCheckpointPassesPerCycle && !caughtUp; pass++ {
		at := walPosition{generation: c.noteRestarts()}
		var busyFlag, checkpointedFrames int
		if err := db.QueryRow("PRAGMA wal_checkpoint(PASSIVE)").Scan(&busyFlag, &at.frames, &checkpointedFrames); err != nil {
			log.Warnf("store: wal_checkpoint(PASSIVE) failed: %v", err)
			return false
		}
		if busyFlag != 0 || checkpointedFrames < at.frames {
			if drained {
				// The chase met a transaction that began after the
				// drained pass. The cycle copied what it came for;
				// the next one chases again.
				break
			}
			continue
		}
		caughtUp = drained && at == drainedAt
		drained, drainedAt = true, at
		if c.afterPass != nil {
			c.afterPass(pass)
		}
	}
	starved = !drained
	if caughtUp {
		c.caughtUp.Add(1)
	}

	size := walFileBytes(c.dbPath)
	if !starved && size > c.ceilingBytes() && c.resetWAL(db) {
		// The reset dropped the file; report the size it left, not
		// the high-water mark this cycle started from.
		size = walFileBytes(c.dbPath)
	}
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
walPosition

Where a pass met the log: which log (the header's sequence number
plus one, zero when the file has no header) and how many frames long.
Two drained passes at one position had no commit between them. The
length alone cannot say that: a log that started over and took a
commit of the same size reads the same.
================
*/
type walPosition struct {
	generation int64
	frames     int
}

/*
================
walCheckpointer.noteRestarts

Reads the WAL header's sequence number, which SQLite raises by one
each time the log starts over - after a pass that ended level with the
writer, or after a reset - counts the restarts since the last read,
and returns the log's generation for walPosition. A file with no
header (just reset, nothing written since) is generation zero and
counts nothing; the next read picks the count up.
================
*/
func (c *walCheckpointer) noteRestarts() (generation int64) {
	sequence, ok := walRestartSequence(c.dbPath)
	if !ok {
		return 0
	}
	generation = int64(sequence) + 1
	previous := c.lastSequence.Swap(generation)
	switch {
	case previous == 0:
	case generation > previous:
		c.restarts.Add(generation - previous)
	case generation < previous:
		// A recreated file counts from zero again: one restart.
		c.restarts.Add(1)
	}
	return generation
}

/*
================
walRestartSequence

The checkpoint sequence number in the WAL file's header, and whether
the file has one. Reading the WAL through a second descriptor is safe
where reading the database file is not: SQLite keeps its POSIX locks
on the database and the shared-memory file, and closing any descriptor
of a locked file drops the process's locks on it.
================
*/
func walRestartSequence(dbPath string) (uint32, bool) {
	file, err := os.Open(dbPath + "-wal")
	if err != nil {
		return 0, false
	}
	defer file.Close()
	var header [walHeaderBytes]byte
	if _, err := io.ReadFull(file, header[:]); err != nil {
		return 0, false
	}
	return binary.BigEndian.Uint32(header[walHeaderSequenceOffset:]), true
}

/*
================
walCheckpointer.ceilingBytes

The drained size past which a cycle resets the file: the stored
value, or walCeilingBytes when nothing stored one (a checkpointer
built outside startWALCheckpointer).
================
*/
func (c *walCheckpointer) ceilingBytes() int64 {
	if ceiling := c.ceiling.Load(); ceiling > 0 {
		return ceiling
	}
	return walCeilingBytes
}

/*
================
walCheckpointer.resetWAL

One wal_checkpoint(TRUNCATE) on the cycle's own handle: the same
reset Close runs, taken here because a completed checkpoint left
the file past the ceiling and PASSIVE alone never returns those
pages to the filesystem. Only a drained cycle reaches it, so the
reader that pinned the file is already gone.

TRUNCATE, unlike PASSIVE, holds off new writers while it is pending
and waits on any reader still using the WAL through the busy handler.
Under the handle's busy_timeout (5 s) one live read would stall every
door commit behind this lane for up to that long. The reset therefore
runs on a pinned connection with busy_timeout=0: a writer or reader in
the way makes it busy at once, which counts and retries next cycle.

A reset that runs keeps writers out while it copies the last frames
and syncs the main file - every page the passes copied since the last
sync, when no cycle caught up in between. That time lands on resetHold,
and past walSlowResetThreshold it names itself in the log.
Returns whether the file was reset.
================
*/
func (c *walCheckpointer) resetWAL(db *sql.DB) bool {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		log.Warnf("store: wal reset connection: %v", err)
		return false
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "PRAGMA busy_timeout=0"); err != nil {
		log.Warnf("store: wal reset busy_timeout: %v", err)
		return false
	}
	var busyFlag, walPages, checkpointedPages int
	started := time.Now()
	err = conn.QueryRowContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)").Scan(&busyFlag, &walPages, &checkpointedPages)
	held := time.Since(started)
	if isBusy(err) {
		c.resetBusy.Add(1)
		return false
	}
	if err != nil {
		log.Warnf("store: wal_checkpoint(TRUNCATE) failed: %v", err)
		return false
	}
	if busyFlag != 0 {
		c.resetBusy.Add(1)
		return false
	}
	c.resets.Add(1)
	c.resetHold.Add(held)
	if held > walSlowResetThreshold {
		log.Warnf("store: wal reset kept writers out %.1f ms; a slow commit beside this line waited on it",
			float64(held)/float64(time.Millisecond))
	}
	return true
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

/*
================
isBusy

Whether err is SQLite's SQLITE_BUSY ("database is locked"): with
busy_timeout=0 a reset that meets a writer or reader returns it at
once instead of waiting.
================
*/
func isBusy(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "SQLITE_BUSY") || strings.Contains(msg, "database is locked")
}
