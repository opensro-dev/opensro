/*
===========================================================================

rename_upgrade_test.go - the schema 33 / layout 11 step (name-change scroll)

===========================================================================
*/
package store

import (
	"errors"
	"path/filepath"
	"testing"
)

/*
================
TestRenameUpgradeAddsReservationTable

A schema 32 authority at layout 10 upgrades to 33/11 with its records
untouched (the rename fields are optional and absent); the upgrade adds
the empty name_reservations table. A second upgrade has nothing to do.
================
*/
func TestRenameUpgradeAddsReservationTable(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preRenameVersion)
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, preNameReservationLayoutVersion)
	db, err := openDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE name_reservations"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.db.QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("the upgrade rewrote the character")
	}
	var tables int
	if err := reopened.db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = 'name_reservations'").Scan(&tables); err != nil {
		t.Fatal(err)
	}
	if tables != 1 {
		t.Fatal("the upgrade did not add name_reservations")
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
