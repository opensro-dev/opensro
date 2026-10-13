/*
===========================================================================

sealrates_test.go - tests for sealrates.go

===========================================================================
*/

package progression

import (
	"reflect"
	"testing"
)

/*
================
TestBetaSealRatesFromEnv

Off (unset, "off", anything else) is every multiplier 1; on is the owner's
table, by ItemClass: three classes per degree, Star, Moon, Sun.
================
*/
func TestBetaSealRatesFromEnv(t *testing.T) {
	for _, value := range []string{"", "off", "0", "yes"} {
		t.Setenv(EnvBetaSealRates, value)
		rates := BetaSealRatesFromEnv()
		if rates.Enabled() || rates.Widening() != 1 || rates.Table() != nil {
			t.Fatalf("%q: %+v", value, rates)
		}
		for class := 0; class <= 28; class++ {
			if rates.Multiplier(class) != 1 {
				t.Fatalf("%q: class %d multiplier %d", value, class, rates.Multiplier(class))
			}
		}
	}
	t.Setenv(EnvBetaSealRates, "on")
	rates := BetaSealRatesFromEnv()
	if !rates.Enabled() || rates.Widening() != 5 {
		t.Fatalf("on: %+v", rates)
	}
	want := [][]int{{3, 3, 3}, {3, 3, 3}, {3, 3, 3}, {3, 3, 3}, {3, 3, 3}, {3, 3, 3}, {3, 3, 3}, {3, 3, 3}, {3, 5, 1}}
	if !reflect.DeepEqual(rates.Table(), want) {
		t.Fatalf("table %v", rates.Table())
	}
	for class, m := range map[int]int{1: 3, 3: 3, 24: 3, 25: 3, 26: 5, 27: 1, 0: 1, 28: 1} {
		if rates.Multiplier(class) != m {
			t.Fatalf("class %d multiplier %d, want %d", class, rates.Multiplier(class), m)
		}
	}
}
