package enterworld

import (
	"strconv"
	"testing"

	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
deferredVitalFields

A 118-field row whose encoded tail is the given program words.
================
*/
func deferredVitalFields(program ...int64) []string {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	for i, n := range program {
		fields[69+i] = strconv.FormatInt(n, 10)
	}
	return fields
}

/*
================
TestParseSkillDeferredVital

Only a pmhp block in the deferred mode (word 4 = 0) projects: mode 2 is
the timed installer's, kind 1 has no carrier, and a later block wins.
================
*/
func TestParseSkillDeferredVital(t *testing.T) {
	const pmhp = int64(0x706d6870)
	for _, tc := range []struct {
		name    string
		program []int64
		want    SkillDeferredVital
	}{
		{"bomb block", []int64{pmhp, 1000, 0, 100, 0}, SkillDeferredVital{Present: true, DurationMs: 1000, Percent: 100}},
		{"mode two stays the installer's", []int64{pmhp, 900000, 0, 50, 2}, SkillDeferredVital{}},
		{"kind one unprojected", []int64{pmhp, 1000, 30, 0, 1}, SkillDeferredVital{}},
		{"no block", []int64{}, SkillDeferredVital{}},
		{"last block wins", []int64{pmhp, 1000, 0, 100, 0, pmhp, 2000, 5, 40, 0},
			SkillDeferredVital{Present: true, DurationMs: 2000, Base: 5, Percent: 40}},
	} {
		row := SkillRow{}
		parseSkillDeferredVital(deferredVitalFields(tc.program...), &row)
		if row.DeferredVital != tc.want {
			t.Fatalf("%s: got %+v, want %+v", tc.name, row.DeferredVital, tc.want)
		}
	}
}

/*
================
TestShippedDeferredVitalRows

Of the shipped v1.150 rows exactly one carries a deferred vital: the
anti-bot bomb's attack (skill 3032), at its whole current HP.
================
*/
func TestShippedDeferredVitalRows(t *testing.T) {
	clientdata.RequireGameData(t)
	skills := NewTextdataSkills(gamedatatest.TextdataDir(t))
	row, ok := skills.SkillByID(3032)
	if !ok {
		t.Fatal("MSKILL_AUTOMOB_ATTACK01 did not load")
	}
	if want := (SkillDeferredVital{Present: true, DurationMs: 1000, Percent: 100}); row.DeferredVital != want {
		t.Fatalf("bomb deferred = %+v, want %+v", row.DeferredVital, want)
	}
	for _, other := range skills.rows.values() {
		if other.ID != 3032 && other.DeferredVital.Present {
			t.Fatalf("row %d (%s) unexpectedly carries %+v", other.ID, other.Codename, other.DeferredVital)
		}
	}
}
