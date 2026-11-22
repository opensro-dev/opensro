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
	"opensro.online/server/internal/game/item/alchemy"
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
	for _, rule := range publicRules().Rules {
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
		for _, rule := range publicRules().Rules {
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
