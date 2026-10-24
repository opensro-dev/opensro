/*
===========================================================================

betasilk_config_test.go - the pure beta silk earn-rate configuration contract

Pins native-off aliases, the beta default and explicit rate bounds without
reading or changing the process environment. Beta silk is port-only, not native.

===========================================================================
*/
package action

import (
	"strings"
	"testing"
)

/*
================
TestParseBetaSilk
================
*/
func TestParseBetaSilk(t *testing.T) {
	t.Parallel()
	for _, value := range []struct {
		raw  string
		want uint32
	}{
		{raw: ""},
		{raw: " \t"},
		{raw: "off"},
		{raw: "OFF"},
		{raw: "0"},
		{raw: "false"},
		{raw: " FaLsE "},
		{raw: "on", want: 50},
		{raw: " ON ", want: 50},
		{raw: "true", want: 50},
		{raw: "TrUe", want: 50},
		{raw: "1", want: 1},
		{raw: " 75 ", want: 75},
		{raw: "100000", want: 100000},
	} {
		got, err := ParseBetaSilk(value.raw)
		if err != nil || got != value.want {
			t.Errorf("ParseBetaSilk(%q) = %d, %v; want %d", value.raw, got, err, value.want)
		}
	}
}

/*
================
TestParseBetaSilkRefusesInvalidRate
================
*/
func TestParseBetaSilkRefusesInvalidRate(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"-1", "00", "x", "1.5", "0x32", "100001", "4294967296", "18446744073709551616"} {
		value, err := ParseBetaSilk(raw)
		if value != 0 || err == nil {
			t.Errorf("ParseBetaSilk(%q) = %d, %v; want refusal", raw, value, err)
			continue
		}
		if !strings.Contains(err.Error(), EnvBetaSilk) {
			t.Errorf("refusal %q does not identify %s", err, EnvBetaSilk)
		}
	}
}
