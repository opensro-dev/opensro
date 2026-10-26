/*
===========================================================================

beta_settings_test.go - beta validation follows the shared settings registry

Pins each beta setting's value grammar and the optional beginner quest switch.
Mastery remains owned by its durable policy.

===========================================================================
*/

package shardsettings

import (
	"strings"
	"testing"
)

/*
================
TestBetaSettingValidationKinds
================
*/
func TestBetaSettingValidationKinds(t *testing.T) {
	want := map[string]ValueValidation{
		"SRO_BEGINNER_QUESTS":        ValueSwitch,
		"SRO_BETA_STARTER_KIT":       ValueSwitch,
		"SRO_BETA_PLAYER_MAP":        ValueSwitch,
		"SRO_BETA_MASTERY":           ValueSwitch,
		"SRO_BETA_SILK":              ValueBetaSilk,
		"SRO_BETA_SILK_ACTIVE_ONLY":  ValueSwitch,
		"SRO_BETA_GROWTH_BANDS":      ValueBetaGrowthBands,
		"SRO_BETA_GROWTH":            ValueSwitch,
		"SRO_BETA_SKILL_EXP_RATE":    ValueBetaGrowth,
		"SRO_BETA_DROP_RATE":         ValueBetaGrowth,
		"SRO_BETA_DROP_MIX":          ValueBetaDropMix,
		"SRO_BETA_DROP_CAP":          ValueBetaGrowth,
		"SRO_BETA_GOLD_RATE":         ValueBetaGrowth,
		"SRO_BETA_RARE_RATE":         ValueBetaGrowth,
		"SRO_BETA_SHOP_ONLY_DEGREE9": ValueSwitch,
		"SRO_BETA_SEAL_RATES":        ValueSwitch,
		"SRO_BETA_UNIQUE_RESPAWN":    ValueSwitch,
	}
	for _, setting := range Settings {
		expected, checked := want[setting.Env]
		if !checked {
			if strings.HasPrefix(setting.Env, "SRO_BETA_") {
				t.Errorf("new beta setting %s needs an explicit grammar expectation", setting.Env)
			}
			if setting.Validation != ValueUnchecked {
				t.Errorf("setting %s needs an explicit validation expectation for %v", setting.Env, setting.Validation)
			}
			continue
		}
		if setting.Validation != expected {
			t.Errorf("%s validation = %v, want %v", setting.Env, setting.Validation, expected)
		}
		if setting.Env == "SRO_BEGINNER_QUESTS" && setting.Default != "off" {
			t.Errorf("beginner quests default = %q, want off", setting.Default)
		}
		if setting.Env == "SRO_BETA_MASTERY" {
			if setting.Source != Built {
				t.Errorf("mastery source = %v, want durable Built policy", setting.Source)
			}
		} else if setting.Source != FromTable {
			t.Errorf("%s source = %v, want FromTable", setting.Env, setting.Source)
		}
		delete(want, setting.Env)
	}
	for env := range want {
		t.Errorf("validated setting %s missing from registry", env)
	}
}
