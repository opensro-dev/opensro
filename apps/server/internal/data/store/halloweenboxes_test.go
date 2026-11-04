/*
===========================================================================

halloweenboxes_test.go - the per-account Halloween box count plane

===========================================================================
*/
package store

import (
	"errors"
	"testing"
)

/*
================
TestHalloweenAccountSurvivesSaveLoad

The account count is the cap's memory: a restart mid-event must not
hand the account a second daily allowance (#2547).
================
*/
func TestHalloweenAccountSurvivesSaveLoad(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	// One settlement-style door: the record moves inside it and the
	// commit stages the plane (persistence.go).
	if !s.UpdateCharacter(c, "test halloween mint", func() bool {
		record := s.HalloweenAccount("account-a")
		record.Day, record.Count = 20736, 17
		return true
	}) {
		t.Fatal("the mint door refused")
	}
	s.Close()

	reopened := openTest(t, dir, clock)
	defer reopened.Close()
	loaded := reopened.HalloweenAccount("account-a")
	if loaded.Day != 20736 || loaded.Count != 17 {
		t.Fatalf("account = day %d count %d, want 20736/17", loaded.Day, loaded.Count)
	}
	if got := reopened.HalloweenAccount("account-b"); got.Day != 0 || got.Count != 0 {
		t.Fatalf("untouched account = %+v, want zero", got)
	}
}

/*
================
TestHalloweenBoxesSeedFromLegacyCounts

An authority with no halloweenBoxes row counted per character
(schema 34/35): at first load the account record is the latest day's
counts summed over the account's characters, so the day's progress
survives the move to the account (#2547). An older day and a
character that never minted seed nothing.
================
*/
func TestHalloweenBoxesSeedFromLegacyCounts(t *testing.T) {
	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	seed := func(name, account string, day, count int64) {
		c := seededCharacter()
		c.Name = name
		c.AccountID = account
		c.HalloweenBoxDay = day
		c.HalloweenBoxCount = count
		if err := s.CreateCharacter(testDivision, account, c); err != nil {
			t.Fatal(err)
		}
	}
	seed("main", "account-a", 100, 5)
	seed("alt", "account-a", 100, 3)
	seed("stale", "account-a", 99, 20)
	seed("other", "account-b", 100, 7)
	seed("fresh", "account-c", 100, 0)
	s.Close()

	reopened := openTest(t, dir, clock)
	defer reopened.Close()
	if got := reopened.HalloweenAccount("account-a"); got.Day != 100 || got.Count != 8 {
		t.Fatalf("account-a seeded = %+v, want day 100 count 8", got)
	}
	if got := reopened.HalloweenAccount("account-b"); got.Day != 100 || got.Count != 7 {
		t.Fatalf("account-b seeded = %+v, want day 100 count 7", got)
	}
	if got := reopened.HalloweenAccount("account-c"); got.Day != 0 || got.Count != 0 {
		t.Fatalf("account-c seeded = %+v, want zero (nothing minted)", got)
	}
}

/*
================
TestHalloweenBoxesRefuseCorruptRows

A negative count or a blank account key in the row is corruption, not
zero progress: the load refuses it, like the trade pools.
================
*/
func TestHalloweenBoxesRefuseCorruptRows(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	defer s.Close()
	if _, err := s.db.Exec("INSERT INTO meta (key, value) VALUES (?, ?)", metaKeyHalloweenBoxes, `{"account-a":{"day":1,"count":-1}}`); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadHalloweenBoxes(s.db); err == nil {
		t.Fatal("a negative count loaded")
	}
	if _, err := s.db.Exec("UPDATE meta SET value = ? WHERE key = ?", `{" ":{"day":1,"count":1}}`, metaKeyHalloweenBoxes); err != nil {
		t.Fatal(err)
	}
	if _, _, err := loadHalloweenBoxes(s.db); err == nil {
		t.Fatal("a blank account key loaded")
	}
}

/*
================
TestHalloweenAccountUpgradeRewritesNothing

A schema 35 authority at layout 11 upgrades to 36 with its records
untouched (the account count starts absent and seeds at first load);
a second upgrade has nothing to do.
================
*/
func TestHalloweenAccountUpgradeRewritesNothing(t *testing.T) {
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
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preHalloweenAccountVersion)
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
