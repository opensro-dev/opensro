/*
===========================================================================

beta_settings_test.go - beta settings survive validated job materialization

Exercises deployment resolution and Nomad's JSON variable boundary without
live jobs or licensed assets. Invalid beta values must fail before catalog
or asset discovery, even when their gameplay feature is switched off.

===========================================================================
*/

package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/action"
)

/*
================
clearShardSettingsEnv

Deployment tests choose every setting explicitly rather than inheriting
the operator's shell or a durable mastery policy from the shared cluster.
================
*/
func clearShardSettingsEnv(t *testing.T) {
	t.Helper()
	for _, setting := range shardsettings.Settings {
		t.Setenv(setting.Env, "")
	}
}

/*
================
betaSettingsOptions
================
*/
func betaSettingsOptions(t *testing.T) commandOptions {
	t.Helper()
	return commandOptions{
		ModuleRoot: filepath.Clean("../../.."),
		StateDir:   t.TempDir(),
		Namespace:  "sro",
		Network:    "game-private",
		AgentPort:  8787,
		PrivateNet: true,
	}
}

/*
================
betaSettingsVariables

Checks the same resolved values serialized for the Nomad job request.
================
*/
func betaSettingsVariables(t *testing.T) map[string]any {
	t.Helper()
	resolved, err := resolveDeployment(betaSettingsOptions(t), false)
	if err != nil {
		t.Fatalf("resolveDeployment: %v", err)
	}
	payload, err := jobVariablesJSON(resolved.gameVariables(shardDeployment{}))
	if err != nil {
		t.Fatal(err)
	}
	var variables map[string]any
	if err := json.Unmarshal([]byte(payload), &variables); err != nil {
		t.Fatalf("decode job variables: %v", err)
	}
	return variables
}

/*
================
TestBetaSettingsDeploymentDefaults
================
*/
func TestBetaSettingsDeploymentDefaults(t *testing.T) {
	clearShardSettingsEnv(t)
	variables := betaSettingsVariables(t)
	want := map[string]string{
		"beta_starter_kit":       "on",
		"beta_player_map":        "on",
		"beta_mastery":           "on",
		"beta_silk":              "50",
		"beta_growth":            "on",
		"beta_skill_exp_rate":    "100",
		"beta_drop_rate":         "20",
		"beta_drop_mix":          "on",
		"beta_drop_cap":          "16",
		"beta_gold_rate":         "100",
		"beta_rare_rate":         "5",
		"beta_shop_only_degree9": "on",
		"beta_seal_rates":        "on",
		"beta_unique_respawn":    "on",
	}
	for name, expected := range want {
		if got := variables[name]; got != expected {
			t.Errorf("%s = %#v, want %q", name, got, expected)
		}
	}
}

/*
================
TestBetaSettingsDeploymentOff
================
*/
func TestBetaSettingsDeploymentOff(t *testing.T) {
	clearShardSettingsEnv(t)
	for _, setting := range shardsettings.Settings {
		if strings.HasPrefix(setting.Env, "SRO_BETA_") &&
			setting.Source == shardsettings.FromTable &&
			setting.Validation == shardsettings.ValueSwitch {
			t.Setenv(setting.Env, "off")
		}
	}
	t.Setenv("SRO_BETA_DROP_MIX", "off")
	t.Setenv("SRO_BETA_SILK", "off")
	variables := betaSettingsVariables(t)
	for _, name := range []string{
		"beta_starter_kit", "beta_player_map", "beta_growth", "beta_drop_mix",
		"beta_silk", "beta_shop_only_degree9", "beta_seal_rates", "beta_unique_respawn",
	} {
		if got := variables[name]; got != "off" {
			t.Errorf("%s = %#v, want explicit off", name, got)
		}
	}
	if got := variables["beta_mastery"]; got != "on" {
		t.Fatalf("durable mastery default = %#v, want on", got)
	}
}

