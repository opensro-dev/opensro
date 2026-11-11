/*
===========================================================================

wal_checkpoint_test.go - slice 0: the checkpoint leaves the commit (#2142)

The writer's handle must never auto-checkpoint (so no door's commit
pays the copy again), the background cycle must copy what commits
leave, chase a writer until the log can start over (#3143), wait out a
reader that pins the WAL, stop with Close, and leave BackupTo working.
The slow-commit line names its door here too.

===========================================================================
*/

package store

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/tier"
	"opensro.online/server/internal/testsupport/wait"
)

const slowCommitTestDoor = "slow-door"

/*
================
TestConfigureDBTakesTheCheckpointOutOfCommits

The writer handle's pragmas are the slice: no automatic checkpoint
inside a commit, and the WAL a finished checkpoint leaves is bounded.
================
*/
func TestConfigureDBTakesTheCheckpointOutOfCommits(t *testing.T) {
	db, err := openDB(filepath.Join(t.TempDir(), DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var autocheckpoint, sizeLimit int64
	if err := db.QueryRow("PRAGMA wal_autocheckpoint").Scan(&autocheckpoint); err != nil {
		t.Fatal(err)
	}
	if autocheckpoint != 0 {
		t.Fatalf("wal_autocheckpoint = %d, want 0 (the background cycle owns the checkpoint)", autocheckpoint)
	}
	if err := db.QueryRow("PRAGMA journal_size_limit").Scan(&sizeLimit); err != nil {
		t.Fatal(err)
	}
	if sizeLimit != walJournalSizeLimitBytes {
		t.Fatalf("journal_size_limit = %d, want %d", sizeLimit, walJournalSizeLimitBytes)
	}
}

/*
================
TestWALCheckpointerDrainsCommittedFrames

A commit leaves its frames in the WAL; one cycle copies them, and
the metrics say so.
================
*/
func TestWALCheckpointerDrainsCommittedFrames(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	if s.checkpoints == nil {
		t.Fatal("Open started no checkpointer")
	}
	c := &enterworld.Character{Name: "checkpointed"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	cycles := s.LockTiming().CheckpointCycles
	if starved := s.checkpoints.cycle(); starved {
		t.Fatal("the cycle ended starved with no reader holding the WAL")
	}
	timing := s.LockTiming()
	if timing.CheckpointCycles <= cycles {
		t.Fatalf("checkpoint cycles %d -> %d, want the manual cycle counted", cycles, timing.CheckpointCycles)
	}
	if timing.CheckpointBusyStreak != 0 {
		t.Fatalf("busy streak %d after a clean cycle, want 0", timing.CheckpointBusyStreak)
	}
}

/*
================
TestWALCheckpointerWaitsOutAReader

A read snapshot pins the WAL past its mark: the cycle ends busy (and
the streak shows it) instead of forcing anything, then drains once
the reader lets go. A backup holds exactly such a snapshot.
================
*/
func TestWALCheckpointerWaitsOutAReader(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	first := &enterworld.Character{Name: "beforepin"}
	if err := s.CreateCharacter(testDivision, "test-account", first); err != nil {
		t.Fatal(err)
	}
	pin, err := connectDB(s.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pin.Close() })
	tx, err := pin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM characters").Scan(&count); err != nil {
		t.Fatal(err)
	}
	second := &enterworld.Character{Name: "afterpin"}
	if err := s.CreateCharacter(testDivision, "test-account", second); err != nil {
		t.Fatal(err)
	}
	if starved := s.checkpoints.cycle(); !starved {
		t.Fatal("the cycle drained the WAL past a reader holding it")
	}
	if streak := s.LockTiming().CheckpointBusyStreak; streak < 1 {
		t.Fatalf("busy streak %d behind a reader, want at least 1", streak)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if starved := s.checkpoints.cycle(); starved {
		t.Fatal("the cycle stayed starved after the reader let go")
	}
	if streak := s.LockTiming().CheckpointBusyStreak; streak != 0 {
		t.Fatalf("busy streak %d after the WAL drained, want 0", streak)
	}
}

/*
================
TestWALCheckpointerResetsAWalPastTheCeiling

The 2026-11-05 shape: a read snapshot pins the WAL while commits
append past its mark, so the file keeps its high-water size after
the reader lets go even though every checkpoint completes. While the
reader holds, no cycle resets anything; the first drained cycle past
the ceiling takes the file back.
================
*/
func TestWALCheckpointerResetsAWalPastTheCeiling(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	if s.checkpoints == nil {
		t.Fatal("Open started no checkpointer")
	}
	// A small ceiling keeps the test's WAL small; production stands
	// at walCeilingBytes.
	const ceiling = 256 << 10
	s.checkpoints.ceiling.Store(ceiling)

	if starved := s.checkpoints.cycle(); starved {
		t.Fatal("the cycle ended starved on an empty store")
	}
	if resets := s.LockTiming().CheckpointResets; resets != 0 {
		t.Fatalf("resets %d under the ceiling, want 0", resets)
	}

	pin, err := connectDB(s.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pin.Close() })
	tx, err := pin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM characters").Scan(&count); err != nil {
		t.Fatal(err)
	}
	// One character per account: the account cap is four, and bulk
	// here is the point - every commit appends past the read mark.
	for i := 0; walFileBytes(s.dbPath) <= ceiling && i < 1000; i++ {
		c := &enterworld.Character{Name: fmt.Sprintf("hero%04d", i)}
		if err := s.CreateCharacter(testDivision, fmt.Sprintf("acct%04d", i), c); err != nil {
			t.Fatal(err)
		}
	}
	if got := walFileBytes(s.dbPath); got <= ceiling {
		t.Fatalf("WAL at %d bytes behind the pin, want past the %d-byte ceiling", got, ceiling)
	}
	if starved := s.checkpoints.cycle(); !starved {
		t.Fatal("the cycle drained the WAL past a reader holding it")
	}
	if resets := s.LockTiming().CheckpointResets; resets != 0 {
		t.Fatalf("resets %d behind a reader, want 0: no lane may force the file", resets)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if starved := s.checkpoints.cycle(); starved {
		t.Fatal("the cycle stayed starved after the reader let go")
	}
	if resets := s.LockTiming().CheckpointResets; resets < 1 {
		t.Fatalf("resets %d past the ceiling with no reader, want at least 1", resets)
	}
	if got := walFileBytes(s.dbPath); got > ceiling {
		t.Fatalf("WAL at %d bytes after the reset, want at most the %d-byte ceiling", got, ceiling)
	}
}

/*
================
TestBackupToWorksBesideTheCheckpointer

The backup's own read snapshot and the checkpointer share the WAL
without either taking the other's lock: the backup lands, and the
cycle after it drains.
================
*/
func TestBackupToWorksBesideTheCheckpointer(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := &enterworld.Character{Name: "backedup"}
	if err := s.CreateCharacter(testDivision, "test-account", c); err != nil {
		t.Fatal(err)
	}
	backupPath := filepath.Join(dir, "backup.db")
	if err := s.BackupTo(backupPath); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(backupPath); err != nil || info.Size() == 0 {
		t.Fatalf("the backup beside the checkpointer: %v (size %v)", err, info)
	}
	if starved := s.checkpoints.cycle(); starved {
		t.Fatal("the cycle ended starved after the backup completed")
	}
}

/*
================
TestCloseStopsTheWALCheckpointer

Close joins the goroutine before its own checkpoint, and a second
Close (every openTest cleanup runs one) stays a no-op.
================
*/
func TestCloseStopsTheWALCheckpointer(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := s.checkpoints
	if c == nil {
		t.Fatal("Open started no checkpointer")
	}
	s.Close()
	if s.checkpoints != nil {
		t.Fatal("Close left the checkpointer installed")
	}
	select {
	case <-c.stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("the checkpointer goroutine did not stop with Close")
	}
}

/*
================
TestWALStarvedLineNamesItsTrigger

Each trigger's line names itself with its numbers (#2296): the size
trigger at a zero busy streak must not read as busy starvation, and
the busy trigger must not borrow the size wording.
================
*/
func TestWALStarvedLineNamesItsTrigger(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	line := func(streak, size int64) string {
		buf.Reset()
		c := &walCheckpointer{}
		c.busyStreak.Store(streak)
		c.logStarved(size)
		return buf.String()
	}

	bigWAL := int64(walStarvationBytes) + 1
	sizeLine := line(0, bigWAL)
	if !strings.Contains(sizeLine, "WAL size, not busy") ||
		!strings.Contains(sizeLine, "0 busy cycle(s)") ||
		!strings.Contains(sizeLine, "67108865 bytes") ||
		strings.Contains(sizeLine, "(busy cycles)") {
		t.Fatalf("size-triggered line misreports its trigger: %q", sizeLine)
	}
	busyLine := line(walBusyStreakToLog, 1024)
	if !strings.Contains(busyLine, "(busy cycles)") ||
		!strings.Contains(busyLine, "3 busy cycle(s)") ||
		strings.Contains(busyLine, "WAL size") {
		t.Fatalf("busy-triggered line misreports its trigger: %q", busyLine)
	}
	bothLine := line(walBusyStreakToLog, bigWAL)
	if !strings.Contains(bothLine, "busy cycles and WAL size") ||
		!strings.Contains(bothLine, "3 busy cycle(s)") ||
		!strings.Contains(bothLine, "67108865 bytes") {
		t.Fatalf("both-triggered line misreports its trigger: %q", bothLine)
	}
}

/*
================
TestSlowCommitNamesItsDoor

A commit over the threshold names its door once; a fast commit and
the rate limit keep the log honest.
================
*/
func TestSlowCommitNamesItsDoor(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	s := openTest(t, t.TempDir(), newTestClock())
	var slow commitPhaseDurations
	slow[commitPhaseEncode] = time.Millisecond
	slow[commitPhaseStatements] = 2 * time.Millisecond
	slow[commitPhaseSQLite] = 140 * time.Millisecond
	slow[commitPhaseTotal] = 150 * time.Millisecond
	s.commits.record(slowCommitTestDoor, slow, commitRowCounts{})
	if !strings.Contains(buf.String(), "slow commit") ||
		strings.Count(buf.String(), slowCommitTestDoor) != 1 {
		t.Fatalf("slow-commit line missing or unnamed, want 1 naming its door:\n%s", buf.String())
	}
	fast := slow
	fast[commitPhaseSQLite] = 40 * time.Millisecond
	fast[commitPhaseTotal] = 45 * time.Millisecond
	s.commits.record(slowCommitTestDoor, fast, commitRowCounts{})
	s.commits.record(slowCommitTestDoor, slow, commitRowCounts{})
	if got := strings.Count(buf.String(), slowCommitTestDoor); got != 1 {
		t.Fatalf("slow-commit lines %d after a fast and a rate-limited slow commit, want 1", got)
	}
}

/*
================
TestWALResetNeverHoldsTheDoorBehindAReader

TRUNCATE holds off writers while it is pending and waits on any reader
still using the WAL. A reader at the latest snapshot lets PASSIVE drain
every frame, so the cycle reaches the reset with that reader open: the
reset must answer busy at once (and retry next cycle), never wait out
the handle's 5 s busy_timeout with every door commit queued behind it.
================
*/
func TestWALResetNeverHoldsTheDoorBehindAReader(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	const ceiling = 256 << 10
	s.checkpoints.ceiling.Store(ceiling)
	for i := 0; walFileBytes(s.dbPath) <= ceiling && i < 1000; i++ {
		c := &enterworld.Character{Name: fmt.Sprintf("door%04d", i)}
		if err := s.CreateCharacter(testDivision, fmt.Sprintf("dacc%04d", i), c); err != nil {
			t.Fatal(err)
		}
	}
	// Every queued write is on disk before the WAL is measured and the
	// reader pins its snapshot (a no-op on the synchronous door).
	s.syncDBLocked()
	if got := walFileBytes(s.dbPath); got <= ceiling {
		t.Fatalf("WAL at %d bytes, want past the %d-byte ceiling", got, ceiling)
	}

	// A reader on the newest snapshot: nothing is past its mark, so
	// PASSIVE copies every frame, but the WAL is still in use.
	pin, err := connectDB(s.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pin.Close() })
	tx, err := pin.Begin()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback() })
	var count int
	if err := tx.QueryRow("SELECT count(*) FROM characters").Scan(&count); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	s.checkpoints.cycle()
	if took := time.Since(start); took > time.Second {
		t.Fatalf("the cycle took %v behind a reader; the reset waited out its busy timeout", took)
	}
	timing := s.LockTiming()
	if timing.CheckpointResets != 0 || timing.CheckpointResetBusy < 1 {
		t.Fatalf("resets %d busy %d with a reader open, want 0 and at least 1",
			timing.CheckpointResets, timing.CheckpointResetBusy)
	}
}

