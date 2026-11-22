/*
===========================================================================

sqlitecol_test.go - the additive column migration

A missing column is added with its default, a present one is left alone,
a second run is a no-op, and a table that does not exist is an error.

===========================================================================
*/
package sqlitecol

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

/*
================
openTestDB
================
*/
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "cols.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE things (id INTEGER PRIMARY KEY, name TEXT NOT NULL);
INSERT INTO things (name) VALUES ('old');`); err != nil {
		t.Fatal(err)
	}
	return db
}

/*
================
TestEnsureAddsWhatAFileLacks

An older file gains the later columns with their defaults, its rows
keep their values, and a second run changes nothing.
================
*/
func TestEnsureAddsWhatAFileLacks(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	columns := []Column{
		{Table: "things", Name: "name", Definition: "TEXT NOT NULL DEFAULT 'ignored'"},
		{Table: "things", Name: "weight", Definition: "INTEGER NOT NULL DEFAULT -1"},
		{Table: "things", Name: "tag", Definition: "TEXT NOT NULL DEFAULT ''"},
	}
	for run := 0; run < 2; run++ {
		if err := Ensure(db, columns); err != nil {
			t.Fatalf("run %d: %v", run, err)
		}
	}
	var name, tag string
	var weight int64
	if err := db.QueryRow(`SELECT name, weight, tag FROM things`).Scan(&name, &weight, &tag); err != nil {
		t.Fatal(err)
	}
	if name != "old" || weight != -1 || tag != "" {
		t.Fatalf("row after the migration: %q %d %q", name, weight, tag)
	}
	for _, column := range columns {
		if present, err := Has(db, column.Table, column.Name); err != nil || !present {
			t.Fatalf("%s missing after Ensure (%v)", column.Name, err)
		}
	}
}

/*
================
TestEnsureRefusesAMissingTable

A column for a table that does not exist is the caller's mistake: it
fails loudly instead of reading as present.
================
*/
func TestEnsureRefusesAMissingTable(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	if present, err := Has(db, "nothing", "id"); err != nil || present {
		t.Fatalf("a missing table read present=%v err=%v", present, err)
	}
	if err := Ensure(db, []Column{{Table: "nothing", Name: "id", Definition: "INTEGER"}}); err == nil {
		t.Fatal("a column for a missing table was accepted")
	}
}
