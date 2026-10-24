/*
===========================================================================

growth_config_test.go - the beta growth numeric configuration contract

Pure parser cases pin the existing gameplay ranges without process environment
or licensed game data. Deployment and runtime must accept the same overrides.

===========================================================================
*/
package progression

import (
	"math"
	"strconv"
	"strings"
	"testing"
)

/*
================
TestParseBetaGrowthSetting
================
*/
func TestParseBetaGrowthSetting(t *testing.T) {
	t.Parallel()
	settings := []struct {
		env              string
		minimum, maximum int64
	}{
		{env: EnvBetaSkillExpRate, minimum: 1, maximum: math.MaxInt64},
		{env: EnvBetaDropRate, minimum: 1, maximum: 100},
		{env: EnvBetaDropCap, minimum: 0, maximum: 1000},
		{env: EnvBetaGoldRate, minimum: 1, maximum: 10000},
		{env: EnvBetaRareRate, minimum: 1, maximum: 1000},
	}
	for _, setting := range settings {
		t.Run(setting.env, func(t *testing.T) {
			for _, value := range []struct {
				raw  string
				want int64
			}{
				{raw: strconv.FormatInt(setting.minimum, 10), want: setting.minimum},
				{raw: strconv.FormatInt(setting.maximum, 10), want: setting.maximum},
				{raw: " \t+12\n", want: 12},
				{raw: "001", want: 1},
			} {
				got, err := ParseBetaGrowthSetting(setting.env, value.raw)
				if err != nil || got != value.want {
					t.Errorf("ParseBetaGrowthSetting(%s, %q) = %d, %v; want %d", setting.env, value.raw, got, err, value.want)
				}
			}
			invalid := []string{
				"", " ", "x", "1.5", "0x10", "1_000",
				strconv.FormatInt(setting.minimum-1, 10),
				"9223372036854775808", "-9223372036854775809",
			}
			if setting.maximum != math.MaxInt64 {
				invalid = append(invalid, strconv.FormatInt(setting.maximum+1, 10))
			}
			for _, raw := range invalid {
				got, err := ParseBetaGrowthSetting(setting.env, raw)
				if got != 0 || err == nil {
					t.Errorf("ParseBetaGrowthSetting(%s, %q) = %d, %v; want refusal", setting.env, raw, got, err)
					continue
				}
				if !strings.Contains(err.Error(), setting.env) || !strings.Contains(err.Error(), strconv.Quote(raw)) {
					t.Errorf("refusal %q does not identify %s=%q", err, setting.env, raw)
				}
			}
		})
	}
}

/*
================
TestParseBetaGrowthSettingRefusesUnknownVariable
================
*/
func TestParseBetaGrowthSettingRefusesUnknownVariable(t *testing.T) {
	t.Parallel()
	const unknown = "SRO_UNKNOWN_BETA_RATE"
	const raw = "12"
	value, err := ParseBetaGrowthSetting(unknown, raw)
	if value != 0 || err == nil {
		t.Fatalf("unknown setting = %d, %v; want refusal", value, err)
	}
	if !strings.Contains(err.Error(), unknown) || !strings.Contains(err.Error(), strconv.Quote(raw)) {
		t.Fatalf("unknown-setting refusal = %q", err)
	}
}