/*
================
newChaseTest

A store with one committed character and a checkpointer of the test's
own on its database: the store's background lane sleeps through its
first interval, so every pass here is the test's. create commits one
more character each call.
================
*/
func newChaseTest(t *testing.T) (s *Store, c *walCheckpointer, create func()) {
	t.Helper()
	s = openTest(t, t.TempDir(), newTestClock())
	created := 0
	create = func() {
		t.Helper()
		hero := &enterworld.Character{Name: fmt.Sprintf("tail%04d", created)}
		if err := s.CreateCharacter(testDivision, fmt.Sprintf("tacc%04d", created), hero); err != nil {
			t.Fatal(err)
		}
		created++
	}
	create()
	return s, &walCheckpointer{dbPath: s.dbPath}, create
}

/*
================
TestWALCheckpointerChasesAWriterUntilTheLogRestarts

A cycle does not stop at its first drained pass. A commit lands
after that pass (and, the log being level there, starts it over): the
second pass copies the commit, the third meets the same log at the
same length and ends the chase. The cycle left the log level again, so
the next commit restarts it in place - the header's sequence number
moves - and the lane counts both restarts without a reset.
================
*/
func TestWALCheckpointerChasesAWriterUntilTheLogRestarts(t *testing.T) {
	s, c, create := newChaseTest(t)
	passes := 0
	c.afterPass = func(pass int) {
		passes++
		if pass == 0 {
			create()
		}
	}
	if starved := c.cycle(); starved {
		t.Fatal("the cycle ended starved with no reader holding the WAL")
	}
	if passes != 3 || c.caughtUp.Load() != 1 {
		t.Fatalf("passes %d caught up %d, want 3 passes (copy, the late commit, nothing new) and 1",
			passes, c.caughtUp.Load())
	}
	before, ok := walRestartSequence(s.dbPath)
	if !ok {
		t.Fatal("the WAL has no header after two commits")
	}

	create()
	after, ok := walRestartSequence(s.dbPath)
	if !ok || after != before+1 {
		t.Fatalf("WAL sequence %d -> %d (read %v), want one restart by the commit after a caught-up cycle",
			before, after, ok)
	}
	c.afterPass = nil
	c.cycle()
	if restarts, resets := c.restarts.Load(), c.resets.Load(); restarts != 2 || resets != 0 {
		t.Fatalf("restarts %d resets %d, want both restarts counted (the late commit's and the next) and no reset",
			restarts, resets)
	}
}

