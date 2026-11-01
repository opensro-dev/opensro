/*
===========================================================================

wal_checkpoint_test.go - slice 0: the checkpoint leaves the commit (#2142)

The writer's handle must never auto-checkpoint (so no door's commit
pays the copy again), the background cycle must copy what commits
leave, wait out a reader that pins the WAL, stop with Close, and leave
BackupTo working. The slow-commit line names its door here too.

===========================================================================
*/

package store

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/enterworld"
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
