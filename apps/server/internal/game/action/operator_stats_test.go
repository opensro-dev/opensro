/*
===========================================================================

operator_stats_test.go - the operator's stat reset keeps every earned point

===========================================================================
*/
package action

import (
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
		{30, 49, 80, 56, 49, 87},
		{30, 60, 60, 40, 49, 62}, // points kept through a level-down stay earned
	} {
		c := statCharacter(row.level, row.strength, row.intellect, row.points)
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
	c := statCharacter(90, 300, 300, 0)
	high := int64(1 << 40)
	c.CurrentHP, c.CurrentMP = &high, &high
	rt, _ := newTestRuntime(c, testItems())
	if err := rt.OperatorResetStats(testDivision, c.Name); err != nil {
		t.Fatal(err)
	}
	maxHP, maxMP, _, _ := rt.playerKeeperVitals(testDivision, c)
	if *c.CurrentHP != maxHP || *c.CurrentMP != maxMP {
		t.Fatalf("current %d/%d not trimmed to %d/%d", *c.CurrentHP, *c.CurrentMP, maxHP, maxMP)
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
