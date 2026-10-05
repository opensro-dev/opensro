/*
===========================================================================

onboarding_test.go - SRO_ONBOARDING reaches the Agent job

===========================================================================
*/
package main

import "testing"

/*
================
TestOnboardingJobVariable
================
*/
func TestOnboardingJobVariable(t *testing.T) {
	for enabled, want := range map[bool]string{true: "1", false: "0"} {
		deployment := &deployment{Onboarding: enabled}
		if got := deployment.agentVariables()["onboarding"]; got != want {
			t.Fatalf("Onboarding=%v: job variable %v, want %s", enabled, got, want)
		}
	}
}
