/*
===========================================================================

topmasteries_test.go - the two masteries a party shows for a character

===========================================================================
*/
package domain

import "testing"

/*
================
TestTopMasteries
================
*/
func TestTopMasteries(t *testing.T) {
	cases := []struct {
		name                       string
		masteries                  []CharacterMastery
		wantPrimary, wantSecondary uint32
	}{
		{"none trained", []CharacterMastery{{ID: 257}, {ID: 258}}, 0, 0},
		{"one trained", []CharacterMastery{{ID: 257}, {ID: 258, Level: 4}}, 258, 0},
		{"highest level first", []CharacterMastery{{ID: 257, Level: 3}, {ID: 258, Level: 9}, {ID: 259, Level: 5}}, 258, 259},
		{"ties keep the lower id", []CharacterMastery{{ID: 275, Level: 7}, {ID: 259, Level: 7}, {ID: 273, Level: 7}}, 259, 273},
	}
	for _, c := range cases {
		primary, secondary := TopMasteries(c.masteries)
		if primary != c.wantPrimary || secondary != c.wantSecondary {
			t.Errorf("%s: got %d,%d want %d,%d", c.name, primary, secondary, c.wantPrimary, c.wantSecondary)
		}
	}
}

/*
================
TestTopMasteriesLeavesInputUntouched
================
*/
func TestTopMasteriesLeavesInputUntouched(t *testing.T) {
	masteries := []CharacterMastery{{ID: 257, Level: 1}, {ID: 258, Level: 9}}
	TopMasteries(masteries)
	if masteries[0].ID != 257 || masteries[1].ID != 258 {
		t.Fatalf("input reordered: %+v", masteries)
	}
}
