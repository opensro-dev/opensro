/*
===========================================================================

cosground_refusal_log_test.go - pet pickup refusals roll up (#2145)

The live shard logged one warning per retry, 81% of all GameWorld lines.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestCosRefusalLogRollsUpPerCharacterAndCause

Different ground items share one rollup row, so a stream of new item gids
cannot grow the map, and a new line waits for the interval.
================
*/
func TestCosRefusalLogRollsUpPerCharacterAndCause(t *testing.T) {
	c := testCharacter()
	rt, clock := newTestRuntime(c, testItems())

	if !rt.logCosRefusal(c, wire.ItemMoveRequest{GroundGID: 1}, 2, "not-found") {
		t.Fatal("the first refusal wrote no line")
	}
	for gid := uint32(2); gid < 100; gid++ {
		if rt.logCosRefusal(c, wire.ItemMoveRequest{GroundGID: gid}, 2, "not-found") {
			t.Fatalf("refusal %d wrote a line inside the interval", gid)
		}
	}
	rows := 0
	rt.cosRefusalLog.Range(func(_, _ any) bool { rows++; return true })
	if rows != 1 {
		t.Fatalf("rollup rows = %d, want 1 whatever the item gid", rows)
	}
	clock.Advance(cosRefusalLogInterval)
	if !rt.logCosRefusal(c, wire.ItemMoveRequest{GroundGID: 100}, 2, "not-found") {
		t.Fatal("no line after the interval")
	}
}
