/*
===========================================================================

operator_stats_test.go - the operator's stat reset keeps every earned point

===========================================================================
*/
package action

import (
	"math"
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
)

/*
================
statCharacter
================
*/
func statCharacter(level, strength, intellect, points int64) *enterworld.Character {
	c := testCharacter()
	c.Level, c.Strength, c.Intellect, c.StatPoints = &level, &strength, &intellect, &points
	maxLevel := level
	c.MaxLevel = &maxLevel
	return c
}

/*
================
TestOperatorResetStatsReturnsSpentPointsAtTheLevelBase

The 2026-10-10 case: level 90 with STR 160, INT 325 and nothing free comes
back to 109 / 109 with 267 free; the total of 485 is kept.
================
*/
func TestOperatorResetStatsReturnsSpentPointsAtTheLevelBase(t *testing.T) {
	for _, row := range []struct{ level, strength, intellect, points, base, free int64 }{
		{90, 160, 325, 0, 109, 267},
		{90, 109, 109, 267, 109, 267}, // already reset: unchanged
		{1, 20, 20, 0, 20, 0},
		{1, 21, 20, 65534, 20, 65535}, // largest representable refund
		{30, 49, 80, 56, 49, 87},
		{30, 60, 60, 40, 49, 62}, // points kept through a level-down stay earned
	} {
		c := statCharacter(row.level, row.strength, row.intellect, row.points)
		if row.level == 30 && row.strength == 60 {
			watermark := int64(40)
			c.MaxLevel = &watermark
		}
		if row.level == 1 && row.points == 0 {
			c.StatPoints = nil // An absent pool is canonically zero.
		}
		rt, _ := newTestRuntime(c, testItems())
		before := c.Snapshot()
		if err := rt.OperatorResetStats(testDivision, c.Name); err != nil {
			t.Fatal(row, err)
		}
		if *c.Strength != row.base || *c.Intellect != row.base || *c.StatPoints != row.free {
			t.Fatalf("%+v: got STR %d INT %d free %d", row, *c.Strength, *c.Intellect, *c.StatPoints)
		}
		if row.base != progression.BaseStatAtLevel(row.level) {
			t.Fatalf("%+v: base %d", row, progression.BaseStatAtLevel(row.level))
		}
		// Nothing else moves except a current gauge trimmed to a lower maximum.
		after := c.Snapshot()
		before.Strength, before.Intellect, before.StatPoints = after.Strength, after.Intellect, after.StatPoints
		before.CurrentHP, before.CurrentMP = after.CurrentHP, after.CurrentMP
		if !reflect.DeepEqual(before, after) {
			t.Fatalf("%+v: reset changed unrelated character state", row)
		}
	}
}

/*
================
TestOperatorResetStatsTrimsCurrentGaugesToTheLowerMaximum
================
*/
func TestOperatorResetStatsTrimsCurrentGaugesToTheLowerMaximum(t *testing.T) {
	c := statCharacter(1, 300, 300, 0)
	rt, _ := newTestRuntime(c, testItems())
	before, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	oldHP, _ := before.Param(3)
	oldMP, _ := before.Param(4)
	hp, mp := int64(oldHP), int64(oldMP)
	c.CurrentHP, c.CurrentMP = &hp, &mp
	if err := rt.OperatorResetStats(testDivision, c.Name); err != nil {
		t.Fatal(err)
	}
	after, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	maxHP, hpOK := after.Param(3)
	maxMP, mpOK := after.Param(4)
	if !hpOK || !mpOK || maxHP <= 0 || maxMP <= 0 || maxHP >= oldHP || maxMP >= oldMP ||
		*c.CurrentHP != int64(maxHP) || *c.CurrentMP != int64(maxMP) {
		t.Fatalf("current %d/%d not trimmed from %v/%v to %v/%v", *c.CurrentHP, *c.CurrentMP, oldHP, oldMP, maxHP, maxMP)
	}
	if *c.Strength != 20 || *c.Intellect != 20 || *c.StatPoints != 560 {
		t.Fatal("incorrect refund")
	}
}

