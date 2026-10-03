/*
===========================================================================
betamastery_test.go - durable beta policy and job publication
===========================================================================
*/
package main

import (
	"os"
	"path/filepath"
	"testing"
)

/*
================
TestConfiguredBetaMastery
================
*/
func TestConfiguredBetaMastery(t *testing.T) {
	dir := t.TempDir()
	if got, err := configuredBetaMastery(dir); err != nil || got != "on" {
		t.Fatalf("default: %q %v", got, err)
	}
	for _, value := range []string{"on", "off", " OFF\n", "", "typo"} {
		if err := os.WriteFile(filepath.Join(dir, betaMasteryPolicyFile), []byte(value), 0600); err != nil {
			t.Fatal(err)
		}
		got, err := configuredBetaMastery(dir)
		if value == "" || value == "typo" {
			if err == nil {
				t.Fatal("invalid policy accepted")
			}
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		want := "off"
		if value == "on" {
			want = "on"
		}
		deployment := &deployment{BetaMastery: got}
		if actual := deployment.gameVariables(shardDeployment{})["beta_mastery"]; actual != want {
			t.Fatalf("job policy %v, want %s", actual, want)
		}
	}
}