/*
================
TestBetaSettingsDeploymentNumericBoundaries
================
*/
func TestBetaSettingsDeploymentNumericBoundaries(t *testing.T) {
	for _, test := range []struct {
		env, variable string
		accepted      []string
		refused       []string
	}{
		{"SRO_BETA_SKILL_EXP_RATE", "beta_skill_exp_rate", []string{"1", "9223372036854775807"},
			[]string{"0", "-1", "9223372036854775808", "typo"}},
		{"SRO_BETA_DROP_RATE", "beta_drop_rate", []string{"1", "100"},
			[]string{"0", "101", "999999999999999999999", "typo"}},
		{"SRO_BETA_DROP_CAP", "beta_drop_cap", []string{"0", "1000"},
			[]string{"-1", "1001", "999999999999999999999", "typo"}},
		{"SRO_BETA_GOLD_RATE", "beta_gold_rate", []string{"1", "10000"},
			[]string{"0", "10001", "999999999999999999999", "typo"}},
		{"SRO_BETA_RARE_RATE", "beta_rare_rate", []string{"1", "1000"},
			[]string{"0", "1001", "999999999999999999999", "typo"}},
		{"SRO_BETA_SILK", "beta_silk", []string{"1", "100000"},
			[]string{"-1", "100001", "4294967296", "typo"}},
	} {
		t.Run(test.env, func(t *testing.T) {
			for _, value := range test.accepted {
				t.Run("accept "+value, func(t *testing.T) {
					clearShardSettingsEnv(t)
					t.Setenv(test.env, " \t"+value+"\n")
					variables := betaSettingsVariables(t)
					if got := variables[test.variable]; got != value {
						t.Fatalf("%s = %#v, want trimmed %q", test.variable, got, value)
					}
					if test.env == "SRO_BETA_SILK" && value == "1" {
						rate, err := action.ParseBetaSilk(variables[test.variable].(string))
						if err != nil || rate != 1 {
							t.Fatalf("silk 1 = %d, %v; want one silk per hour", rate, err)
						}
					}
				})
			}
			for _, value := range test.refused {
				t.Run("refuse "+value, func(t *testing.T) {
					clearShardSettingsEnv(t)
					t.Setenv("SRO_BETA_GROWTH", "off")
					t.Setenv(test.env, value)
					_, err := resolveDeployment(betaSettingsOptions(t), false)
					if err == nil || !strings.Contains(err.Error(), test.env) {
						t.Fatalf("%s=%q with growth off: got %v", test.env, value, err)
					}
				})
			}
		})
	}
}

/*
================
TestBetaSettingsDeploymentDropMix
================
*/
func TestBetaSettingsDeploymentDropMix(t *testing.T) {
	for _, value := range []string{
		"equipment=100,potion=1", "assigned=1,ammo=100", "off", "on",
	} {
		t.Run(value, func(t *testing.T) {
			clearShardSettingsEnv(t)
			t.Setenv("SRO_BETA_DROP_MIX", " \t"+value+"\n")
			if got := betaSettingsVariables(t)["beta_drop_mix"]; got != value {
				t.Fatalf("beta_drop_mix = %#v, want %q", got, value)
			}
		})
	}
	for _, value := range []string{
		"unknown=1", "potion=0", "potion=101", "potion=1,POTION=2", "potion", "typo",
	} {
		t.Run("refuse "+value, func(t *testing.T) {
			clearShardSettingsEnv(t)
			t.Setenv("SRO_BETA_GROWTH", "off")
			t.Setenv("SRO_BETA_DROP_MIX", value)
			_, err := resolveDeployment(betaSettingsOptions(t), false)
			if err == nil || !strings.Contains(err.Error(), "SRO_BETA_DROP_MIX") {
				t.Fatalf("invalid mix %q with growth off: got %v", value, err)
			}
		})
	}
}

/*
================
TestBetaSettingsValidationPrecedesCatalogDiscovery
================
*/
func TestBetaSettingsValidationPrecedesCatalogDiscovery(t *testing.T) {
	for _, setting := range shardsettings.Settings {
		if !strings.HasPrefix(setting.Env, "SRO_BETA_") || setting.Source != shardsettings.FromTable {
			continue
		}
		t.Run(setting.Env, func(t *testing.T) {
			clearShardSettingsEnv(t)
			t.Setenv(setting.Env, "typo")
			options := betaSettingsOptions(t)
			options.Catalog = filepath.Join(t.TempDir(), "missing-catalog.json")
			_, err := resolveDeployment(options, false)
			if err == nil || !strings.Contains(err.Error(), setting.Env) ||
				strings.Contains(err.Error(), "shard catalog") {
				t.Fatalf("invalid beta setting before missing catalog: got %v", err)
			}
		})
	}
}

/*
================
TestBetaSettingsSnapshotOwnsValidatedValues
================
*/
func TestBetaSettingsSnapshotOwnsValidatedValues(t *testing.T) {
	clearShardSettingsEnv(t)
	t.Setenv("SRO_BETA_DROP_RATE", "7")
	t.Setenv("SRO_GM_SETSPEED", "operator-specific")
	settings := portOnlySettingsFromEnv()
	t.Setenv("SRO_BETA_DROP_RATE", "typo")
	if err := settings.validate(); err != nil {
		t.Fatalf("captured valid snapshot rejected after environment change: %v", err)
	}
	variables := (&deployment{PortOnly: settings}).gameVariables(shardDeployment{})
	if got := variables["beta_drop_rate"]; got != "7" {
		t.Fatalf("captured beta_drop_rate = %#v, want 7", got)
	}
	if got := variables["gm_setspeed"]; got != "operator-specific" {
		t.Fatalf("unchecked setting = %#v, want unchanged operator value", got)
	}
}
