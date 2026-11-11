/*
===========================================================================

economy_fold_test.go - history folds into the buckets and the rows go
(#3187)

The fold eats unmarked rows from the read side and stores the buckets
with its watermark in one write; the deletes are bounded batches
below the watermark that never touch a keep-reason row; and the boot
after the drain vacuums the file back to the size of its live data
without losing the commit that landed after the last batch.

===========================================================================
*/
package store

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"
)

/*
================
seedLedgerRow

One history row, written as an older binary wrote it: unmarked, so
the fold still owes it a bucket.
================
*/
func seedLedgerRow(t *testing.T, s *Store, atMs, charID int64, reason string, gold int64) {
	t.Helper()
	if _, err := s.syncDBLocked().Exec(`INSERT INTO economy_ledger (at_ms, division, char_id, reason, door, gold, balance)
VALUES (?, ?, ?, ?, 'seed', ?, 0)`, atMs, testDivision, charID, reason, gold); err != nil {
		t.Fatal(err)
	}
}

/*
================
testFoldLane

A lane driven by hand: no timer, the test calls its passes.
================
*/
func testFoldLane(s *Store) *economyFoldLane {
	return &economyFoldLane{store: s, done: make(chan struct{}), stopped: make(chan struct{})}
}

const foldHour1 = int64(1_700_000_000_000) / economyHourMs * economyHourMs
const foldHour2 = foldHour1 + economyHourMs

/*
================
TestFoldMovesHistoryIntoBuckets

History's rows fold into both bucket tables with the sums the row
fold would give, keep-reason rows included in the aggregates; the
watermark records the folded range; the deletes then remove only
the bulk rows, and the finish drops the empty index.
================
*/
func TestFoldMovesHistoryIntoBuckets(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	seedLedgerRow(t, s, foldHour1+60_000, 7, "monster_gold", 100)
	seedLedgerRow(t, s, foldHour1+120_000, 7, "monster_gold", 50)
	seedLedgerRow(t, s, foldHour2+60_000, 7, "monster_gold", 30)
	seedLedgerRow(t, s, foldHour1+90_000, 8, "npc_sale", 25)
	seedLedgerRow(t, s, foldHour1+30_000, 7, "exchange", -200)
	seedLedgerRow(t, s, foldHour1+45_000, 7, "shop_buy", -40)

	lane := testFoldLane(s)
	folded, err := lane.foldNewRows()
	if err != nil || !folded {
		t.Fatalf("fold %v, %v", folded, err)
	}
	hours := ledgerHourTotals(t, s)
	if hours["monster_gold gain"] != 180 || hours["npc_sale gain"] != 25 ||
		hours["exchange loss"] != -200 || hours["shop_buy loss"] != -40 {
		t.Fatalf("buckets after the fold %v", hours)
	}
	if got := ledgerCharHourTotals(t, s, 7); got["monster_gold"] != 180 {
		t.Fatalf("char buckets %v", got)
	}
	if got := ledgerCharHourTotals(t, s, 8); got["npc_sale"] != 25 {
		t.Fatalf("char buckets %v", got)
	}
	// The fold deletes nothing itself; the deletes are separate.
	if rows := ledgerRows(t, s); len(rows) != 6 {
		t.Fatalf("rows after the fold %+v", rows)
	}
	for {
		n, err := lane.deleteBatch()
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	rows := ledgerRows(t, s)
	if len(rows) != 2 || rows[0].reason != "exchange" || rows[1].reason != "shop_buy" {
		t.Fatalf("keep rows after the deletes %+v", rows)
	}
	if err := lane.finishIfDrained(); err != nil {
		t.Fatal(err)
	}
	if !lane.stats().Done {
		t.Fatal("fold-complete was not recorded")
	}
	var index int
	if err := s.syncDBLocked().QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = 'economy_ledger_at'").Scan(&index); err != nil {
		t.Fatal(err)
	}
	if index != 0 {
		t.Fatal("the empty at_ms index survived the drain")
	}
}