/*
================
TestWALCheckpointerGivesUpTheChaseAtThePassCap

A writer that commits after every pass is never caught. The cycle
stops at the pass cap, is not starved - every pass copied what it met -
and claims no catch-up, so the log's growth toward the ceiling reads
in the counters instead of hiding behind a clean cycle.
================
*/
func TestWALCheckpointerGivesUpTheChaseAtThePassCap(t *testing.T) {
	s, c, create := newChaseTest(t)
	passes := 0
	c.afterPass = func(int) {
		passes++
		create()
	}
	if starved := c.cycle(); starved {
		t.Fatal("the cycle ended starved with no reader holding the WAL")
	}
	if passes != walCheckpointPassesPerCycle || c.caughtUp.Load() != 0 {
		t.Fatalf("passes %d caught up %d, want the %d-pass cap and 0", passes, c.caughtUp.Load(),
			walCheckpointPassesPerCycle)
	}
	// The commit after the last pass was never copied: the log is not
	// level, so the next commit appends instead of starting it over.
	before, _ := walRestartSequence(s.dbPath)
	create()
	if after, _ := walRestartSequence(s.dbPath); after != before {
		t.Fatalf("WAL sequence %d -> %d, want no restart behind a writer the cycle never caught", before, after)
	}
}

/*
================
TestWALStaysShortBesideASteadyWriter

The #3143 measurement as a long-tier test: a writer commits about
280 KB every 20 ms without a pause while a cycle runs after every 25
of its commits (about twice a second; the writer's progress paces the
cycles, never the wall clock). Before the chase the log never started over under this load (232 MB in
45 s) and only the reset at the ceiling took it back, with writers
locked out. With it the log restarts in most cycles, the file stays
near journal_size_limit, and no reset runs.
================
*/
func TestWALStaysShortBesideASteadyWriter(t *testing.T) {
	tier.Long(t, "the log restarts and stays near journal_size_limit beside a writer that never pauses")
	const (
		rows            = 2000
		rowBytes        = 14000
		rowsPerCommit   = 20
		commitEvery     = 20 * time.Millisecond
		commitsPerCycle = 25
		// Bounds a wedged writer only: 25 commits take about half a second.
		cycleWait   = 10 * time.Second
		cycles      = 12
		minRestarts = cycles / 3
		maxWALBytes = 2 * walJournalSizeLimitBytes
	)
	path := filepath.Join(t.TempDir(), DBFileName)
	db, err := openDB(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec("CREATE TABLE soak (k INTEGER PRIMARY KEY, v BLOB)"); err != nil {
		t.Fatal(err)
	}
	blob := make([]byte, rowBytes)
	rng := rand.New(rand.NewSource(1))
	fill, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for k := 0; k < rows; k++ {
		rng.Read(blob)
		if _, err := fill.Exec("INSERT INTO soak (k, v) VALUES (?, ?)", k, blob); err != nil {
			t.Fatal(err)
		}
	}
	if err := fill.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		t.Fatal(err)
	}

	stop := make(chan struct{})
	writerDone := make(chan error, 1)
	// commits counts the writer's finished transactions; writerEnded
	// says it stopped early, so a cycle never waits out a dead writer.
	var commits atomic.Int64
	var writerEnded atomic.Bool
	// worstCommit is the writer's slowest begin-to-commit, in
	// nanoseconds: reported, never asserted (a busy box may spike it;
	// the absence of resets is the property this test holds).
	var worstCommit atomic.Int64
	go func() {
		writerDone <- func() error {
			defer writerEnded.Store(true)
			ticker := time.NewTicker(commitEvery)
			defer ticker.Stop()
			for {
				select {
				case <-stop:
					return nil
				case <-ticker.C:
				}
				begun := time.Now()
				tx, err := db.Begin()
				if err != nil {
					return err
				}
				for i := 0; i < rowsPerCommit; i++ {
					rng.Read(blob[:64])
					if _, err := tx.Exec("UPDATE soak SET v = ? WHERE k = ?", blob, rng.Intn(rows)); err != nil {
						_ = tx.Rollback()
						return err
					}
				}
				if err := tx.Commit(); err != nil {
					return err
				}
				if took := int64(time.Since(begun)); took > worstCommit.Load() {
					worstCommit.Store(took)
				}
				commits.Add(1)
			}
		}()
	}()

	c := &walCheckpointer{dbPath: path}
	var peak int64
	for i := 0; i < cycles; i++ {
		target := int64(i+1) * commitsPerCycle
		wait.Eventually(t, cycleWait, "the writer's next commits before a cycle", func() bool {
			return commits.Load() >= target || writerEnded.Load()
		})
		if writerEnded.Load() {
			// The writer's own error, read below, is the failure.
			break
		}
		c.cycle()
		if size := walFileBytes(path); size > peak {
			peak = size
		}
	}
	close(stop)
	if err := <-writerDone; err != nil {
		t.Fatal(err)
	}
	t.Logf("%d cycles: caught up %d, log restarts %d, resets %d, WAL peak %.1f MB, worst commit %.1f ms",
		cycles, c.caughtUp.Load(), c.restarts.Load(), c.resets.Load(), float64(peak)/(1<<20),
		float64(worstCommit.Load())/float64(time.Millisecond))
	if restarts, resets := c.restarts.Load(), c.resets.Load(); restarts < minRestarts || resets != 0 {
		t.Fatalf("restarts %d resets %d in %d cycles (caught up %d), want at least %d restarts and no reset",
			restarts, resets, cycles, c.caughtUp.Load(), minRestarts)
	}
	if peak > maxWALBytes {
		t.Fatalf("WAL peaked at %d bytes beside a steady writer, want at most %d", peak, int64(maxWALBytes))
	}
}

