/*
===========================================================================

growth_test.go - the closed-beta growth rates

===========================================================================
*/
package progression

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
growthLevels

Level 1 takes 5 at-level kills, level 3 takes 10, level 50 takes 400.
================
*/
type growthLevels struct{}

func (growthLevels) SkillPointCost(level int64) (int64, bool) { return 1, true }

func (growthLevels) ExpRequired(level int64) (int64, bool) {
	switch level {
	case 1:
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
	case 1, 3:
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

func TestBetaGrowthHoldsEveryLevelToTheLevelOnePace(t *testing.T) {
	beta := GrowthRates{Enabled: true, SkillExpRate: 100}
	// Level 50 needs 400 kills against level 1's 5: a gain is worth 80x,
	// and skill EXP keeps that pace before its own 100x.
	if exp, sp := beta.scale(growthLevels{}, 50, 1_000, 7); exp != 80_000 || sp != 7*80*100 {
		t.Fatalf("level 50 beta gain = %d/%d", exp, sp)
	}
	// Level 1 skill EXP gets only the flat rate.
	if _, sp := beta.scale(growthLevels{}, 1, 0, 7); sp != 700 {
		t.Fatalf("level 1 beta skill gain = %d", sp)
	}
	// Level 3 needs 10 kills: twice the level-1 pace.
	if exp, _ := beta.scale(growthLevels{}, 3, 10, 0); exp != 20 {
		t.Fatalf("level 3 beta gain = %d", exp)
	}
	// Level 1 itself is the reference and stays native.
	if exp, _ := beta.scale(growthLevels{}, 1, 10, 0); exp != 10 {
		t.Fatalf("level 1 beta gain = %d", exp)
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
	if rates := BetaGrowthFromEnv(); !rates.Enabled || rates.SkillExpRate != 25 || rates.DropRate != betaDropRateDefault ||
		rates.GoldRate != betaGoldRateDefault || rates.RareRate != betaRareRateDefault {
		t.Fatalf("beta switch = %+v", rates)
	}
	t.Setenv(EnvBetaDropRate, "8")
	if rates := BetaGrowthFromEnv(); rates.DropRate != 8 {
		t.Fatalf("drop override = %+v", rates)
	}
	for _, bad := range []string{"0", "-3", "101", "x"} {
		t.Setenv(EnvBetaDropRate, bad)
		if rates := BetaGrowthFromEnv(); rates.DropRate != betaDropRateDefault {
			t.Fatalf("drop override %q = %+v", bad, rates)
		}
	}
	t.Setenv(EnvBetaGoldRate, "200")
	if rates := BetaGrowthFromEnv(); rates.GoldRate != 200 {
		t.Fatalf("gold override = %+v", rates)
	}
	for _, bad := range []string{"0", "-3", "10001", "x"} {
		t.Setenv(EnvBetaGoldRate, bad)
		if rates := BetaGrowthFromEnv(); rates.GoldRate != betaGoldRateDefault {
			t.Fatalf("gold override %q = %+v", bad, rates)
		}
	}
	t.Setenv(EnvBetaRareRate, "10")
	if rates := BetaGrowthFromEnv(); rates.RareRate != 10 {
		t.Fatalf("rare override = %+v", rates)
	}
	for _, bad := range []string{"0", "-3", "1001", "x"} {
		t.Setenv(EnvBetaRareRate, bad)
		if rates := BetaGrowthFromEnv(); rates.RareRate != betaRareRateDefault {
			t.Fatalf("rare override %q = %+v", bad, rates)
		}
	}
}

/*
================
TestBetaGrowthLeavesAResurrectionRefundNative

A resurrection gives back a share of the EXP a death took. With beta
growth on, a level-50 gain is worth 80 times its amount, but the refund
is not a gain: it lands exactly as computed.
================
*/
func TestBetaGrowthLeavesAResurrectionRefundNative(t *testing.T) {
	const level, refund = 50, 1_000
	fresh := func() (*Runtime, *enterworld.Character) {
		character := levelupTestCharacter()
		character.Level = int64Ptr(level)
		character.MaxLevel = int64Ptr(level)
		character.Experience = int64Ptr(0)
		rt := NewRuntime(&enterworld.Deps{
			Characters: enterworld.StaticCharacterSource{testDivision: {character}},
			Items:      emptyItemRefs{},
			Levels:     growthLevels{},
		})
		rt.Growth = GrowthRates{Enabled: true}
		return rt, character
	}

	rt, character := fresh()
	if _, ok := rt.ExperienceUpdater()(character, refund, 0, 0); !ok || *character.Experience != 80*refund {
		t.Fatalf("gain landed as %d, want the beta %d", *character.Experience, 80*refund)
	}

	rt, character = fresh()
	frames, ok := rt.ExperienceRefundUpdater()(character, refund)
	if !ok || len(frames) == 0 || *character.Experience != refund {
		t.Fatalf("refund landed as %d (ok %v), want %d", *character.Experience, ok, refund)
	}
}
