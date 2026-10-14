/*
===========================================================================

snapshot_test.go - a snapshot is the committed generation it was taken from

===========================================================================
*/
package store

import (
	"path/filepath"
	"testing"
)

/*
================
TestSnapshotCopiesTheCommittedGeneration

The copy holds the committed ledger and characters, a later door changes
the live store but not the copy, and a new snapshot replaces the old one.
================
*/
func TestSnapshotCopiesTheCommittedGeneration(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := newLedgerCharacter(t, s, "SnapOne", 250)
	path := filepath.Join(t.TempDir(), "snapshot.db")
	if err := s.SnapshotTo(path); err != nil {
		t.Fatal(err)
	}
	setGoldIn(s, c, "pickup-gold", 300)
	copyDB, err := connectDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer copyDB.Close()
	var rows, characters int
	if err := copyDB.QueryRow("SELECT count(*) FROM economy_ledger").Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if err := copyDB.QueryRow("SELECT count(*) FROM characters").Scan(&characters); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || characters != 1 {
		t.Fatalf("snapshot holds %d ledger rows and %d characters, want 1 and 1", rows, characters)
	}
	if live := ledgerRows(t, s); len(live) != 2 {
		t.Fatalf("live ledger %+v", live)
	}
	// A second snapshot replaces the first with the newer generation.
	copyDB.Close()
	if err := s.SnapshotTo(path); err != nil {
		t.Fatal(err)
	}
	again, err := connectDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	if err := again.QueryRow("SELECT count(*) FROM economy_ledger").Scan(&rows); err != nil || rows != 2 {
		t.Fatalf("second snapshot holds %d rows (%v)", rows, err)
	}
}

/*
================
TestDoorCommitsWhileASnapshotReads

A snapshot reads through its own connection; while such a reader holds
its read transaction open (as VACUUM INTO does for the whole copy), a door
still commits, because WAL readers never block the writer.
================
*/
func TestDoorCommitsWhileASnapshotReads(t *testing.T) {
	s := openTest(t, t.TempDir(), newTestClock())
	c := newLedgerCharacter(t, s, "SnapReader", 10)
	reader, err := connectDB(s.dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	tx, err := reader.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback() }()
	var held int
	if err := tx.QueryRow("SELECT count(*) FROM economy_ledger").Scan(&held); err != nil {
		t.Fatal(err)
	}
	setGoldIn(s, c, "pickup-gold", 25)
	if health := s.Health(); health.FailedWrites != 0 {
		t.Fatalf("the door failed to commit beside an open reader: %+v", health)
	}
	if rows := ledgerRows(t, s); len(rows) != held+1 {
		t.Fatalf("live ledger %d rows, reader held %d", len(rows), held)
	}
}
