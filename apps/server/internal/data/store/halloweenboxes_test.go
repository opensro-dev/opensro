/*
===========================================================================

halloweenboxes_test.go - the per-account Halloween box count plane

===========================================================================
*/
package store

import (
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestHalloweenAccountSurvivesSaveLoad

The account count is the cap's memory: a restart mid-event must not
hand the account a second daily allowance (#2547).
================
*/
func TestHalloweenAccountSurvivesSaveLoad(t *testing.T) {
	t.Parallel()

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
	t.Parallel()

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
TestSeededHalloweenPlaneJoinsFirstCommit

The seed is derived at load, not read: its bytes become the disk's
only when a commit writes the halloweenBoxes row. The first commit
after such a load writes the plane even though no settlement
touched it (the exception #3164's plane skip needs); left
unwritten, every later boot re-derives the seed from the surviving
characters, so a character deleted in between permanently shrinks
the account's boxes. The deleting door is the unrelated commit
that proves it.
================
*/
func TestSeededHalloweenPlaneJoinsFirstCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	seed := func(name string, day, count int64, doomed bool) {
		c := seededCharacter()
		c.Name = name
		c.AccountID = "account-a"
		c.HalloweenBoxDay = day
		c.HalloweenBoxCount = count
		if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
			t.Fatal(err)
		}
		if doomed {
			// The ordinary delete: reserve, let the reservation mature.
			s.MutateCharacter(c, "delete-reserve", func() {
				c.DeletePending = true
				c.DeleteReservedAt = clock.Now().UTC().Format(time.RFC3339)
			})
		}
	}
	seed("main", 100, 5, false)
	seed("alt", 100, 3, true)
	seed("stale", 99, 20, false)
	s.Close()

	clock.Advance(DeleteReservationWindow + time.Hour)
	reopened := openTest(t, dir, clock)
	if reaped := reopened.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want the reserved character", reaped)
	}
	// The reap archives through its own fail-closed transaction, not
	// the plane commit; the first ordinary commit after the load is
	// the one that must freeze the seed's row.
	touched := false
	for _, c := range reopened.Characters().CharactersForDivision(testDivision) {
		if c.Name == "main" {
			reopened.MutateCharacter(c, "unrelated touch", func() {})
			touched = true
		}
	}
	if !touched {
		t.Fatal("surviving character not found after reopen")
	}
	reopened.Close()

	third := openTest(t, dir, clock)
	defer third.Close()
	if got := third.meta.HalloweenBoxes["account-a"]; got == nil || got.Day != 100 || got.Count != 8 {
		t.Fatalf("account-a after delete and restart = %+v, want the frozen seed day 100 count 8", got)
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
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	defer s.Close()
	if _, err := s.syncDBLocked().Exec("INSERT INTO meta (key, value) VALUES (?, ?)", metaKeyHalloweenBoxes, `{"account-a":{"day":1,"count":-1}}`); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadHalloweenBoxes(s.syncDBLocked()); err == nil {
		t.Fatal("a negative count loaded")
	}
	if _, err := s.syncDBLocked().Exec("UPDATE meta SET value = ? WHERE key = ?", `{" ":{"day":1,"count":1}}`, metaKeyHalloweenBoxes); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadHalloweenBoxes(s.syncDBLocked()); err == nil {
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
	tier.Long(t, "a schema-35 layout-11 authority upgrades to 36 with its records untouched")
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
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preHalloweenAccountVersion)
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

/*
================
TestHalloweenGourdCountSurvivesSaveLoad

The Gourd allowance is the wave's memory too (#2950): it survives a
restart mid-event, a row written before the field existed (schema 40
and older) decodes with no Gourd boxes minted, and a negative Gourd
count is corruption, like a negative ordinary one.
================
*/
func TestHalloweenGourdCountSurvivesSaveLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	if !s.UpdateCharacter(c, "test halloween gourd mint", func() bool {
		record := s.HalloweenAccount("account-a")
		record.Day, record.Count, record.GourdCount = 20736, 17, 9
		return true
	}) {
		t.Fatal("the mint door refused")
	}
	s.Close()

	reopened := openTest(t, dir, clock)
	defer reopened.Close()
	loaded := reopened.HalloweenAccount("account-a")
	if loaded.Day != 20736 || loaded.Count != 17 || loaded.GourdCount != 9 {
		t.Fatalf("account = %+v, want day 20736 count 17 gourd 9", loaded)
	}

	// A row written before gourdCount existed decodes it as zero.
	if _, err := reopened.db.Exec("DELETE FROM meta WHERE key = ?", metaKeyHalloweenBoxes); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.db.Exec("INSERT INTO meta (key, value) VALUES (?, ?)", metaKeyHalloweenBoxes, `{"account-a":{"day":20736,"count":17}}`); err != nil {
		t.Fatal(err)
	}
	accounts, _, _, err := loadHalloweenBoxes(reopened.db)
	if err != nil {
		t.Fatal(err)
	}
	if got := accounts["account-a"]; got == nil || got.Count != 17 || got.GourdCount != 0 {
		t.Fatalf("old row = %+v, want count 17 gourd 0", got)
	}
	if _, err := reopened.db.Exec("UPDATE meta SET value = ? WHERE key = ?", `{"account-a":{"day":20736,"count":17,"gourdCount":-1}}`, metaKeyHalloweenBoxes); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := loadHalloweenBoxes(reopened.db); err == nil {
		t.Fatal("a negative gourd count loaded")
	}
}
