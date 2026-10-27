/*
===========================================================================

sealgroup_test.go - the seal class of the top degree a drop level offers

The guaranteed unique seal names its tier instead of rolling the rare
row: the group is 3*(degree-1) + tier, where the degree is the one the
level's ordinary class row stars with.

===========================================================================
*/
package loot

import "testing"

/*
================
TestSealGroupAtFollowsTheLevelsTopDegree

Degree 9 at the native cap (G24 Star, G25 Moon, G26 Sun), degree 3 at
level 20, degree 10 at level 94, and degree 11 once a cap-110 row puts
class 30 (G30 Star, G31 Moon, G32 Sun) at the top.
================
*/
func TestSealGroupAtFollowsTheLevelsTopDegree(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level uint8
		want  [3]int
	}{
		{"level 20", 20, [3]int{6, 7, 8}},
		{"level 90", 90, [3]int{24, 25, 26}},
		{"level 94", 94, [3]int{27, 28, 29}},
	} {
		for tier, want := range tc.want {
			got, ok := SealGroupAt(tc.level, tier)
			if !ok || got != want {
				t.Fatalf("%s tier %d = %d/%v, want %d", tc.name, tier, got, ok, want)
			}
		}
	}

	before := equipment
	t.Cleanup(func() { equipment = before })
	equipment = topLevelEquipmentCatalog(before, expansionMonsterLevelCap, true)
	// The cap-110 catalog's level-110 row stars at class 30: degree 11's
	// Star bucket, the same shape its own install test uses.
	equipment.classes[0][expansionMonsterLevelCap-1] = []classThreshold{{group: 30, threshold: 1_000_000}}
	for tier, want := range [3]int{30, 31, 32} {
		got, ok := SealGroupAt(expansionMonsterLevelCap, tier)
		if !ok || got != want {
			t.Fatalf("level 110 tier %d = %d/%v, want %d (top class 30)", tier, got, ok, want)
		}
	}
}

/*
================
TestSealGroupAtRejectsAnUnknownTier
================
*/
func TestSealGroupAtRejectsAnUnknownTier(t *testing.T) {
	for _, tier := range []int{-1, 3, 99} {
		if _, ok := SealGroupAt(90, tier); ok {
			t.Fatalf("tier %d accepted", tier)
		}
	}
}
