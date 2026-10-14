/*
===========================================================================

mostwanted_upgrade_test.go - the schema 24 step (mostWanted)

===========================================================================
*/
package store

import (
	"errors"
	"testing"
)

/*
================
TestMostWantedUpgradeRewritesNothing

A schema 23 authority at layout 9 upgrades to 24 with its records untouched
(the new streak is optional and absent); a second upgrade has nothing to do.
================
*/
func TestMostWantedUpgradeRewritesNothing(t *testing.T) {
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
	downgradeToLayout9(t, dir)
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preMostWantedVersion)
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
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
