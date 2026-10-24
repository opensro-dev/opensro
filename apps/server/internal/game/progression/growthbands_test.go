/*
===========================================================================

growthbands_test.go - tests for growthbands.go: the per-level-band EXP and
SP multiplier and its strict parser

===========================================================================
*/
package progression

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// ownerBands is the owner's 2026-10-24 deployment value.
const ownerBands = "1-59:pace,60-89:20,90-110:10"

/*
================
TestGrowthBandsPickTheMultiplierAtEveryEdge

growthLevels gives level 50 eighty times the level-1 kill count and level
59 no table row (so its pace is 1). Each band edge takes its own band's
rule, for EXP and for skill EXP before the flat SkillExpRate, and a level
past the last band earns the native gain.
================
*/
func TestGrowthBandsPickTheMultiplierAtEveryEdge(t *testing.T) {
	bands, err := ParseGrowthBands(ownerBands)
	if err != nil {
		t.Fatal(err)
	}
	beta := GrowthRates{Enabled: true, SkillExpRate: 100, Bands: bands}
	for _, c := range []struct {
		level int64
		want  int64
	}{
		{1, 1}, {50, 80}, {59, 1}, // pace band
		{60, 20}, {89, 20}, // 20x band
		{90, 10}, {110, 10}, // 10x band
	} {
		exp, sp := beta.scale(growthLevels{}, c.level, 100, 3)
		if exp != 100*c.want || sp != 3*c.want*100 {
			t.Fatalf("level %d: gain %d/%d, want %d/%d", c.level, exp, sp, 100*c.want, 3*c.want*100)
		}
		if pace := beta.ExpPace(growthLevels{}, c.level); pace != float64(c.want) {
			t.Fatalf("level %d: pet pace %v, want %d", c.level, pace, c.want)
		}
	}
	bands, err = ParseGrowthBands("1-59:pace")
	if err != nil {
		t.Fatal(err)
	}
	if exp, _ := (GrowthRates{Enabled: true, Bands: bands}).scale(growthLevels{}, 60, 100, 0); exp != 100 {
		t.Fatalf("an uncovered level gained %d, want the native 100", exp)
	}
}

/*
================
TestGrowthBandsUnsetKeepsTheLevelOnePace

No bands is the beta before this change: every level at the level-1 pace.
================
*/
func TestGrowthBandsUnsetKeepsTheLevelOnePace(t *testing.T) {
	beta := GrowthRates{Enabled: true, SkillExpRate: 1}
	if exp, _ := beta.scale(growthLevels{}, 50, 100, 0); exp != 8_000 {
		t.Fatalf("unset bands at level 50 gained %d, want the paced 8000", exp)
	}
	if exp, _ := (GrowthRates{Bands: mustBands(t, ownerBands)}).scale(growthLevels{}, 60, 100, 0); exp != 100 {
		t.Fatalf("beta off with bands set gained %d, want the native 100", exp)
	}
}

/*
================
TestParseGrowthBandsRefusesBadLists

Overlap, descending, out-of-range levels, a bad rule and too many bands
are all refused; the owner's value parses.
================
*/
func TestParseGrowthBandsRefusesBadLists(t *testing.T) {
	for _, bad := range []string{
		"1-59", "1:pace", "0-10:pace", "1-111:pace", "10-5:pace",
		"1-60:pace,60-89:20", "60-89:20,1-59:pace", "1-59:fast", "1-59:0", "1-59:10001",
		"1-1:1,2-2:1,3-3:1,4-4:1,5-5:1,6-6:1,7-7:1,8-8:1,9-9:1",
	} {
		if _, err := ParseGrowthBands(bad); err == nil {
			t.Fatalf("%q parsed", bad)
		}
	}
	bands := mustBands(t, " 1-59:PACE , 60-89:20,90-110:10 ")
	if bands.Count != 3 || bands.Band[1] != (GrowthBand{First: 60, Last: 89, Multiplier: 20}) {
		t.Fatalf("parsed %+v", bands)
	}
}

/*
================
TestBetaGrowthReadsTheBandsFromTheEnvironment

The variable feeds GrowthRates; an invalid list keeps the all-pace default
rather than half-applying.
================
*/
func TestBetaGrowthReadsTheBandsFromTheEnvironment(t *testing.T) {
	t.Setenv(EnvBetaGrowth, "on")
	t.Setenv(EnvBetaGrowthBands, ownerBands)
	if rates := BetaGrowthFromEnv(); rates.Bands.Count != 3 {
		t.Fatalf("bands %+v", rates.Bands)
	}
	t.Setenv(EnvBetaGrowthBands, "1-200:pace")
	if rates := BetaGrowthFromEnv(); rates.Bands.Count != 0 {
		t.Fatalf("invalid bands applied: %+v", rates.Bands)
	}
}

/*
================
TestGrowthBandsReachEveryGainThroughTheUpdater

A kill's EXP in a 20x band lands at 20x through the runtime's
ExperienceUpdater, the path kills, party shares and quest rewards share.
Level 50 is the fixture's level with a full table row.
================
*/
func TestGrowthBandsReachEveryGainThroughTheUpdater(t *testing.T) {
	character := levelupTestCharacter()
	character.Level = int64Ptr(50)
	character.MaxLevel = int64Ptr(50)
	character.Experience = int64Ptr(0)
	rt := NewRuntime(&enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Items:      emptyItemRefs{},
		Levels:     growthLevels{},
	})
	rt.Growth = GrowthRates{Enabled: true, Bands: mustBands(t, "1-49:pace,50-89:20")}
	if _, ok := rt.ExperienceUpdater()(character, 7, 0, 0); !ok || *character.Experience != 140 {
		t.Fatalf("level 50 gain landed as %d, want 140", *character.Experience)
	}
}

/*
================
mustBands
================
*/
func mustBands(t *testing.T, text string) GrowthBands {
	t.Helper()
	bands, err := ParseGrowthBands(text)
	if err != nil {
		t.Fatal(err)
	}
	return bands
}
