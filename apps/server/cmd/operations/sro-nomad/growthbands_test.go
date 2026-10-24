/*
===========================================================================

growthbands_test.go - the beta growth bands are admitted before deployment

Port-only, not native (owner 2026-10-24). The deployment default and an
operator's valid list pass admission; a malformed list is refused before
any Nomad mutation, as the GameWorld refuses it at boot.

===========================================================================
*/
package main

import (
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/progression"
)

/*
================
TestGrowthBandsAdmission
================
*/
func TestGrowthBandsAdmission(t *testing.T) {
	for _, test := range []struct {
		name, value string
		invalid     bool
	}{
		{name: "default"},
		{name: "operator", value: "1-79:pace,80-110:5"},
		{name: "overlap", value: "1-60:pace,60-89:20", invalid: true},
		{name: "past cap", value: "1-120:pace", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(progression.EnvBetaGrowthBands, test.value)
			_, err := resolveDeployment(commandOptions{
				ModuleRoot: filepath.Clean("../../.."),
				Namespace:  "sro",
				Network:    "game-private",
				AgentPort:  8787,
				PrivateNet: true,
			}, false)
			if test.invalid {
				if err == nil || !strings.Contains(err.Error(), progression.EnvBetaGrowthBands) {
					t.Fatalf("invalid bands: got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}