/*
================
TestOperatorResetStatsRefusesUnrepresentableRecords

Recovery must not normalize unknown stats or create a pool the wire truncates.
Refusals leave the entire authority record unchanged.
================
*/
func TestOperatorResetStatsRefusesUnrepresentableRecords(t *testing.T) {
	for _, row := range []struct {
		name                               string
		level, strength, intellect, points int64
	}{
		{"negative strength", 1, -1, 40, 0},
		{"negative intellect", 1, 40, -1, 0},
		{"negative pool", 1, 40, 40, -1},
		{"below base with enough total", 30, 48, 100, 10},
		{"zero level", 0, 20, 20, 0},
		{"level overflow", math.MaxInt64, 20, 20, 0},
		{"strength overflow", 1, math.MaxInt64, 20, 0},
		{"refund overflow", 1, 21, 20, 65535},
		{"pool overflow", 1, 20, 20, math.MaxInt64},
		{"missing strength", 1, 20, 20, 0},
		{"missing intellect", 1, 20, 20, 0},
	} {
		t.Run(row.name, func(t *testing.T) {
			c := statCharacter(row.level, row.strength, row.intellect, row.points)
			if row.name == "missing strength" {
				c.Strength = nil
			}
			if row.name == "missing intellect" {
				c.Intellect = nil
			}
			rt, _ := newTestRuntime(c, testItems())
			before := c.Snapshot()
			if err := rt.OperatorResetStats(testDivision, c.Name); err == nil {
				t.Fatal("invalid reset accepted")
			}
			if !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatal("refused reset changed character")
			}
		})
	}
}

/*
================
TestOperatorResetStatsRefusesMissingDeletedAndInconsistent
================
*/
func TestOperatorResetStatsRefusesMissingDeletedAndInconsistent(t *testing.T) {
	c := statCharacter(90, 160, 325, 0)
	rt, _ := newTestRuntime(c, testItems())
	if err := rt.OperatorResetStats(testDivision, "Nobody"); err == nil {
		t.Fatal("missing character reset")
	}
	c.DeletePending = true
	if err := rt.OperatorResetStats(testDivision, c.Name); err == nil || *c.Strength != 160 {
		t.Fatal("deleted character reset")
	}
	c.DeletePending = false
	low := statCharacter(90, 50, 50, 0)
	rt, _ = newTestRuntime(low, testItems())
	if err := rt.OperatorResetStats(testDivision, low.Name); err == nil || *low.Strength != 50 || *low.StatPoints != 0 {
		t.Fatal("a record below its level base was 'reset' into invented points")
	}
}

/*
================
TestOperatorResetStatsPersistsThroughAuthorityRestart
================
*/
func TestOperatorResetStatsPersistsThroughAuthorityRestart(t *testing.T) {
	seed := statCharacter(90, 160, 325, 0)
	d := openDoorRuntime(t, t.TempDir(), seed)
	deps := d.rt.deps.(*enterworld.Deps)
	deps.UpdateCharacter = d.authority.UpdateCharacter
	deps.ReadCharacter = func(_ string, read func()) { d.authority.ReadState(read) }
	if err := d.rt.OperatorResetStats(testDivision, d.character.Name); err != nil {
		t.Fatal(err)
	}
	if health := d.authority.Health(); health.LastError != "" {
		t.Fatalf("storage: %+v", health)
	}
	got := d.reboot(t).character
	if domain.CharacterStrength(got) != 109 || domain.CharacterIntellect(got) != 109 || got.StatPoints == nil || *got.StatPoints != 267 {
		t.Fatalf("reset did not survive restart: STR %d INT %d free %v", domain.CharacterStrength(got), domain.CharacterIntellect(got), got.StatPoints)
	}
}
