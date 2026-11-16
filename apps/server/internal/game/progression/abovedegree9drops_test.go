/*
===========================================================================

abovedegree9drops_test.go - the switch that keeps drops at degree 9 or below

===========================================================================
*/
package progression

import "testing"

/*
================
TestAboveDegree9DropsSwitch

On, equipment and the degree-banded alchemy goods above degree 9 are
excluded and degree 9 is not; off, nothing is. Etc items whose ItemClass is
not a degree (pet collars up to 17, the class-11 speed potion) never are.
================
*/
func TestAboveDegree9DropsSwitch(t *testing.T) {
	type item struct {
		name    string
		typeIDs [4]int64
		degree  int
		above   bool
	}
	items := []item{
		{"degree-10 sword", [4]int64{3, 1, 6, 2}, 10, true},
		{"degree-11 ring", [4]int64{3, 1, 5, 3}, 11, true},
		{"degree-9 sword", [4]int64{3, 1, 6, 2}, 9, false},
		{"degree-10 Lucky Powder", [4]int64{3, 3, 10, 2}, 10, true},
		{"degree-10 magic stone", [4]int64{3, 3, 11, 1}, 10, true},
		{"degree-10 tablet", [4]int64{3, 3, 11, 3}, 10, true},
		{"degree-9 attribute stone", [4]int64{3, 3, 11, 2}, 9, false},
		{"pet collar, class 17", [4]int64{3, 3, 9, 0}, 17, false},
		{"speed potion, class 11", [4]int64{3, 3, 13, 1}, 11, false},
		{"not an item", [4]int64{1, 2, 1, 1}, 12, false},
	}
	for _, tc := range []struct {
		env string
		on  bool
	}{{"", false}, {"off", false}, {"0", false}, {"on", true}, {"TRUE", true}, {"1", true}} {
		t.Setenv(EnvAboveDegree9DropsOff, tc.env)
		drops := AboveDegree9DropsFromEnv()
		if drops.Off() != tc.on {
			t.Fatalf("%q: off %v, want %v", tc.env, drops.Off(), tc.on)
		}
		for _, it := range items {
			if got := drops.Excludes(it.typeIDs, it.degree); got != (tc.on && it.above) {
				t.Fatalf("%q: %s excluded %v, want %v", tc.env, it.name, got, tc.on && it.above)
			}
		}
	}
}
