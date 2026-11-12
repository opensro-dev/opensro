/*
===========================================================================

sookarenaaccounts_test.go - the per-account survival arena entry plane

===========================================================================
*/
package store

import (
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestSookArenaAccountSurvivesSaveLoad

The account count is the daily allowance's memory: a restart must not
hand the account three fresh entries (#3322).
================
*/
func TestSookArenaAccountSurvivesSaveLoad(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	c := seededCharacter()
	if err := s.CreateCharacter(testDivision, "account-a", c); err != nil {
		t.Fatal(err)
	}
	// One entry-style door: the record moves inside it and the commit
	// stages the plane (persistence.go).
	if !s.UpdateCharacter(c, "test arena entry", func() bool {
		record := s.SookArenaAccount("account-a")
		record.Day, record.Entries = 20769, 2
		return true
	}) {
		t.Fatal("the entry door refused")
	}
	s.Close()

	reopened := openTest(t, dir, clock)
	defer reopened.Close()
	if got := reopened.SookArenaAccount("account-a"); got.Day != 20769 || got.Entries != 2 {
		t.Fatalf("account = %+v, want day 20769 entries 2", got)
	}
	if got := reopened.SookArenaAccount("account-b"); got.Day != 0 || got.Entries != 0 {
		t.Fatalf("untouched account = %+v, want zero", got)
	}
}

/*
================
TestSookArenaAccountsSeedFromLegacyCounts

An authority with no sookArenaAccounts row counted per character
(schema 42): at first load the account record is the latest day's
entries summed over the account's characters, saturated at the daily
allowance, so no alt keeps entries the account already spent. An
older day and a character that never entered seed nothing.
================
*/
func TestSookArenaAccountsSeedFromLegacyCounts(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	seed := func(name, account string, day int32, entries uint8) {
		c := seededCharacter()
		c.Name = name
		c.AccountID = account
		c.SookArenaDay = day
		c.SookArenaEntries = entries
		if err := s.CreateCharacter(testDivision, account, c); err != nil {
			t.Fatal(err)
		}
	}
	seed("main", "account-a", 100, 1)
	seed("alt", "account-a", 100, 1)
	seed("stale", "account-a", 99, 3)
	seed("spent", "account-b", 100, 3)
	seed("spentalt", "account-b", 100, 2)
	seed("fresh", "account-c", 100, 0)
	s.Close()

	reopened := openTest(t, dir, clock)
	defer reopened.Close()
	want := map[string]domain.SookArenaAccount{
		"account-a": {Day: 100, Entries: 2},
		"account-b": {Day: 100, Entries: domain.SookArenaDailyEntries},
		"account-c": {},
	}
	for account, expected := range want {
		if got := reopened.SookArenaAccount(account); *got != expected {
			t.Fatalf("%s seeded = %+v, want %+v", account, *got, expected)
		}
	}
}

/*
================
TestSeededSookArenaPlaneJoinsFirstCommit

The seed is derived at load, not read: its bytes become the disk's
only when a commit writes the sookArenaAccounts row. The first commit
after such a load writes the plane even though no entry touched it;
left unwritten, every later boot re-derives the seed from the
surviving characters, so a character deleted in between would hand
its account back the entries it spent.
================
*/
func TestSeededSookArenaPlaneJoinsFirstCommit(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	clock := newTestClock()
	s := openTest(t, dir, clock)
	seed := func(name string, entries uint8, doomed bool) {
		c := seededCharacter()
		c.Name = name
		c.AccountID = "account-a"
		c.SookArenaDay = 100
		c.SookArenaEntries = entries
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
	seed("main", 1, false)
	seed("alt", 2, true)
	s.Close()

	clock.Advance(DeleteReservationWindow + time.Hour)
	reopened := openTest(t, dir, clock)
	if reaped := reopened.ReapMaturedDeletions(); len(reaped) != 1 {
		t.Fatalf("reaped %v, want the reserved character", reaped)
	}
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
	if got := third.meta.SookArenaAccounts["account-a"]; got == nil || got.Day != 100 || got.Entries != 3 {
		t.Fatalf("account-a after delete and restart = %+v, want the frozen seed day 100 entries 3", got)
	}
}

/*
================
TestSookArenaAccountsRefuseCorruptRows

A count past the daily allowance, a negative day or a blank account
key is corruption, not a reason to grant or forget entries: the load
refuses it, like the Halloween plane.
================
*/
func TestSookArenaAccountsRefuseCorruptRows(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	defer s.Close()
	if _, err := s.syncDBLocked().Exec("INSERT INTO meta (key, value) VALUES (?, ?)", metaKeySookArenaAccounts, `{"account-a":{"day":1,"entries":3}}`); err != nil {
		t.Fatal(err)
	}
	if accounts, present, err := loadSookArenaAccounts(s.syncDBLocked()); err != nil || !present || accounts["account-a"].Entries != 3 {
		t.Fatalf("a spent account = %+v present %v: %v", accounts, present, err)
	}
	for _, row := range []string{
		`{"account-a":{"day":1,"entries":4}}`,
		`{"account-a":{"day":-1,"entries":1}}`,
		`{" ":{"day":1,"entries":1}}`,
		`{"account-a":{"day":1,"entries":1,"spare":true}}`,
	} {
		if _, err := s.syncDBLocked().Exec("UPDATE meta SET value = ? WHERE key = ?", row, metaKeySookArenaAccounts); err != nil {
			t.Fatal(err)
		}
		if _, _, err := loadSookArenaAccounts(s.syncDBLocked()); err == nil {
			t.Fatalf("corrupt row %s loaded", row)
		}
	}
}

/*
================
TestSookArenaAccountUpgradeSeedsFromSchema42

A schema 42 authority upgrades to 43 with its records untouched, and
its first load derives the account count from the per-character
entries it saved; a second upgrade has nothing to do.
================
*/
func TestSookArenaAccountUpgradeSeedsFromSchema42(t *testing.T) {
	tier.Long(t, "a schema-42 authority upgrades to 43 and seeds the arena account plane")
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	c := seededCharacter()
	c.AccountID = "account"
	c.SookArenaDay, c.SookArenaEntries = 20769, 2
	if err := s.CreateCharacter(testDivision, "account", c); err != nil {
		t.Fatal(err)
	}
	var before string
	if err := s.syncDBLocked().QueryRow("SELECT record FROM characters WHERE division = ? AND id = ?", testDivision, c.ID).Scan(&before); err != nil {
		t.Fatal(err)
	}
	s.Close()
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preSookArenaAccountVersion)
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
	if got := reopened.SookArenaAccount("account"); got.Day != 20769 || got.Entries != 2 {
		t.Fatalf("seeded account = %+v, want day 20769 entries 2", got)
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}
