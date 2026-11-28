/*
===========================================================================

public_rules_test.go - every shard setting is on the rules page or named out

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/item/gacha"
	"opensro.online/server/internal/game/progression"
)

/*
================
TestEveryShardSettingIsARuleOrUnlisted

A shard setting reaches players unless it is operator-only, so it is a rule
on the "Original vs OpenSRO" page, or unlistedSettings says why not. A new
setting cannot slip past the page (issue #938 found ten that had).
================
*/
func TestEveryShardSettingIsARuleOrUnlisted(t *testing.T) {
	rules := map[string]bool{}
	for _, rule := range publicRules(nil).Rules {
		if rules[rule.Flag] {
			t.Fatalf("%s is listed twice", rule.Flag)
		}
		rules[rule.Flag] = true
	}
	settings := map[string]bool{}
	for _, setting := range shardsettings.Settings {
		settings[setting.Env] = true
		if !rules[setting.Env] && unlistedSettings[setting.Env] == "" {
			t.Fatalf("%s is a shard setting but neither a rule nor in unlistedSettings", setting.Env)
		}
		if rules[setting.Env] && unlistedSettings[setting.Env] != "" {
			t.Fatalf("%s is both a rule and unlisted", setting.Env)
		}
	}
	for env := range unlistedSettings {
		if !settings[env] {
			t.Fatalf("unlistedSettings names %s, which is no shard setting", env)
		}
	}
}

/*
================
TestPublicRulesShowNativeBehaviorWhenBetaCapsAreOff

The rules page must not describe an off numeric cap as a change to zero.
================
*/
func TestPublicRulesShowNativeBehaviorWhenBetaCapsAreOff(t *testing.T) {
	rulesByFlag := func() map[string]publicstats.Rule {
		rules := make(map[string]publicstats.Rule)
		for _, rule := range publicRules(nil).Rules {
			rules[rule.Flag] = rule
		}
		return rules
	}

	t.Setenv(progression.EnvBetaMastery, "off")
	t.Setenv(alchemy.EnvReinforceMaxPlus, "off")
	off := rulesByFlag()
	for flag, wantNow := range map[string]string{
		progression.EnvBetaMastery:  "The native race-specific mastery totals apply.",
		alchemy.EnvReinforceMaxPlus: "The native +250 reinforcement ceiling applies.",
	} {
		rule := off[flag]
		if rule.On {
			t.Errorf("%s is on with its switch off", flag)
		}
		if rule.Now != wantNow {
			t.Errorf("%s now = %q, want %q", flag, rule.Now, wantNow)
		}
	}

	t.Setenv(progression.EnvBetaMastery, "on")
	t.Setenv(alchemy.EnvReinforceMaxPlus, "on")
	on := rulesByFlag()
	for flag, wantNow := range map[string]string{
		progression.EnvBetaMastery:  "Every character may train masteries up to a total of 5000; each mastery's own limits stay.",
		alchemy.EnvReinforceMaxPlus: "Reinforcement stops at +11.",
	} {
		rule := on[flag]
		if !rule.On {
			t.Errorf("%s is off with its switch on", flag)
		}
		if rule.Now != wantNow {
			t.Errorf("%s now = %q, want %q", flag, rule.Now, wantNow)
		}
	}
}

/*
================
TestPublicRulesReportTheGachaTheRuntimeServes

SRO_MAGIC_POP_V1696 can be on while its tables are absent: ConfigureGacha
then serves the v1.150 machine and leaves pool v2 to its own flag, so the
page must show the v1.696 machine off and pool v2 as the runtime has it.
================
*/
func TestPublicRulesReportTheGachaTheRuntimeServes(t *testing.T) {
	t.Setenv(action.EnvMagicPopV1696, "on")
	t.Setenv(action.EnvGachaPoolV2, "on")
	// What ConfigureGacha settles on the fallback: no v1.696 table and
	// pool v2 left to its own flag.
	fallback := action.NewRuntime(nil, nil)
	fallback.GachaPoolV2 = true
	rules := map[string]publicstats.Rule{}
	for _, rule := range publicRules(fallback).Rules {
		rules[rule.Flag] = rule
	}
	if rules[action.EnvMagicPopV1696].On {
		t.Error("the page shows the v1.696 machine that fell back to v1.150")
	}
	if !rules[action.EnvGachaPoolV2].On {
		t.Error("the page hides the pool v2 the fallback serves")
	}

	v1696 := action.NewRuntime(nil, nil)
	v1696.GachaCatalog = &gacha.Catalog{V1696: true}
	rules = map[string]publicstats.Rule{}
	for _, rule := range publicRules(v1696).Rules {
		rules[rule.Flag] = rule
	}
	if !rules[action.EnvMagicPopV1696].On {
		t.Error("the page hides the v1.696 machine the runtime serves")
	}
	if rules[action.EnvGachaPoolV2].On {
		t.Error("the page shows pool v2 beside the v1.696 machine")
	}
}
