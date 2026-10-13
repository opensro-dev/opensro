/*
===========================================================================

baseline_test.go - tests for baseline.go

===========================================================================
*/

package guild

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

/*
================
TestDeltasReachOnlyASessionHoldingTheGuildBlock

A delta is queued only after the session's block for that guild was
queued, and never for another guild or once the block is retired. The
entry of a character without a guild records no block.
================
*/
func TestDeltasReachOnlyASessionHoldingTheGuildBlock(t *testing.T) {
	s := &transport.Session{ID: 0xB10C}
	t.Cleanup(func() { ForgetSession(s) })
	var queued []string
	delta := func(name string) func() error {
		return func() error { queued = append(queued, name); return nil }
	}
	if SendDelta(s, 7, delta("before the block")) {
		t.Fatal("a delta reached a session without the block")
	}
	if err := SendBaseline(s, 7, delta("block")); err != nil {
		t.Fatal(err)
	}
	if !SendDelta(s, 7, delta("member joined")) || SendDelta(s, 8, delta("other guild")) {
		t.Fatal("a delta went to the wrong guild's block")
	}
	RetireBaseline(s, 8)
	if !SendDelta(s, 7, delta("still held")) {
		t.Fatal("retiring another guild retired this block")
	}
	RetireBaseline(s, 7)
	if SendDelta(s, 7, delta("after the leave")) {
		t.Fatal("a delta reached a session whose block was retired")
	}
	if err := SendBaseline(s, 0, delta("entry without a guild")); err != nil {
		t.Fatal(err)
	}
	if SendDelta(s, 7, delta("after a guildless entry")) {
		t.Fatal("a guildless entry kept the old block")
	}
	want := []string{"block", "member joined", "still held", "entry without a guild"}
	if len(queued) != len(want) {
		t.Fatalf("queued %v, want %v", queued, want)
	}
	for i := range want {
		if queued[i] != want[i] {
			t.Fatalf("queued %v, want %v", queued, want)
		}
	}
}

/*
================
TestFilterDeltasKeepsOtherFramesInOrder
================
*/
func TestFilterDeltasKeepsOtherFramesInOrder(t *testing.T) {
	frames := []wire.Frame{{Opcode: OpGuildUpdatePush, Payload: []byte{5}}, {Opcode: 0x3891}, {Opcode: OpGuildUpdatePush, Payload: []byte{6}}, {Opcode: 0x3892}}
	if got := FilterDeltas(true, frames); len(got) != 4 {
		t.Fatalf("a holder lost frames: %v", got)
	}
	got := FilterDeltas(false, frames)
	if len(got) != 2 || got[0].Opcode != 0x3891 || got[1].Opcode != 0x3892 {
		t.Fatalf("without the block: %v", got)
	}
}
