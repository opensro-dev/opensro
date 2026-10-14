/*
===========================================================================

public_rules_test.go - every shard setting is on the rules page or named out

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/config/shardsettings"
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
