/*
===========================================================================

moonlitthiefsuit_test.go - tests for moonlitthiefsuit.go

===========================================================================
*/
package action

import "testing"

/*
================
TestMoonlitThiefSuitFromEnv
================
*/
func TestMoonlitThiefSuitFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true, "1": true, "TRUE": true} {
		t.Setenv(EnvMoonlitThiefSuit, value)
		if got := MoonlitThiefSuitFromEnv(); got != want {
			t.Fatalf("%q: %v, want %v", value, got, want)
		}
	}
}
