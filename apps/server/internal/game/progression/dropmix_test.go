/*
===========================================================================

dropmix_test.go - parsing the beta drop mix

===========================================================================
*/
package progression

import (
	"reflect"
	"testing"
)

/*
================
TestParseDropMix
================
*/
func TestParseDropMix(t *testing.T) {
	on := DropMix{"potion": 1, "cure": 1, "scroll": 1, "elixir": 1, "ammo": 1, "stone": 1, "tablet": 1}
	for _, tc := range []struct {
		text string
		want DropMix
		bad  bool
	}{
		{"", nil, false},
		{"off", nil, false},
		{"ON", on, false},
		{" Equipment=5 , potion=1", DropMix{"equipment": 5, "potion": 1}, false},
		{"potion=0", nil, true},
		{"potion=101", nil, true},
		{"gold=1", nil, true},
		{"potion", nil, true},
		{"potion=1,potion=2", nil, true},
		{"potion=x", nil, true},
	} {
		got, err := ParseDropMix(tc.text)
		if (err != nil) != tc.bad || !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%q: %v, %v; want %v (error %v)", tc.text, got, err, tc.want, tc.bad)
		}
	}
}

/*
================
TestDropMixRate

A named family takes its own rate; every other family, and every family
of a nil mix, takes the uniform rate.
================
*/
func TestDropMixRate(t *testing.T) {
	mix := DropMix{"potion": 1}
	if mix.Rate("potion", 20) != 1 || mix.Rate("equipment", 20) != 20 || DropMix(nil).Rate("potion", 20) != 20 {
		t.Fatal("drop mix rates")
	}
}
