/*
===========================================================================

partymasteries_test.go - SRO_PARTY_MASTERIES reaches the GameWorld job

The GameWorld treats an unset SRO_PARTY_MASTERIES as native (no mastery
pair); the deployment default (shardsettings) keeps production's pair on,
and the operator's value wins.

===========================================================================
*/
package main

import "testing"

/*
================
TestPartyMasteriesJobVariable
================
*/
func TestPartyMasteriesJobVariable(t *testing.T) {
	for raw, want := range map[string]string{"": "on", "off": "off", "on": "on"} {
		t.Setenv("SRO_PARTY_MASTERIES", raw)
		deployment := &deployment{PortOnly: portOnlySettingsFromEnv()}
		if got := deployment.gameVariables(shardDeployment{})["party_masteries"]; got != want {
			t.Fatalf("SRO_PARTY_MASTERIES=%q: job variable %v, want %s", raw, got, want)
		}
	}
}
