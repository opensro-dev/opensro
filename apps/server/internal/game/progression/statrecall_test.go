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
