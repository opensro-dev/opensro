/*
===========================================================================

halloween_upgrade_test.go - the schema 34 step (Halloween box count)

===========================================================================
*/
package store

import (
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestHalloweenUpgradeRewritesNothing

A schema 33 authority at layout 11 upgrades to 34 with its records untouched
(the daily box count is optional and absent); a second upgrade has nothing
to do.
================
*/
func TestHalloweenUpgradeRewritesNothing(t *testing.T) {
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
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preHalloweenVersion)
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

/*
================
TestHalloweenBoxCountSurvivesSaveLoad

The daily cap fields round-trip through the store (the restart-mid-event
edge: a server restart must not reset the day's count).
================
*/
func TestHalloweenBoxCountSurvivesSaveLoad(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	c.HalloweenBoxDay = 20736
	c.HalloweenBoxCount = 17
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened := openTest(t, dir, clock)
	var loaded *domain.Character
	reopened.ReadCharacters(testDivision, func(characters []*domain.Character) {
		for _, candidate := range characters {
			if candidate.ID == c.ID {
				loaded = candidate
			}
		}
	})
	if loaded == nil {
		t.Fatal("character not loaded")
	}
	if loaded.HalloweenBoxDay != 20736 || loaded.HalloweenBoxCount != 17 {
		t.Fatalf("cap = day %d count %d, want 20736/17", loaded.HalloweenBoxDay, loaded.HalloweenBoxCount)
	}
}
