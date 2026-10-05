/*
===========================================================================

skillimbue_test.go - imbue skill programs

The shipped fire force program is admitted whole; partial or unknown imbue
programs are refused.

===========================================================================
*/
package enterworld

import (
	"strings"
	"testing"

	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestShippedFireForceWholeProgram
================
*/
func TestShippedFireForceWholeProgram(t *testing.T) {
	licensed.RequireGameData(t)
	dir := licensed.RetailTextdataDir(t)
	source := NewTextdataSkills(dir)
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range source.rows.values() {
		if strings.HasPrefix(row.Codename, "SKILL_CH_FIRE_GIGONGTA_") {
			count++
			if !row.Imbue.Pinned || row.Imbue.Attack.Flags != 11 || row.EffectDurationMs == 0 || row.ContinueBasicAttack {
				t.Fatal("Fire Force rank missing", row)
			}
		}
	}
	if count != 40 {
		t.Fatal("Fire Force census", count)
	}
	for _, id := range []uint32{3, 6, 7, 8} {
		if !source.rows.get(id).ContinueBasicAttack {
			t.Fatal("native continuation flag missing", id)
		}
	}
}

/*
================
TestImbueRefusesPartialAndUnknownPrograms
================
*/
func TestImbueRefusesPartialAndUnknownPrograms(t *testing.T) {
	fields := make([]string, 118)
	for i := range fields {
		fields[i] = "0"
	}
	for col, value := range map[int]string{0: "1", 8: "1", 18: "1", 50: "255", 51: "255", 68: "3", 69: "1685418593", 70: "5000", 71: "6386804", 72: "8", 73: "100", 74: "16", 75: "26", 76: "100", 77: "25205", 78: "30", 79: "25", 80: "1", 81: "1734702198", 82: "1296122196"} {
		fields[col] = value
	}
	row := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}}
	parseSkillImbue(fields, &row)
	if !row.Imbue.Pinned || row.Imbue.Burn != (SkillBurn{30, 25, 1}) {
		t.Fatal(row)
	}
	for col, value := range map[int]string{12: "1", 19: "1", 22: "1", 50: "2", 68: "2", 72: "4", 76: "50", 78: "65536", 79: "101", 80: "141", 81: "0", 82: "1", 83: "99", 117: "1"} {
		copyFields := append([]string(nil), fields...)
		copyFields[col] = value
		candidate := SkillRow{TimingPinned: true, Consumption: SkillConsumption{Pinned: true}}
		parseSkillImbue(copyFields, &candidate)
		if candidate.Imbue.Pinned {
			t.Fatal("unrecovered branch admitted", col, value)
		}
	}
}

/*
================
TestBasicAttackResumesOnlyOnOne

CGCharAutoCommandActor_Handler_SkillCast compares ref +0x90 with exactly
1 at 4AED19: Strong Bow (1) resumes the basic attack, Cold Wave (0) and a
row authoring 2 (Sword Geomgi D) end it.
================
*/
func TestBasicAttackResumesOnlyOnOne(t *testing.T) {
	licensed.RequireGameData(t)
	source := NewTextdataSkills(licensed.RetailTextdataDir(t))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[uint32]bool{87: true, 97: false, 18896: false} {
		if got := source.rows.get(id).ContinueBasicAttack; got != want {
			t.Errorf("skill %d resumes the basic attack = %v, want %v", id, got, want)
		}
	}
}
