/*
===========================================================================

sunrate_test.go - the Sun drop rate's environment parse

The flag is off unless it holds a positive integer; anything else is
native parity, so a typo on the shard can never widen a drop row.

===========================================================================
*/
package progression

import "testing"

/*
================
TestSunRateFromEnvParsesOnlyPositiveRates
================
*/
func TestSunRateFromEnvParsesOnlyPositiveRates(t *testing.T) {
	for _, tc := range []struct {
		value  string
		active bool
		rate   int
		mass   int
	}{
		{"", false, 0, 1},
		{"0", false, 0, 1},
		{"-1", false, 0, 1},
		{"abc", false, 0, 1},
		{"2.5", false, 0, 1},
		{" 2", true, 2, 60},
		{"1", true, 1, 30},
		{"2", true, 2, 60},
		{"7", true, 7, 210},
	} {
		t.Setenv(EnvSunDropRate, tc.value)
		got := SunRateFromEnv()
		if got.Active() != tc.active || got.Rate() != tc.rate || got.MassMultiplier() != tc.mass {
			t.Errorf("SRO_SUN_DROP_RATE=%q: got active=%v rate=%d mass=%d, want %v/%d/%d",
				tc.value, got.Active(), got.Rate(), got.MassMultiplier(), tc.active, tc.rate, tc.mass)
		}
	}
}
