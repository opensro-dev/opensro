/*
===========================================================================

onboarding_test.go - /title/onboarding through the Agent's real handler

===========================================================================
*/
package agentserver

import (
	"encoding/json"
	"net/http"
	"testing"
)

/*
================
TestOnboardingFollowsTheDeployerSwitch

The tour is on by default; Config.OnboardingOff turns it off.
================
*/
func TestOnboardingFollowsTheDeployerSwitch(t *testing.T) {
	for _, off := range []bool{false, true} {
		worker := http.NotFoundHandler()
		f := newAgentFixture(t, worker, worker, func(config *Config) {
			config.OnboardingOff = off
		})
		recorder := performJSON(t, f.handler, http.MethodGet, onboardingPath, "", "")
		var body struct {
			OK         bool `json:"ok"`
			Onboarding struct {
				Enabled bool `json:"enabled"`
			} `json:"onboarding"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if recorder.Code != http.StatusOK || !body.OK || body.Onboarding.Enabled == off {
			t.Fatalf("OnboardingOff=%v: %d %s", off, recorder.Code, recorder.Body.String())
		}
	}
}
