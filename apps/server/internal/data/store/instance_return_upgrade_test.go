/*
===========================================================================

instance_return_upgrade_test.go - the schema 29 step (world.instanceReturn)

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
TestInstanceReturnUpgradeRewritesNothing

A schema 28 authority upgrades to 29 with its records untouched (the
instance return anchor is optional and absent); a second upgrade has
nothing to do.
================
*/
func TestInstanceReturnUpgradeRewritesNothing(t *testing.T) {
	tier.Long(t, "a schema-28 authority upgrades to 29 with its records untouched")
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
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preInstanceReturnVersion)
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
