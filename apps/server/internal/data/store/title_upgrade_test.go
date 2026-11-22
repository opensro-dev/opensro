/*
===========================================================================

title_upgrade_test.go - the schema 49 step (per-character title id)

===========================================================================
*/
package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestTitleUpgradeRewritesNothing

A schema 48 authority at layout 14 upgrades to 49 with its records
untouched (the title id is optional and absent, so every character
wears no title); a second upgrade has nothing to do.
================
*/
func TestTitleUpgradeRewritesNothing(t *testing.T) {
	tier.Long(t, "a schema-48 layout-14 authority upgrades to 49 with its records untouched")
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preTitleVersion)
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrade %q: %v", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	var after string
	if err := reopened.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("the upgrade rewrote the character")
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
