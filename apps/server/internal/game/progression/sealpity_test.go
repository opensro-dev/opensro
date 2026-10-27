/*
===========================================================================

sealpity_test.go - the seal pity switches parse (port-only, not native)

===========================================================================
*/
package progression

import "testing"

/*
================
TestSealPityFromEnvDefaultsOff
================
*/
func TestSealPityFromEnvDefaultsOff(t *testing.T) {
	pity, err := SealPityFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if pity.Enabled() || pity.ForcesDrops() || pity.Mode() != SealPityOff {
		t.Fatalf("unset switches = mode %v, want off", pity.Mode())
	}
	// The default table rides along even off, ready for the mode flip.
	if got := pity.Threshold(9, SealMoon); got != 1.5 {
		t.Fatalf("degree-9 Moon threshold = %v, want 1.5", got)
	}
	if got := pity.Threshold(9, SealSun); got != 1.5 {
		t.Fatalf("degree-9 Sun threshold = %v, want 1.5", got)
	}
	if got := pity.Threshold(9, SealStar); got != 0 {
		t.Fatalf("degree-9 Star threshold = %v, want 0 (calibration only)", got)
	}
	if got := pity.Threshold(8, SealSun); got != 0 {
		t.Fatalf("degree-8 Sun threshold = %v, want 0", got)
	}
	if pity.DailyCap() != 1 {
		t.Fatalf("daily cap = %d, want 1", pity.DailyCap())
	}
}

/*
================
TestSealPityFromEnvModesAndTable
================
*/
func TestSealPityFromEnvModesAndTable(t *testing.T) {
	t.Setenv(EnvSealPity, "shadow")
	t.Setenv(EnvSealPityThresholds, "8:sun:2.5, 9:Moon:1.25")
	t.Setenv(EnvSealPityDailyCap, "3")
	pity, err := SealPityFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if !pity.Enabled() || pity.ForcesDrops() {
		t.Fatalf("shadow = enabled %v forces %v, want counting without forcing", pity.Enabled(), pity.ForcesDrops())
	}
	if got := pity.Threshold(8, SealSun); got != 2.5 {
		t.Fatalf("degree-8 Sun threshold = %v, want 2.5", got)
	}
	if got := pity.Threshold(9, SealMoon); got != 1.25 {
		t.Fatalf("degree-9 Moon threshold = %v, want 1.25", got)
	}
	if pity.DailyCap() != 3 {
		t.Fatalf("daily cap = %d, want 3", pity.DailyCap())
	}

	t.Setenv(EnvSealPity, "on")
	pity, err = SealPityFromEnv()
	if err != nil || !pity.ForcesDrops() {
		t.Fatalf("on = %v/%v, want forcing", pity.Mode(), err)
	}

	// Set-but-empty enables no tier.
	t.Setenv(EnvSealPityThresholds, "")
	pity, err = SealPityFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got := pity.Threshold(9, SealSun); got != 0 {
		t.Fatalf("empty table degree-9 Sun = %v, want 0", got)
	}
}

/*
================
TestSealPityFromEnvRefusesMalformed
================
*/
func TestSealPityFromEnvRefusesMalformed(t *testing.T) {
	for _, tc := range []struct {
		name  string
		key   string
		value string
	}{
		{"mode", EnvSealPity, "sometimes"},
		{"entry shape", EnvSealPityThresholds, "9:sun"},
		{"degree low", EnvSealPityThresholds, "0:sun:1.5"},
		{"degree high", EnvSealPityThresholds, "10:sun:1.5"},
		{"tier", EnvSealPityThresholds, "9:comet:1.5"},
		{"zero T", EnvSealPityThresholds, "9:sun:0"},
		{"negative T", EnvSealPityThresholds, "9:sun:-1"},
		{"huge T", EnvSealPityThresholds, "9:sun:100000"},
		{"repeated tier", EnvSealPityThresholds, "9:sun:1.5,9:sun:2"},
		{"cap text", EnvSealPityDailyCap, "one"},
		{"cap negative", EnvSealPityDailyCap, "-1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(EnvSealPity, "on")
			t.Setenv(tc.key, tc.value)
			if _, err := SealPityFromEnv(); err == nil {
				t.Fatalf("%s=%q parsed, want a boot-stopping error", tc.key, tc.value)
			}
		})
	}
}
