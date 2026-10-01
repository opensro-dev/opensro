/*
===========================================================================

growth_test.go - the closed-beta growth rates

===========================================================================
*/
package progression

import "testing"

/*
================
growthLevels

Level 3 takes 10 at-level kills, level 50 takes 400, level 2 takes 5.
================
*/
type growthLevels struct{}

func (growthLevels) SkillPointCost(level int64) (int64, bool) { return 1, true }

func (growthLevels) ExpRequired(level int64) (int64, bool) {
	switch level {
	case 2:
		return 50, true
	case 3:
		return 100, true
	case 50:
		return 400_000, true
	}
	return 0, false
}

func (growthLevels) MonsterExpBasis(level int64) (int64, bool) {
	switch level {
	case 2, 3:
		return 10, true
	case 50:
		return 1_000, true
	}
	return 0, false
}

func TestNativeGrowthLeavesEveryGainUntouched(t *testing.T) {
	exp, sp := GrowthRates{}.scale(growthLevels{}, 50, 123, 45)
	if exp != 123 || sp != 45 {
		t.Fatalf("native rates changed a gain: %d/%d", exp, sp)
	}
}

func TestBetaGrowthHoldsEveryLevelToTheLevelThreePace(t *testing.T) {
	beta := GrowthRates{Enabled: true, SkillExpRate: 100}
	// Level 50 needs 400 kills against level 3's 10: a gain is worth 40x.
	if exp, sp := beta.scale(growthLevels{}, 50, 1_000, 7); exp != 40_000 || sp != 700 {
		t.Fatalf("level 50 beta gain = %d/%d", exp, sp)
	}
	// A level already quicker than level 3 is never slowed down.
	if exp, _ := beta.scale(growthLevels{}, 2, 10, 0); exp != 10 {
		t.Fatalf("level 2 beta gain = %d", exp)
	}
	// Losses and levels without table rows stay native.
	if exp, _ := beta.scale(growthLevels{}, 50, -500, 0); exp != -500 {
		t.Fatalf("death penalty scaled: %d", exp)
	}
	if exp, _ := beta.scale(growthLevels{}, 99, 10, 0); exp != 10 {
		t.Fatalf("unknown level scaled: %d", exp)
	}
}

func TestBetaGrowthSwitchReadsTheEnvironment(t *testing.T) {
	t.Setenv(EnvBetaGrowth, "")
	if BetaGrowthFromEnv().Enabled {
		t.Fatal("unset switch enabled beta growth")
	}
	t.Setenv(EnvBetaGrowth, "on")
	t.Setenv(EnvBetaSkillExpRate, "25")
	if rates := BetaGrowthFromEnv(); !rates.Enabled || rates.SkillExpRate != 25 {
		t.Fatalf("beta switch = %+v", rates)
	}
}
