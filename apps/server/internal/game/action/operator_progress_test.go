/*
===========================================================================

operator_progress_test.go - the operator's level and SP corrections walk
the stat authority's own experience door

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

// progressLevelExp is the flat test curve: every level needs this much
// EXP, so a jump of many levels crosses several walk steps.
const progressLevelExp int64 = 1_000_000_000

/*
================
progressLevels
================
*/
type progressLevels struct{ combatRewardLevels }

/*
================
ExpRequired
================
*/
func (progressLevels) ExpRequired(level int64) (int64, bool) {
	return progressLevelExp, level >= 1 && level < 140
}

/*
================
progressRuntime

A test runtime whose RefundExperience is the real progression walk.
================
*/
func progressRuntime(t *testing.T, c *enterworld.Character) *Runtime {
	t.Helper()
	rt, _ := newTestRuntime(c, testItems())
	deps := rt.deps.(*enterworld.Deps)
	deps.Levels = progressLevels{}
	rt.RefundExperience = progression.NewRuntime(deps).ExperienceRefundUpdater()
	return rt
}

/*
================
TestOperatorSetLevelRaisesThroughTheExperienceWalk

A raise grants the per-level automatic STR/INT and 3 stat points and moves
the watermark, and lands on zero EXP into the target level.
================
*/
func TestOperatorSetLevelRaisesThroughTheExperienceWalk(t *testing.T) {
	c := statCharacter(10, progression.BaseStatAtLevel(10), progression.BaseStatAtLevel(10), 0)
	exp := int64(12345)
	c.Experience = &exp
	rt := progressRuntime(t, c)
	if err := rt.OperatorSetLevel(testDivision, c.Name, 80, progression.LevelCap); err != nil {
		t.Fatal(err)
	}
	if *c.Level != 80 || *c.MaxLevel != 80 || *c.Experience != 0 {
		t.Fatalf("level %d max %d exp %d", *c.Level, *c.MaxLevel, *c.Experience)
	}
	if *c.Strength != progression.BaseStatAtLevel(80) || *c.Intellect != progression.BaseStatAtLevel(80) ||
		*c.StatPoints != 70*progression.StatPointsPerLevel {
		t.Fatalf("STR %d INT %d free %d", *c.Strength, *c.Intellect, *c.StatPoints)
	}
}

/*
================
TestOperatorSetLevelLowersAndKeepsTheWatermark

A lowering changes only the level and EXP, as the native level-down does,
and leaves no EXP loss for a resurrection to refund.
================
*/
func TestOperatorSetLevelLowersAndKeepsTheWatermark(t *testing.T) {
	c := statCharacter(60, progression.BaseStatAtLevel(60), progression.BaseStatAtLevel(60), 177)
	rt := progressRuntime(t, c)
	if err := rt.OperatorSetLevel(testDivision, c.Name, 20, progression.LevelCap); err != nil {
		t.Fatal(err)
	}
	if *c.Level != 20 || *c.MaxLevel != 60 || *c.Experience != 0 || c.LastExpLoss != 0 {
		t.Fatalf("level %d max %d exp %d loss %d", *c.Level, *c.MaxLevel, *c.Experience, c.LastExpLoss)
	}
	if *c.Strength != progression.BaseStatAtLevel(60) || *c.StatPoints != 177 {
		t.Fatalf("a lowering moved earned stats: STR %d free %d", *c.Strength, *c.StatPoints)
	}
}

/*
================
TestOperatorSetLevelRefusals
================
*/
func TestOperatorSetLevelRefusals(t *testing.T) {
	c := statCharacter(10, progression.BaseStatAtLevel(10), progression.BaseStatAtLevel(10), 0)
	rt := progressRuntime(t, c)
	for _, level := range []int64{0, progression.LevelCap + 1} {
		if rt.OperatorSetLevel(testDivision, c.Name, level, progression.LevelCap) == nil {
			t.Fatalf("level %d admitted", level)
		}
	}
	if rt.OperatorSetLevel(testDivision, "Nobody", 20, progression.LevelCap) == nil {
		t.Fatal("unknown character admitted")
	}
	if *c.Level != 10 {
		t.Fatalf("a refusal moved the level to %d", *c.Level)
	}
	rt.RefundExperience = nil
	if rt.OperatorSetLevel(testDivision, c.Name, 20, progression.LevelCap) == nil {
		t.Fatal("a shard without the experience door admitted a level correction")
	}
}

/*
================
TestOperatorSetSkillPoints
================
*/
func TestOperatorSetSkillPoints(t *testing.T) {
	c := statCharacter(10, progression.BaseStatAtLevel(10), progression.BaseStatAtLevel(10), 0)
	rt := progressRuntime(t, c)
	for _, sp := range []int64{5000, 0, operatorMaxSkillPoints} {
		if err := rt.OperatorSetSkillPoints(testDivision, c.Name, sp); err != nil || c.SkillPoints == nil || *c.SkillPoints != sp {
			t.Fatalf("set %d: %v", sp, err)
		}
	}
	for _, sp := range []int64{-1, operatorMaxSkillPoints + 1} {
		if rt.OperatorSetSkillPoints(testDivision, c.Name, sp) == nil {
			t.Fatalf("SP %d admitted", sp)
		}
	}
}
