/*
===========================================================================

unique_kills_test.go - tests for unique_kills.go and the schema 22 step

===========================================================================
*/
package store

import (
	"errors"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCommunityUpgradeAddsTheUniqueKillTable

A schema 21 authority at layout 8 upgrades to 22 with an empty unique-kill
table and its records untouched; a second upgrade has nothing to do.
================
*/
func TestCommunityUpgradeAddsTheUniqueKillTable(t *testing.T) {
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
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE unique_kills"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	rewriteDatabaseMeta(t, dir, metaKeyLayoutVersion, preUniqueKillLayoutVersion)
	rewriteDatabaseMeta(t, dir, metaKeySchemaVersion, preCommunityVersion)
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
	kills, err := reopened.UniqueKillsAfter(testDivision, 0)
	if err != nil || len(kills) != 0 {
		t.Fatalf("fresh unique-kill table %+v %v", kills, err)
	}
	reopened.Close()
	if _, err := UpgradeAuthority(dir, true); !errors.Is(err, ErrAuthorityCurrent) {
		t.Fatalf("second upgrade = %v, want ErrAuthorityCurrent", err)
	}
}

/*
================
TestUniqueKillsAppendInOrderAndSurviveReopen

Each kill takes the division's next sequence; a reopen validates and reads
them back oldest first, and a reader holding a sequence gets only newer ones.
================
*/
func TestUniqueKillsAppendInOrderAndSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	for i, kill := range []domain.UniqueKill{
		{AtMs: 1000, RefObjID: 1954, KillerCharID: 7, KillerName: "Kekw"},
		{AtMs: 2000, RefObjID: 1982, KillerCharID: 8, KillerName: "Lune"},
	} {
		recorded, err := s.RecordUniqueKill(testDivision, kill)
		if err != nil || recorded.Seq != int64(i+1) {
			t.Fatalf("kill %d recorded as %+v: %v", i, recorded, err)
		}
	}
	if _, err := s.RecordUniqueKill(testDivision, domain.UniqueKill{AtMs: 3000}); err == nil {
		t.Fatal("a kill without a unique was recorded")
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	defer reopened.Close()
	all, err := reopened.UniqueKillsAfter(testDivision, 0)
	if err != nil || len(all) != 2 || all[0].KillerName != "Kekw" || all[1].Seq != 2 {
		t.Fatalf("kills after reopen %+v %v", all, err)
	}
	recent, err := reopened.UniqueKillsAfter(testDivision, 1)
	if err != nil || len(recent) != 1 || recent[0].RefObjID != 1982 {
		t.Fatalf("kills after sequence 1 %+v %v", recent, err)
	}
}

/*
================
TestLevelReachedRecordsOnlyCrossedMilestones

A gain records the milestones it crossed; a character that predates the
record never claims a lower milestone, and a recorded time is kept.
================
*/
func TestLevelReachedRecordsOnlyCrossedMilestones(t *testing.T) {
	c := &domain.Character{}
	if !c.RecordLevelReached(18, 31, 500) || c.LevelReachedAt[20] != 500 || c.LevelReachedAt[30] != 500 || len(c.LevelReachedAt) != 2 {
		t.Fatalf("18 -> 31 recorded %v", c.LevelReachedAt)
	}
	if c.RecordLevelReached(31, 39, 600) {
		t.Fatal("31 -> 39 crossed no milestone")
	}
	old := &domain.Character{}
	if old.RecordLevelReached(80, 81, 700) || len(old.LevelReachedAt) != 0 {
		t.Fatalf("an existing level 80 claimed %v", old.LevelReachedAt)
	}
	c.LevelReachedAt[40] = 650
	c.RecordLevelReached(35, 41, 900)
	if c.LevelReachedAt[40] != 650 {
		t.Fatal("a recorded milestone time was overwritten")
	}
}
