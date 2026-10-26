/*
===========================================================================

wiring_levelcap_test.go - the level cap and growth bands at GameWorld boot

A valid cap sets the runtime's limit; a malformed growth band list refuses
the boot instead of running the all-pace fallback silently.

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/game/progression"
)

/*
================
TestApplyLevelCapRefusesMalformedBands
================
*/
func TestApplyLevelCapRefusesMalformedBands(t *testing.T) {
	t.Setenv(progression.EnvLevelCap, "110")
	t.Setenv(progression.EnvBetaGrowthBands, "1-59:pace,60-89:20,90-110:10")
	stats := &progression.Runtime{}
	if err := applyLevelCap(stats); err != nil || stats.LevelLimit != progression.ExpansionLevelCap {
		t.Fatalf("valid settings: limit %d, err %v", stats.LevelLimit, err)
	}
	t.Setenv(progression.EnvBetaGrowthBands, "90-110:10,1-59:pace")
	if err := applyLevelCap(&progression.Runtime{}); err == nil {
		t.Fatal("a descending band list booted")
	}
}

/*
================
TestPublishedLevelCap

Only the expansion cap reaches the world-entry blob; the native cap and
an unset limit publish nothing, so the native blob stays byte-identical.
================
*/
func TestPublishedLevelCap(t *testing.T) {
	for _, tc := range []struct {
		limit int64
		want  int64
	}{
		{0, 0},
		{progression.LevelCap, 0},
		{progression.ExpansionLevelCap, progression.ExpansionLevelCap},
	} {
		if got := publishedLevelCap(tc.limit); got != tc.want {
			t.Fatalf("limit %d: published %d, want %d", tc.limit, got, tc.want)
		}
	}
}
