/*
===========================================================================

portonly_test.go - the port-only switches reach the GameWorld job

Storage auto-stack defaults to on (owner decision 2026-10-11); the reverse
return map defaults to off. A deployer's value, including "off", passes
through unchanged.

===========================================================================
*/
package main

import "testing"

/*
================
TestPortOnlyJobVariables
================
*/
func TestPortOnlyJobVariables(t *testing.T) {
	for _, tc := range []struct {
		stack, reverse         string
		wantStack, wantReverse string
	}{
		{"", "", "on", ""},
		{"off", "on", "off", "on"},
		{"on", "off", "on", "off"},
	} {
		t.Setenv("SRO_STORAGE_AUTO_STACK", tc.stack)
		t.Setenv("SRO_REVERSE_RETURN_MAP", tc.reverse)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["storage_auto_stack"] != tc.wantStack || variables["reverse_return_map"] != tc.wantReverse {
			t.Fatalf("env %q/%q: job variables %v/%v, want %s/%s", tc.stack, tc.reverse,
				variables["storage_auto_stack"], variables["reverse_return_map"], tc.wantStack, tc.wantReverse)
		}
	}
}