/*
================
TestLockTimingCarriesTheChaseAndTheResetHold

The store's own lane reports on /transport/metrics: a quiet cycle
counts as caught up, and a reset records how long it kept writers out.
================
*/
func TestLockTimingCarriesTheChaseAndTheResetHold(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	const ceiling = 256 << 10
	s.checkpoints.ceiling.Store(ceiling)
	s.checkpoints.cycle()
	if caughtUp := s.LockTiming().CheckpointCaughtUp; caughtUp != 1 {
		t.Fatalf("caught-up cycles %d after one quiet cycle, want 1", caughtUp)
	}
	for i := 0; walFileBytes(s.dbPath) <= ceiling && i < 1000; i++ {
		hero := &enterworld.Character{Name: fmt.Sprintf("hold%04d", i)}
		if err := s.CreateCharacter(testDivision, fmt.Sprintf("hacc%04d", i), hero); err != nil {
			t.Fatal(err)
		}
	}
	s.syncDBLocked()
	s.checkpoints.cycle()
	timing := s.LockTiming()
	var holds uint64
	for _, count := range timing.CheckpointResetHoldMs.Buckets {
		holds += count
	}
	if timing.CheckpointResets != 1 || holds != 1 {
		t.Fatalf("resets %d with %d hold(s) timed, want 1 and 1", timing.CheckpointResets, holds)
	}
}
