/*
===========================================================================

partymasteries_test.go - SRO_PARTY_MASTERIES reaches the GameWorld job

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
	for enabled, want := range map[bool]string{true: "1", false: "0"} {
		deployment := &deployment{PartyMasteries: enabled}
		if got := deployment.gameVariables(shardDeployment{})["party_masteries"]; got != want {
			t.Fatalf("PartyMasteries=%v: job variable %v, want %s", enabled, got, want)
		}
	}
}
