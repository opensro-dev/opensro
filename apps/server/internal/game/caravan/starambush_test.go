/*
===========================================================================

starambush_test.go - tests for starambush.go

===========================================================================
*/
package caravan

import "testing"

/*
================
TestStarAmbushCountIsTheAdvisorTable
================
*/
func TestStarAmbushCountIsTheAdvisorTable(t *testing.T) {
	for stars, want := range map[uint32]int{0: 2, 1: 2, 2: 3, 3: 4, 4: 7, 5: 10, 6: 10, 40: 10} {
		if got := StarAmbushCount(stars); got != want {
			t.Fatalf("%d stars: %d bandits, want %d", stars, got, want)
		}
	}
}

/*
================
TestStarAmbushFromEnv
================
*/
func TestStarAmbushFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "on": true, "TRUE": true, "1": true} {
		t.Setenv(EnvStarAmbush, value)
		if got := StarAmbushFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
