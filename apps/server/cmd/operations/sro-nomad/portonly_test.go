/*
===========================================================================

portonly_test.go - the port-only switches reach the GameWorld job

Storage auto-stack and the alchemy rates default to on (owner decisions
2026-10-11); /SETSPEED defaults to on (2026-10-12); the enhancement cap and the mall goods default to on (2026-10-13); the team-death revive defaults to on, native (2026-10-13); the reverse return map defaults to off. A deployer's value, including "off", passes
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
		stack, reverse, rates, speed, cap, mall                         string
		wantStack, wantReverse, wantRates, wantSpeed, wantCap, wantMall string
	}{
		{"", "", "", "", "", "", "on", "", "on", "on", "off", "on"},
		{"off", "on", "off", "off", "off", "off", "off", "on", "off", "off", "off", "off"},
		{"on", "off", "100,50", "on", "9", "MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_FUR_HAT", "on", "off", "100,50", "on", "9",
			"MALL_AVATAR_HAT:PACKAGE_ITEM_MALL_AVATAR_M_FUR_HAT"},
	} {
		t.Setenv("SRO_STORAGE_AUTO_STACK", tc.stack)
		t.Setenv("SRO_REVERSE_RETURN_MAP", tc.reverse)
		t.Setenv("SRO_ALCHEMY_RATES", tc.rates)
		t.Setenv("SRO_GM_SETSPEED", tc.speed)
		t.Setenv("SRO_ALCHEMY_MAX_PLUS", tc.cap)
		t.Setenv("SRO_MALL_EXTRA_GOODS", tc.mall)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["mall_extra_goods"] != tc.wantMall {
			t.Fatalf("env %q: mall_extra_goods %v, want %s", tc.mall, variables["mall_extra_goods"], tc.wantMall)
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

/*
================
TestTeamDeathReviveJobVariable

team_death_revive defaults to on, native; "off" (the port-only town rule)
and "on" pass through as written.
================
*/
func TestTeamDeathReviveJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "on", "on": "on", "off": "off"} {
		t.Setenv("SRO_TEAM_DEATH_REVIVE", env)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["team_death_revive"] != want {
			t.Fatalf("env %q: team_death_revive %v, want %s", env, variables["team_death_revive"], want)
		}
	}
}

/*
================
TestAlchemyNoAssimilationJobVariable

Unset deploys the owner's default (on); "off" and "on" pass through.
================
*/
func TestAlchemyNoAssimilationJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "on", "off": "off", "on": "on"} {
		t.Setenv("SRO_ALCHEMY_NO_ASSIMILATION", env)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["alchemy_no_assimilation"] != want {
			t.Fatalf("env %q: alchemy_no_assimilation %v, want %s", env, variables["alchemy_no_assimilation"], want)
		}
	}
}

/*
================
TestCaravanBanditCapJobVariable

caravan_bandit_cap defaults to 8 (owner, 2026-10-14; 16 before); "off"
and other counts pass through as written.
================
*/
func TestCaravanBanditCapJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "8", "off": "off", "24": "24"} {
		t.Setenv("SRO_CARAVAN_BANDIT_CAP", env)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["caravan_bandit_cap"] != want {
			t.Fatalf("env %q: caravan_bandit_cap %v, want %s", env, variables["caravan_bandit_cap"], want)
		}
	}
}

/*
================
TestElixirDropsOffJobVariable

elixir_drops_off defaults to on (owner, 2026-10-13); "off" (native drops)
and "on" pass through as written.
================
*/
func TestElixirDropsOffJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "on", "on": "on", "off": "off"} {
		t.Setenv("SRO_ELIXIR_DROPS_OFF", env)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["elixir_drops_off"] != want {
			t.Fatalf("env %q: elixir_drops_off %v, want %s", env, variables["elixir_drops_off"], want)
		}
	}
}

/*
================
TestAlchemyMaterialDropsOffJobVariable

alchemy_material_drops_off defaults to off, the native drops (community
vote, 2026-10-24); "on" (the 2026-10-14 beta removal) and "off" pass
through as written.
================
*/
func TestAlchemyMaterialDropsOffJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "off", "on": "on", "off": "off"} {
		t.Setenv("SRO_ALCHEMY_MATERIAL_DROPS_OFF", env)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["alchemy_material_drops_off"] != want {
			t.Fatalf("env %q: alchemy_material_drops_off %v, want %s", env, variables["alchemy_material_drops_off"], want)
		}
	}
}

/*
================
TestRaisedGoldCapsJobVariable

raised_gold_caps defaults to off, the native stall and deposit caps;
"on" and "off" pass through as written.
================
*/
func TestRaisedGoldCapsJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "off", "off": "off", "on": "on"} {
		t.Setenv("SRO_RAISED_GOLD_CAPS", env)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["raised_gold_caps"] != want {
			t.Fatalf("env %q: raised_gold_caps %v, want %s", env, variables["raised_gold_caps"], want)
		}
	}
}

/*
================
TestJobGoodsTeleportJobVariable

job_goods_teleport defaults to off, native (owner, 2026-10-18 reversed
2026-10-15); "on" and "off" pass through as written.
================
*/
func TestJobGoodsTeleportJobVariable(t *testing.T) {
	for env, want := range map[string]string{"": "off", "on": "on", "off": "off"} {
		t.Setenv("SRO_JOB_GOODS_TELEPORT", env)
		variables := (&deployment{PortOnly: portOnlySettingsFromEnv()}).gameVariables(shardDeployment{})
		if variables["job_goods_teleport"] != want {
			t.Fatalf("env %q: job_goods_teleport %v, want %s", env, variables["job_goods_teleport"], want)
		}
	}
}
