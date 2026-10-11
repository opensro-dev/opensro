/*
===========================================================================

portonly_test.go - the port-only switches reach the GameWorld job

Storage auto-stack and the alchemy rates default to on (owner decisions
2026-10-11); /SETSPEED defaults to on (2026-10-12); the reverse return map defaults to off. A deployer's value, including "off", passes
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
		stack, reverse, rates, speed                 string
		wantStack, wantReverse, wantRates, wantSpeed string
	}{
		{"", "", "", "", "on", "", "on", "on"},
		{"off", "on", "off", "off", "off", "on", "off", "off"},
		{"on", "off", "100,50", "on", "on", "off", "100,50", "on"},
	} {
		t.Setenv("SRO_STORAGE_AUTO_STACK", tc.stack)
		t.Setenv("SRO_REVERSE_RETURN_MAP", tc.reverse)
		t.Setenv("SRO_ALCHEMY_RATES", tc.rates)
		t.Setenv("SRO_GM_SETSPEED", tc.speed)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["gm_setspeed"] != tc.wantSpeed {
			t.Fatalf("env %q: gm_setspeed %v, want %s", tc.speed, variables["gm_setspeed"], tc.wantSpeed)
		}
		if variables["storage_auto_stack"] != tc.wantStack || variables["reverse_return_map"] != tc.wantReverse ||
			variables["alchemy_rates"] != tc.wantRates {
			t.Fatalf("env %q/%q/%q: job variables %v/%v/%v, want %s/%s/%s", tc.stack, tc.reverse, tc.rates,
				variables["storage_auto_stack"], variables["reverse_return_map"], variables["alchemy_rates"],
				tc.wantStack, tc.wantReverse, tc.wantRates)
		}
	}
}
