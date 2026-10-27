/*
===========================================================================

previous_schema_test.go - a release's own previous schema takes the offline route

Every release that bumps CurrentVersion meets authorities stamped with the
version before it. The server must refuse that store in place (production
startup never rewrites a schema), and sro-authority-upgrade must take it:
the release rehearsal depends on both halves holding at once.

===========================================================================
*/
package store

import (
	"errors"
	"testing"
)

/*
================
TestOpenRefusesThePreviousSchemaUntilUpgraded

A fresh store stamped CurrentVersion-1 fails Open with the incompatible-schema
error; the offline upgrade validates it, commits it, and Open then succeeds.
================
*/
func TestOpenRefusesThePreviousSchemaUntilUpgraded(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	openTest(t, dir, clock).Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, CurrentVersion-1)

	if s, err := Open(dir, Options{Now: clock.Now, DefaultSkills: testSkillSeeder}); err == nil {
		s.Close()
		t.Fatal("Open accepted the previous schema")
	} else if !errors.Is(err, errIncompatibleSchema) {
		t.Fatalf("Open refused the previous schema with %v, want the incompatible-schema error", err)
	}

	// Validation alone keeps the store as it was and leaves no backup.
	if backup, err := UpgradeAuthority(dir, false); err != nil || backup != "" {
		t.Fatalf("validating the previous schema: backup %q, %v", backup, err)
	}
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatalf("upgrading the previous schema: backup %q, %v", backup, err)
	}
	openTest(t, dir, clock).Close()
}
