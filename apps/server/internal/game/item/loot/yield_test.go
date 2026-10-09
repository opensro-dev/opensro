/*
===========================================================================

yield_test.go - the class-table rates the ISRO-R merge ships (#461)

The generator takes each level's whole class row from the first source that
has one: ISRO-R, then vSRO (rare stays vSRO). These pin the last admitted
class roll (of 10^6) at the levels the issue measured, so a regenerated
catalog that drops back to the thinner vSRO rows fails here, not in play.

===========================================================================
*/
package loot

import (
	"strings"
	"testing"
)

/*
================
TestEquipmentClassRatesPerLevel

Level 80 admits 0.533% (vSRO was 0.053%); level 60 has no ISRO-R row and
keeps the vSRO 0.180%.
================
*/
func TestEquipmentClassRatesPerLevel(t *testing.T) {
	for _, tc := range []struct {
		level uint8
		last  uint32
	}{
		{60, 1797}, {74, 7118}, {80, 5330}, {88, 3851},
	} {
		if _, ok := EquipmentGroup(tc.level, false, tc.last); !ok {
			t.Fatalf("level %d: roll %d no longer admitted", tc.level, tc.last)
		}
		if _, ok := EquipmentGroup(tc.level, false, tc.last+1); ok {
			t.Fatalf("level %d: roll %d admitted past the table", tc.level, tc.last+1)
		}
	}
}

/*
================
TestStoneClassRateAtLevel80

The magic and attribute stone families admit 2.55% at level 80 (vSRO was
0.037%): 2% degree 8, then 0.55% degree 9, both stones the v1.150 client has.
================
*/
func TestStoneClassRateAtLevel80(t *testing.T) {
	const firstDegree9, last = 20000, 25518
	for family, prefix := range map[int]string{8: "ITEM_ETC_ARCHEMY_MAGICSTONE_", 9: "ITEM_ETC_ARCHEMY_ATTRSTONE_"} {
		for _, tc := range []struct {
			roll   uint32
			degree string
		}{
			{0, "_08"}, {firstDegree9 - 1, "_08"}, {firstDegree9, "_09"}, {last, "_09"},
		} {
			got, ok := SelectConsumable(family, 80, tc.roll, zeroRoll)
			if !ok || !strings.HasPrefix(got.Codename, prefix) || !strings.HasSuffix(got.Codename, tc.degree) {
				t.Fatalf("family %d roll %d: %+v/%v, want degree %s", family, tc.roll, got, ok, tc.degree)
			}
		}
		if _, ok := SelectConsumable(family, 80, last+1, zeroRoll); ok {
			t.Fatalf("family %d: roll %d admitted past the table", family, last+1)
		}
	}
}
