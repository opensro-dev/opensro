/*
===========================================================================

jobsuitarmor_test.go - tests for jobsuitarmor.go

===========================================================================
*/
package action

import "testing"

/*
================
TestJobSuitHidesArmorFromEnv
================
*/
func TestJobSuitHidesArmorFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvJobSuitHidesArmor, value)
		if got := JobSuitHidesArmorFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
