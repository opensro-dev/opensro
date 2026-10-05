/*
===========================================================================

skillactionarea_test.go - efr kind 1 recorded on every row

===========================================================================
*/

package enterworld

import (
	"strconv"
	"testing"
)

/*
================
TestActionAreaSurvivesPlayerOffenseRefusal

MSKILL_CH_TIGERWOMAN_ATTACK02's shape: att, mc, then efr kind 1. A monster
row the player offense gate refuses (column 15 set) still records the
action area its attack reads; an unknown shape records none.
================
*/
func TestActionAreaSurvivesPlayerOffenseRefusal(t *testing.T) {
	for _, shape := range []int64{1, 5} {
		fields := make([]string, 118)
		for i := range fields {
			fields[i] = "0"
		}
		fields[0], fields[15] = "1", "4500"
		values := []int64{skillAttackTag, 9, 300, 281, 321, 100, skillMultiImpactTag, 2, 1, tagEfr, 1, shape, 40, 5, 0, 24}
		for i, value := range values {
			fields[69+i] = strconv.FormatInt(value, 10)
		}
		row := SkillRow{CombatPinned: true, TimingPinned: true, ActionRangePinned: true, TargetRequired: true}
		parseSkillOffense(fields, &row)
		if row.OffenseRefusal == "" {
			t.Fatal("the fixture was admitted as a player offense")
		}
		want := SkillOffensiveArea{}
		if shape == 1 {
			want = SkillOffensiveArea{Shape: 1, Radius: 40, MaxTargets: 5, Select: 24}
		}
		if row.ActionArea != want {
			t.Fatalf("shape %d area %+v, want %+v", shape, row.ActionArea, want)
		}
	}
}
