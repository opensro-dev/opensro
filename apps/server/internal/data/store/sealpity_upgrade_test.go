/*
===========================================================================

sealpity_upgrade_test.go - the schema 33 step (sealPity)

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
TestSealPityUpgradeRewritesNothing

A schema 32 authority at layout 10 upgrades to 33 with its records
untouched (the seal pity counters are optional and absent); a second
upgrade has nothing to do.
================
*/
func TestSealPityUpgradeRewritesNothing(t *testing.T) {
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
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preSealPityVersion)
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
TestSealPityCountersRoundTrip

A character's luck-debt counters survive a save and reload exactly:
classes, fractional debts and the forced-day counters.
================
*/
func TestSealPityCountersRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	c.SealPity = []domain.SealPityDebt{
		{ItemClass: 26, Debt: 1.4375, ForcedDay: 1727740800, ForcedToday: 1},
		{ItemClass: 25, Debt: 0.03125},
	}
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	s.Close()

	reopened := openTest(t, dir, newTestClock())
	reopened.ReadCharacters(testDivision, func(characters []*domain.Character) {
		if len(characters) != 1 {
			t.Fatalf("characters = %d, want 1", len(characters))
		}
		got := characters[0].SealPity
		if len(got) != len(c.SealPity) {
			t.Fatalf("seal pity rows = %+v, want %+v", got, c.SealPity)
		}
		for i, want := range c.SealPity {
			if got[i] != want {
				t.Fatalf("row %d = %+v, want %+v", i, got[i], want)
			}
		}
	})
	reopened.Close()
}
