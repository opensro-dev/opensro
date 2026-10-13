/*
===========================================================================

portonly_test.go - the port-only switches reach the GameWorld job

Storage auto-stack and the alchemy rates default to on (owner decisions
2026-10-11); /SETSPEED defaults to on (2026-10-12); the enhancement cap defaults to on (2026-10-13); the team-death revive defaults to on, native (2026-10-13); the reverse return map defaults to off. A deployer's value, including "off", passes
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
		stack, reverse, rates, speed, cap                     string
		wantStack, wantReverse, wantRates, wantSpeed, wantCap string
	}{
		{"", "", "", "", "", "on", "", "on", "on", "on"},
		{"off", "on", "off", "off", "off", "off", "on", "off", "off", "off"},
		{"on", "off", "100,50", "on", "9", "on", "off", "100,50", "on", "9"},
	} {
		t.Setenv("SRO_STORAGE_AUTO_STACK", tc.stack)
		t.Setenv("SRO_REVERSE_RETURN_MAP", tc.reverse)
		t.Setenv("SRO_ALCHEMY_RATES", tc.rates)
		t.Setenv("SRO_GM_SETSPEED", tc.speed)
		t.Setenv("SRO_ALCHEMY_MAX_PLUS", tc.cap)
		t.Setenv("SRO_TEAM_DEATH_REVIVE", tc.cap)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		// The team-death revive passes the same values: "" defaults to on.
		if variables["team_death_revive"] != tc.wantCap {
			t.Fatalf("env %q: team_death_revive %v, want %s", tc.cap, variables["team_death_revive"], tc.wantCap)
		}
		if variables["alchemy_max_plus"] != tc.wantCap {
			t.Fatalf("env %q: alchemy_max_plus %v, want %s", tc.cap, variables["alchemy_max_plus"], tc.wantCap)
		}
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