/*
================
TestDeleteBatchIsBounded

One delete batch removes at most economyFoldDeleteBatchRows rows
and advances only its own watermark.
================
*/
func TestDeleteBatchIsBounded(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	for i := 0; i < economyFoldDeleteBatchRows+2500; i++ {
		seedLedgerRow(t, s, foldHour1+int64(i%3600)*1000, 7, "monster_gold", 1)
	}
	lane := testFoldLane(s)
	if _, err := lane.foldNewRows(); err != nil {
		t.Fatal(err)
	}
	n, err := lane.deleteBatch()
	if err != nil {
		t.Fatal(err)
	}
	if n != economyFoldDeleteBatchRows {
		t.Fatalf("one batch deleted %d rows, bound is %d", n, economyFoldDeleteBatchRows)
	}
	var remaining int
	if err := s.syncDBLocked().QueryRow("SELECT count(*) FROM economy_ledger WHERE reason = 'monster_gold'").Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 2500 {
		t.Fatalf("%d bulk rows remain, want 2500", remaining)
	}
	if lane.stats().Batches != 1 {
		t.Fatalf("batches %d", lane.stats().Batches)
	}
}

/*
================
TestFoldSkipsWhatCommitsAlreadyBucketed

A row a bucketing commit wrote (marked) is never folded twice: the
fold range passes it by, and its sums stay the commit's own.
================
*/
func TestFoldSkipsWhatCommitsAlreadyBucketed(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "FoldMarked", 100)
	setGoldIn(s, c, "npc-repair", 60)
	seedLedgerRow(t, s, foldHour1+60_000, c.ID, "repair", -5)
	lane := testFoldLane(s)
	if _, err := lane.foldNewRows(); err != nil {
		t.Fatal(err)
	}
	hours := ledgerHourTotals(t, s)
	// The commit's -40 and history's -5, each counted once.
	if hours["repair loss"] != -45 {
		t.Fatalf("buckets %v, want repair loss -45 counted once each", hours)
	}
}

/*
================
TestBootVacuumShrinksAndKeepsTheLastCommit

After the drain, the next boot's VACUUM returns the file to the size
of its live data, and a commit made after the last background batch
survives it.
================
*/
func TestBootVacuumShrinksAndKeepsTheLastCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := newLedgerCharacter(t, s, "FoldVacuum", 100)
	for i := 0; i < 20000; i++ {
		seedLedgerRow(t, s, foldHour1+int64(i%3600)*1000, c.ID, "monster_gold", 1)
	}
	lane := testFoldLane(s)
	if _, err := lane.foldNewRows(); err != nil {
		t.Fatal(err)
	}
	for {
		n, err := lane.deleteBatch()
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 {
			break
		}
	}
	// A commit after the last batch: bucketed at once, never a row.
	setGoldIn(s, c, "pickup-gold", 177)
	s.Close()
	// Sizes are only meaningful settled: while a store is open the
	// deleted pages sit in the WAL, and the boot VACUUM's rebuilt
	// file reaches the main file at the close checkpoint too.
	path := filepath.Join(dir, DBFileName)
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	reopened := openTest(t, dir, newTestClock())
	if got := ledgerCharHourTotals(t, reopened, c.ID); got["monster_gold"] != 20000+77 {
		t.Fatalf("char buckets after the shrink %v, want the folded 20000 and the last commit's 77", got)
	}
	reopened.Close()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Size() >= before.Size() {
		t.Fatalf("the boot vacuum did not shrink the file: %d before, %d after", before.Size(), after.Size())
	}
}

/*
================
TestEconomyCharacterHoursAnswers

The per-character buckets answer who gained how much of a bulk
reason, and when.
================
*/
func TestEconomyCharacterHoursAnswers(t *testing.T) {
	t.Parallel()

	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "FoldChar", 100)
	setGoldIn(s, c, "pickup-gold", 160)
	setGoldIn(s, c, "pickup-gold", 260)
	hours, err := s.EconomyCharacterHours(c.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(hours) != 1 || hours[0].Reason != "monster_gold" || hours[0].Total != 160 || hours[0].N != 2 {
		t.Fatalf("char hours %+v", hours)
	}
}

