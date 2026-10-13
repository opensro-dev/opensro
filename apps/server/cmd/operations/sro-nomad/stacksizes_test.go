/*
===========================================================================

stacksizes_test.go - port-only stack caps reach the deployed GameWorld

The deployer's environment must survive job materialization: unset is the
deployment default (Lucky Powder 500), "off" is native, and an invalid
setting is refused before any Nomad mutation.

===========================================================================
*/
package main

import (
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestStackSizesDeployment
================
*/
func TestStackSizesDeployment(t *testing.T) {
	for _, test := range []struct {
		name, value, want string
		invalid           bool
	}{
		// The deployment default (shardsettings, owner 2026-10-13).
		{name: "default", want: "luckypowder=500"},
		{name: "native", value: "off"},
		{name: "raised", value: " Potion=2000 ,elixir=50", want: "elixir=50,potion=2000"},
		{name: "invalid", value: "potion=65536", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(enterworld.EnvStackSizes, test.value)
			resolved, err := resolveDeployment(commandOptions{
				ModuleRoot: filepath.Clean("../../.."),
				Namespace:  "sro",
				Network:    "game-private",
				AgentPort:  8787,
				PrivateNet: true,
			}, false)
			if test.invalid {
				if err == nil || !strings.Contains(err.Error(), enterworld.EnvStackSizes) {
					t.Fatalf("invalid setting: got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := resolved.gameVariables(shardDeployment{})["stack_sizes"]; got != test.want {
				t.Fatalf("stack_sizes = %q, want %q", got, test.want)
			}
		})
	}
}
