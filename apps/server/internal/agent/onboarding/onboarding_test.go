/*
===========================================================================

onboarding_test.go - SRO_ONBOARDING defaults on and turns off by name

===========================================================================
*/
package onboarding

import "testing"

/*
================
TestFromEnv
================
*/
func TestFromEnv(t *testing.T) {
	for raw, want := range map[string]bool{
		"": true, "1": true, "on": true, "yes": true,
		"off": false, "OFF": false, " 0 ": false, "false": false,
	} {
		if got := FromEnv(func(string) string { return raw }); got != want {
			t.Fatalf("%s=%q: %v, want %v", Env, raw, got, want)
		}
	}
}