/*
================
TestAShrinkThatCannotRunLeavesAHealthyFile

The ladder reads any error from the open as a damaged database and
quarantines the file. A shrink that cannot run found nothing damaged:
it must report and return, the file must stay as it was, and the
handle must go on working. Here another connection holds the write
lock, so the VACUUM is refused.
================
*/
func TestAShrinkThatCannotRunLeavesAHealthyFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "shrink.db")
	db, err := connectDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, statement := range []string{
		"CREATE TABLE kept (id INTEGER PRIMARY KEY, body TEXT)",
		"INSERT INTO kept VALUES (1, 'survives')",
		"CREATE TABLE filler (body BLOB)",
		"INSERT INTO filler VALUES (zeroblob(400000))",
		"DROP TABLE filler",
		"PRAGMA busy_timeout=50",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	var before, freelist int64
	if err := db.QueryRow("PRAGMA page_count").Scan(&before); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA freelist_count").Scan(&freelist); err != nil {
		t.Fatal(err)
	}
	if freelist*economyVacuumFreelistShare <= before {
		t.Fatalf("the fixture's freelist (%d of %d pages) would not trigger the shrink", freelist, before)
	}

	blocker, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer blocker.Close()
	held, err := blocker.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer held.Close()
	if _, err := held.ExecContext(context.Background(), "BEGIN IMMEDIATE"); err != nil {
		t.Fatal(err)
	}
	vacuumIfFreelistDominates(context.Background(), db)
	if _, err := held.ExecContext(context.Background(), "ROLLBACK"); err != nil {
		t.Fatal(err)
	}

	var after int64
	var body string
	if err := db.QueryRow("PRAGMA page_count").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT body FROM kept WHERE id = 1").Scan(&body); err != nil || body != "survives" {
		t.Fatalf("the handle stopped answering after a refused shrink: %q, %v", body, err)
	}
	if after != before {
		t.Fatalf("a refused shrink changed the file: %d pages, was %d", after, before)
	}
	// With the lock free the same call shrinks the file.
	vacuumIfFreelistDominates(context.Background(), db)
	if err := db.QueryRow("PRAGMA page_count").Scan(&after); err != nil {
		t.Fatal(err)
	}
	if after >= before {
		t.Fatalf("the shrink left %d pages of %d with the lock free", after, before)
	}
}

/*
================
TestOneSlowHoldDoesNotStallTheDeletes

The busy test judges the write holds since the lane last looked. One
slow commit is busy once; after it, a run of quick holds is quiet
again, although the histogram's maximum never comes down. Read as an
all-time maximum, the first hold past the bound stopped the deletes
for the life of the process.
================
*/
func TestOneSlowHoldDoesNotStallTheDeletes(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	lane := testFoldLane(s)
	lane.storeBusy()

	s.mu.writeHold.Add(80 * time.Millisecond)
	if !lane.storeBusy() {
		t.Fatal("a hold of 80 ms since the last look is busy")
	}
	if lane.storeBusy() {
		t.Fatal("with no hold since the last look the store is quiet, whatever its all-time maximum")
	}
	for range 20 {
		s.mu.writeHold.Add(time.Millisecond)
	}
	if lane.storeBusy() {
		t.Fatal("twenty quick holds since the last look are quiet; the old slow one is history")
	}
	for range 3 {
		s.mu.writeHold.Add(40 * time.Millisecond)
	}
	s.mu.writeHold.Add(time.Millisecond)
	if !lane.storeBusy() {
		t.Fatal("three slow holds of four since the last look are busy")
	}
	// A quiet store never makes the deletes wait.
	started := time.Now()
	if !lane.waitForQuiet() {
		t.Fatal("waitForQuiet reported a stop on a running lane")
	}
	if waited := time.Since(started); waited > economyFoldPace {
		t.Fatalf("a quiet store made the deletes wait %s", waited)
	}
}
