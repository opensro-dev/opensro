/*
===========================================================================

alliances_test.go - the union door

===========================================================================
*/
package store

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestAlliancesSurviveReopen

A union is committed by its save and read back after the store reopens;
a dissolved union is gone, and a record that is not a union is refused.
================
*/
func TestAlliancesSurviveReopen(t *testing.T) {
	dir := t.TempDir()
	s := openTest(t, dir, newTestClock())
	door := s.Alliances()
	union := domain.AllianceRecord{AllianceID: 7, Guilds: [domain.AllianceSlots]int64{7, 9, 0, 11}}
	if err := door.SaveAlliance(testDivision, union, true); err != nil {
		t.Fatal(err)
	}
	dissolved := domain.AllianceRecord{AllianceID: 12, Guilds: [domain.AllianceSlots]int64{12, 13}}
	if err := door.SaveAlliance(testDivision, dissolved, true); err != nil {
		t.Fatal(err)
	}
	if err := door.SaveAlliance(testDivision, dissolved, false); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []domain.AllianceRecord{
		{AllianceID: 14, Guilds: [domain.AllianceSlots]int64{14}},
		{AllianceID: 14, Guilds: [domain.AllianceSlots]int64{0, 14, 9}},
		{AllianceID: 14, Guilds: [domain.AllianceSlots]int64{14, 14}},
		{AllianceID: 14, Guilds: [domain.AllianceSlots]int64{15, 16}},
	} {
		if err := door.SaveAlliance(testDivision, bad, true); err == nil {
			t.Fatalf("saved %+v", bad)
		}
	}
	s.Close()
	reopened := openTest(t, dir, newTestClock())
	unions, err := reopened.Alliances().Alliances(testDivision)
	if err != nil || len(unions) != 1 || unions[0] != union {
		t.Fatalf("unions %+v (%v)", unions, err)
	}
	if other, err := reopened.Alliances().Alliances("other"); err != nil || len(other) != 0 {
		t.Fatalf("another division sees %+v (%v)", other, err)
	}
}
