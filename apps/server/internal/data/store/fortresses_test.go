/*
===========================================================================

fortresses_test.go - the fortress door and its layout 6 upgrade

===========================================================================
*/
package store

import (
	"path/filepath"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestFortressStateSurvivesReopen

An occupation and the war's requests are committed by their saves and read
back after the store reopens; a withdrawn request is gone.
================
*/
func TestFortressStateSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	door := s.Fortresses()
	if err := door.SaveFortress(testDivision, domain.FortressRecord{FortressID: 1, GuildID: 7, TempGuildID: 9}); err != nil {
		t.Fatal(err)
	}
	for _, guild := range []int64{9, 11} {
		if err := door.SaveFortressRequest(testDivision, domain.FortressRequestRecord{FortressID: 1, GuildID: guild}, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := door.SaveFortressRequest(testDivision, domain.FortressRequestRecord{FortressID: 1, GuildID: 11}, false); err != nil {
		t.Fatal(err)
	}
	tower := domain.FortressStructureRecord{FortressID: 1, EventStructID: 85, RefObjID: 19536, OwnerGuildID: 7, HP: 120, State: 1}
	if err := door.SaveFortressStructure(testDivision, tower, true); err != nil {
		t.Fatal(err)
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	records, requests, err := reopened.Fortresses().FortressState(testDivision)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0] != (domain.FortressRecord{FortressID: 1, GuildID: 7, TempGuildID: 9}) {
		t.Fatalf("fortresses %+v", records)
	}
	if len(requests) != 1 || requests[0].GuildID != 9 {
		t.Fatalf("requests %+v", requests)
	}
	structures, err := reopened.Fortresses().FortressStructures(testDivision)
	if err != nil || len(structures) != 1 || structures[0] != tower {
		t.Fatalf("structures %+v (%v)", structures, err)
	}
	if other, _, err := reopened.Fortresses().FortressState("other"); err != nil || len(other) != 0 {
		t.Fatalf("another division sees %+v (%v)", other, err)
	}
}

/*
================
TestLayout5AuthorityGainsTheFortressTables

A schema 16 authority still at layout 5 upgrades to layout 6 with empty
fortress tables and its characters untouched.
================
*/
func TestLayout5AuthorityGainsTheFortressTables(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	if err := s.CreateCharacter(testDivision, "account", seededCharacter()); err != nil {
		t.Fatal(err)
	}
	s.Close()
	db, err := connectDB(filepath.Join(dir, DBFileName))
	if err != nil {
		t.Fatal(err)
	}
	downgradeToLayout5(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if backup, err := UpgradeAuthority(dir, true); err != nil || backup == "" {
		t.Fatal("upgrade", backup, err)
	}
	reopened := openTest(t, dir, newTestClock())
	if len(reopened.Characters().CharactersForDivision(testDivision)) != 1 {
		t.Fatal("character lost")
	}
	if records, requests, err := reopened.Fortresses().FortressState(testDivision); err != nil || len(records)+len(requests) != 0 {
		t.Fatalf("fresh fortress tables %+v %+v (%v)", records, requests, err)
	}
}
