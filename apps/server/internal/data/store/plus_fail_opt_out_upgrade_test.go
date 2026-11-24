/*
===========================================================================

plus_fail_opt_out_upgrade_test.go - the schema 51 step (plusFailOptOut)

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
TestPlusFailOptOutUpgradeRewritesNothing

A schema 50 authority upgrades to 51 with its records untouched (the
new opt-out is optional and absent, so every character keeps being
announced); a second upgrade has nothing to do.
================
*/
func TestPlusFailOptOutUpgradeRewritesNothing(t *testing.T) {
	tier.Long(t, "a schema-50 authority upgrades to 51 with its records untouched")
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
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, prePlusFailOptOutVersion)
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
