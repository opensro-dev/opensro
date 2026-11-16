/*
===========================================================================
raven_refuge_upgrade_test.go - preserve records and optional Refuge deadlines.

Schema 44 adds a top-level optional value; its offline step must not rewrite
characters, their equipment or a deadline retained by an earlier reader.
===========================================================================
*/
package store

import (
	"errors"
	"testing"
)

/*
================
TestRavenRefugeUpgradePreservesRecords
================
*/
func TestRavenRefugeUpgradePreservesRecords(t *testing.T) {
	for _, saved := range []bool{false, true} {
		name := "absent"
		if saved {
			name = "retained deadline"
		}
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			s := openTest(t, dir, newTestClock())
			c := seededCharacter()
			if saved {
				c.RavenRefugeReadyAtMs = 1900000120000
			}
			if err := s.CreateCharacter(testDivision, "account", c); err != nil {
				t.Fatal(err)
			}
			var before string
			if err := s.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
				t.Fatal(err)
			}
			s.Close()
			rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preRavenRefugeVersion)
			if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
				t.Fatalf("offline upgrade %q: %v", backup, err)
			}
			reopened := openTest(t, dir, newTestClock())
			var after string
			if err := reopened.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&after); err != nil {
				t.Fatal(err)
			}
			if before != after {
				t.Fatal("the additive upgrade rewrote the character or its equipment")
			}
			reopened.Close()
			if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
				t.Fatalf("repeat upgrade: %v", err)
			}
		})
	}
}
