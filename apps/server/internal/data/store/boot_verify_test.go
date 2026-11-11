/*
===========================================================================

boot_verify_test.go - the boot gate covers the state; the rest follows

The store proves the tables a boot loads before it serves, and leaves
the whole-file check and the spare copy to the verifier that runs after
it (boot_verify.go, opensro-ops#277). These tests damage one table's
root page in a closed database and read what each half says: a damaged
log table must not stop a boot and must be reported afterwards, a
damaged state table must send the boot down the recovery ladder, and a
stop must never wait for the verifier's copy.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// The database header names the first freelist trunk page in its bytes
// 32..35, big-endian (sqlite.org/fileformat.html, section 1.3).
const (
	freelistTrunkHeaderOffset = 32
	freelistTrunkHeaderEnd    = 36
)

/*
================
damageTableRoot

Overwrites the root page of one table in a closed database with bytes
no b-tree page can hold. The store's Close has checkpointed the WAL, so
the main file is the whole database.
================
*/
func damageTableRoot(t *testing.T, dir, table string) {
	t.Helper()
	path := filepath.Join(dir, DBFileName)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	var root, pageSize int64
	if err := db.QueryRow("SELECT rootpage FROM sqlite_master WHERE name = ?", table).Scan(&root); err != nil {
		t.Fatalf("root page of %s: %v", table, err)
	}
	if err := db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	removeDBSidecars(path)
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	garbage := make([]byte, pageSize)
	for i := range garbage {
		garbage[i] = 0xA5
	}
	if _, err := file.WriteAt(garbage, (root-1)*pageSize); err != nil {
		t.Fatal(err)
	}
}

/*
================
seedBootStore

A closed authority holding one character, with its spare copy written
by a second boot.
================
*/
func seedBootStore(t *testing.T, dir string, clock *testClock) {
	t.Helper()
	s := openTest(t, dir, clock)
	if err := s.CreateCharacter(testDivision, "test-account", seededCharacter()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	openTest(t, dir, clock).Close()
}

/*
================
TestBootServesBesideADamagedLogTableAndReportsIt

The economy ledger is a log no boot step loads. Damage in it must not
keep the server down: the gate passes, the state loads, and the
verifier's full check reports the damage in Health afterwards. The
spare copy is not refreshed from a file that failed its check.
================
*/
func TestBootServesBesideADamagedLogTableAndReportsIt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	seedBootStore(t, dir, clock)
	bakPath := filepath.Join(dir, DBBakFileName)
	before, err := os.ReadFile(bakPath)
	if err != nil {
		t.Fatalf("the seed boots left no spare copy: %v", err)
	}
	damageTableRoot(t, dir, "economy_ledger")

	s := openTest(t, dir, clock)
	if got := len(s.Characters().CharactersForDivision(testDivision)); got != 1 {
		t.Fatalf("a damaged log table kept the state from loading: %d characters, want 1", got)
	}
	health := s.Health()
	if health.LoadedFromBak {
		t.Fatal("a damaged log table sent the boot to the spare copy; the state was whole")
	}
	if health.IntegrityError == "" {
		t.Fatal("the full check after boot did not report the damaged log table")
	}
	after, err := os.ReadFile(bakPath)
	if err != nil || string(after) != string(before) {
		t.Fatalf("the spare copy was refreshed from a file that failed its check (err %v)", err)
	}
}

/*
================
TestBootGateSendsADamagedStateTableDownTheLadder

The gate is the state: a damaged characters table is what the
quarantine and bak ladder exists for, and it still fires before the
store serves.
================
*/
func TestBootGateSendsADamagedStateTableDownTheLadder(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	seedBootStore(t, dir, clock)
	damageTableRoot(t, dir, "characters")

	s := openTest(t, dir, clock)
	health := s.Health()
	if !health.LoadedFromBak {
		t.Fatal("a damaged state table must quarantine the main file and load the spare copy")
	}
	if got := len(s.Characters().CharactersForDivision(testDivision)); got != 1 {
		t.Fatalf("the recovered store has %d characters, want 1", got)
	}
	quarantines, err := filepath.Glob(filepath.Join(dir, DBFileName) + corruptSuffix + "*")
	if err != nil || len(quarantines) != 1 {
		t.Fatalf("exactly one quarantine artifact expected, got %v (err %v)", quarantines, err)
	}
}

/*
================
TestBootGateChecksTheFreelist

A partial check reads the freelist only when it is asked for the schema
table. The gate asks: a freelist whose first trunk page is garbage would
hand a state table a page already in use, so it must fail before the
store serves.
================
*/
func TestBootGateChecksTheFreelist(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	seedBootStore(t, dir, clock)
	path := filepath.Join(dir, DBFileName)

	// A dropped table leaves its pages on the freelist.
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{
		"CREATE TABLE freelist_filler (body BLOB)",
		"INSERT INTO freelist_filler VALUES (zeroblob(200000))",
		"DROP TABLE freelist_filler",
		"PRAGMA wal_checkpoint(TRUNCATE)",
	} {
		if _, err := db.Exec(statement); err != nil {
			t.Fatalf("%s: %v", statement, err)
		}
	}
	var pageSize int64
	if err := db.QueryRow("PRAGMA page_size").Scan(&pageSize); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	removeDBSidecars(path)

	// The header's bytes 32..35 name the first freelist trunk page.
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	header := make([]byte, freelistTrunkHeaderEnd)
	if _, err := file.ReadAt(header, 0); err != nil {
		t.Fatal(err)
	}
	trunk := int64(binary.BigEndian.Uint32(header[freelistTrunkHeaderOffset:freelistTrunkHeaderEnd]))
	if trunk == 0 {
		t.Fatal("the dropped table left no freelist to damage")
	}
	garbage := make([]byte, pageSize)
	for i := range garbage {
		garbage[i] = 0xA5
	}
	if _, err := file.WriteAt(garbage, (trunk-1)*pageSize); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	probe, err := connectDB(path)
	if err != nil {
		t.Fatal(err)
	}
	defer probe.Close()
	if err := quickCheckState(probe); err == nil {
		t.Fatal("the gate passed a database whose freelist is garbage")
	}
}

/*
================
TestAStopNeverWaitsForTheSpareCopy

Close interrupts the verifier: whichever of the check and the copy it
had reached, the stop returns, no partial copy is left behind, and the
spare copy on disk is either the previous one or a complete new one.
================
*/
func TestAStopNeverWaitsForTheSpareCopy(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	clock := newTestClock()
	seedBootStore(t, dir, clock)
	bakPath := filepath.Join(dir, DBBakFileName)

	for range 20 {
		s, err := Open(dir, Options{Now: clock.Now, DefaultSkills: testSkillSeeder})
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		if _, err := os.Stat(bakPath + bootSpareSuffix); !os.IsNotExist(err) {
			t.Fatalf("an interrupted verifier left its partial copy behind (err %v)", err)
		}
		// Whatever the interrupt caught, the spare copy still loads.
		probe, err := connectDB(bakPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := quickCheckState(probe); err != nil {
			t.Fatalf("the spare copy does not pass the gate after an interrupted refresh: %v", err)
		}
		probe.Close()
	}
}
