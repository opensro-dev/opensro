/*
===========================================================================

cosground_refusal_log_test.go - pet pickup refusals roll up (#2145)

The live shard logged one warning per retry, 81% of all GameWorld lines.

===========================================================================
*/
package action

import (
	"sync/atomic"
	"testing"

	log "github.com/sirupsen/logrus"
	testhook "github.com/sirupsen/logrus/hooks/test"

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

/*
================
TestCosRefusalExpectedCausesAreCountedNotWarned

#2298: not-found and reserved are ordinary play, so they count into
the per-cause totals and never reach the warning log; a grant fault
keeps its warning and names the fault that produced it.
================
*/
func TestCosRefusalExpectedCausesAreCountedNotWarned(t *testing.T) {
	hook := testhook.NewGlobal()
	defer hook.Reset()
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())

	rt.logCosRefusal(c, wire.ItemMoveRequest{GroundGID: 7}, 2, "not-found")
	rt.logCosRefusal(c, wire.ItemMoveRequest{GroundGID: 8}, 2, "reserved")
	rt.logCosRefusal(c, wire.ItemMoveRequest{GroundGID: 9}, cosPickupBagFull, "grant-fault/inventoryFull")

	var warned []string
	for _, entry := range hook.AllEntries() {
		if entry.Message == "cos ground refused" && entry.Level <= log.WarnLevel {
			warned = append(warned, entry.Data["cause"].(string))
		}
	}
	if len(warned) != 1 || warned[0] != "grant-fault/inventoryFull" {
		t.Fatalf("warned refusal causes = %v, want only grant-fault/inventoryFull", warned)
	}
	for cause, want := range map[string]uint64{"not-found": 1, "reserved": 1, "grant-fault/inventoryFull": 1} {
		entry, ok := rt.cosRefusalTotals.Load(cause)
		if !ok {
			t.Fatalf("no refusal total for %s", cause)
		}
		if got := entry.(*atomic.Uint64).Load(); got != want {
			t.Fatalf("refusal total[%s] = %d, want %d", cause, got, want)
		}
	}
}
