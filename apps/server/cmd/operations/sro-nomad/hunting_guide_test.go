/*
===========================================================================

hunting_guide_test.go - the optional atlas setting reaches the GameWorld job

===========================================================================
*/
package main

import "testing"

/*
================
TestHuntingGuideJobVariable

Unset stays off. Explicit values survive deployment unchanged.
================
*/
func TestHuntingGuideJobVariable(t *testing.T) {
	for _, value := range []string{"", "off", "on", "true", "1"} {
		t.Run(value, func(t *testing.T) {
			t.Setenv("SRO_HUNTING_GUIDE", value)
			variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
			if variables["hunting_guide"] != value {
				t.Fatalf("env %q: hunting_guide %v", value, variables["hunting_guide"])
			}
		})
	}
}
