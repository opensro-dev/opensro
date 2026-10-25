/*
===========================================================================

statrecall_test.go - the stat point recall returns spent points, invents none

===========================================================================
*/

package progression

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestStatRecallReturnsEverySpentPoint

A level-10 character has STR and INT 29 automatically. With 6 points in
STR, 3 in INT and 18 unspent, the recall leaves 29/29 and 27 points.
================
*/
func TestStatRecallReturnsEverySpentPoint(t *testing.T) {
	c := testCharacter()
	c.Level, c.MaxLevel = int64Ptr(10), int64Ptr(10)
	c.Strength, c.Intellect, c.StatPoints = int64Ptr(35), int64Ptr(32), int64Ptr(18)
	rt := newTestRuntime(c)
	frames, ok := rt.StatRecallUpdater()(c)
	if !ok || *c.Strength != 29 || *c.Intellect != 29 || *c.StatPoints != 27 {
		t.Fatalf("recall ok=%v STR %d INT %d points %d; want 29 29 27", ok, *c.Strength, *c.Intellect, *c.StatPoints)
	}
	var stats, pool bool
	for _, frame := range frames {
		stats = stats || frame.Opcode == wire.OpBaseStats
		if frame.Opcode == wire.OpPointsUpdate && frame.Payload[0] == wire.PointsTypeStat {
			pool = frame.Payload[1] == 27 && frame.Payload[2] == 0
		}
	}
	if !stats || !pool {
		t.Fatalf("missing the 0x343C stat words or the absolute 27-point pool: %v", frames)
	}
}

/*
================
TestStatRecallUsesTheHighestLevelReached

A character that lost a level keeps the automatic stats of its maximum.
================
*/
func TestStatRecallUsesTheHighestLevelReached(t *testing.T) {
	c := testCharacter()
	c.Level, c.MaxLevel = int64Ptr(9), int64Ptr(10)
	c.Strength, c.Intellect, c.StatPoints = int64Ptr(30), int64Ptr(29), int64Ptr(0)
	rt := newTestRuntime(c)
	if _, ok := rt.StatRecallUpdater()(c); !ok || *c.Strength != 29 || *c.StatPoints != 1 {
		t.Fatalf("STR %d points %d; want 29 and 1", *c.Strength, *c.StatPoints)
	}
}

/*
================
TestStatRecallWithNothingSpentChangesNothing
================
*/
func TestStatRecallWithNothingSpentChangesNothing(t *testing.T) {
	c := testCharacter()
	c.Level, c.MaxLevel = int64Ptr(10), int64Ptr(10)
	c.Strength, c.Intellect, c.StatPoints = int64Ptr(29), int64Ptr(29), int64Ptr(27)
	rt := newTestRuntime(c)
	if frames, ok := rt.StatRecallUpdater()(c); ok || len(frames) != 0 || *c.StatPoints != 27 {
		t.Fatalf("a recall with nothing spent reported %v and %d frames", ok, len(frames))
	}
}

/*
================
TestResetStatsFormula

#1609's rule, shared by the scroll and the console: at a never-spent build
STR = INT = MaxLevel + 19 and the pool is 3 x (MaxLevel - 1), whatever the
split of a spent build.
================
*/
func TestResetStatsFormula(t *testing.T) {
	for _, maxLevel := range []int64{1, 10, 90, 100, 110} {
		base, pool := maxLevel+19, 3*(maxLevel-1)
		for _, spentStr := range []int64{0, pool / 3, pool} {
			c := testCharacter()
			c.Level, c.MaxLevel = int64Ptr(maxLevel), int64Ptr(maxLevel)
			c.Strength, c.Intellect, c.StatPoints = int64Ptr(base+spentStr), int64Ptr(base), int64Ptr(pool-spentStr)
			got, ok := ResetStats(c)
			if !ok || got != (StatReset{Strength: base, Intellect: base, Points: pool}) {
				t.Fatalf("max level %d, %d in STR: %+v ok=%v; want %d/%d/%d", maxLevel, spentStr, got, ok, base, base, pool)
			}
		}
	}
}

/*
================
TestResetStatsRefusesUnrepresentableRecords

A refused record is never normalised: no invented point, no truncated pool.
================
*/
func TestResetStatsRefusesUnrepresentableRecords(t *testing.T) {
	for _, row := range []struct {
		name                               string
		level, strength, intellect, points int64
	}{
		{"below base", 30, 48, 100, 10},
		{"negative pool", 1, 40, 40, -1},
		{"zero level", 0, 20, 20, 0},
		{"level beyond the wire byte", 256, 20, 20, 0},
		{"refund wider than the word", 1, 21, 20, 65535},
	} {
		c := testCharacter()
		c.Level, c.MaxLevel = int64Ptr(row.level), int64Ptr(row.level)
		c.Strength, c.Intellect, c.StatPoints = int64Ptr(row.strength), int64Ptr(row.intellect), int64Ptr(row.points)
		if got, ok := ResetStats(c); ok {
			t.Fatalf("%s: reset accepted as %+v", row.name, got)
		}
	}
	missing := testCharacter()
	missing.Level, missing.Strength = int64Ptr(10), nil
	if _, ok := ResetStats(missing); ok {
		t.Fatal("a missing STR was reset")
	}
}

/*
================
TestStatRecallKeepsTheScrollRatherThanLoseAPoint

The scroll used to clamp a refund wider than the stat-point word, silently
losing the excess. It now refuses, changes nothing, and the scroll is kept.
================
*/
func TestStatRecallKeepsTheScrollRatherThanLoseAPoint(t *testing.T) {
	c := testCharacter()
	c.Level, c.MaxLevel = int64Ptr(1), int64Ptr(1)
	c.Strength, c.Intellect, c.StatPoints = int64Ptr(21), int64Ptr(20), int64Ptr(65535)
	rt := newTestRuntime(c)
	if frames, ok := rt.StatRecallUpdater()(c); ok || len(frames) != 0 || *c.Strength != 21 || *c.StatPoints != 65535 {
		t.Fatalf("an unrepresentable refund was applied: ok=%v STR %d points %d", ok, *c.Strength, *c.StatPoints)
	}
}
