/*
===========================================================================

tickpublication_test.go - tick private batches are captured under the gate

A tick commit and the capture of its private recipients happen while the
publication gate is held, so an entry bootstrap (which holds the same gate
around its snapshot) is either wholly before or wholly after them (#713).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestSealTickPrivateCapturesUnderTheGate

Private batches become captured sends, keyed by the player's object id,
taken while the gate is held; public and scoped batches keep routing, and
every batch keeps its order.
================
*/
func TestSealTickPrivateCapturesUnderTheGate(t *testing.T) {
	rt := &Runtime{}
	rt.LockPublication("d")()
	lane := rt.operations.lane("d", &rt.maintenance)
	var captured []uint32
	var delivered [][]wire.Frame
	rt.CaptureObjectFrames = func(division string, gid uint32) func([]wire.Frame) {
		if lane.publication.TryLock() {
			lane.publication.Unlock()
			t.Fatal("captured without the publication gate")
		}
		captured = append(captured, gid)
		return func(frames []wire.Frame) { delivered = append(delivered, frames) }
	}
	unlock := rt.lockTickPublication()
	out := rt.sealTickPrivate([]simulation.DivisionFrames{
		{DivisionID: "d", Frames: []simulation.Frame{{Opcode: 1}}},
		{DivisionID: "d", OnlyCharacterID: 7, Frames: []simulation.Frame{{Opcode: 2}}},
		{DivisionID: "d", OnlyCharacterID: 7, Frames: []simulation.Frame{{Opcode: 3}}},
		{DivisionID: "d", OnlyCharacterID: 8, Frames: []simulation.Frame{{Opcode: 4, ScopeGID: 99}}},
	})
	unlock()
	if len(out) != 3 || out[0].Deliver != nil || out[0].Frames[0].Opcode != 1 ||
		out[1].Deliver == nil || out[2].Deliver != nil || out[2].OnlyCharacterID != 8 {
		t.Fatalf("sealed batches = %+v", out)
	}
	if len(captured) != 1 || captured[0] != simulation.PlayerObjectID(7) {
		t.Fatalf("captured = %v, want character 7's object", captured)
	}
	out[1].Deliver()
	if len(delivered) != 1 || len(delivered[0]) != 2 || delivered[0][0].Opcode != 2 || delivered[0][1].Opcode != 3 {
		t.Fatalf("delivered = %+v, want the coalesced private batch", delivered)
	}
}

/*
================
TestSealTickPrivateWithoutCaptureKeepsRouting

Without a capture (tests, tools) private batches keep the ticker's routing.
================
*/
func TestSealTickPrivateWithoutCaptureKeepsRouting(t *testing.T) {
	out := (&Runtime{}).sealTickPrivate([]simulation.DivisionFrames{
		{DivisionID: "d", OnlyCharacterID: 7, Frames: []simulation.Frame{{Opcode: 2}}},
	})
	if len(out) != 1 || out[0].Deliver != nil || out[0].OnlyCharacterID != 7 {
		t.Fatalf("sealed batches = %+v", out)
	}
}

/*
================
TestTickHooksReleaseThePublicationGate

Both tick hooks hand the gate back before they return, so request
handlers and entry bootstraps are never left waiting on a finished tick.
================
*/
func TestTickHooksReleaseThePublicationGate(t *testing.T) {
	rt, clock, _, _ := newCombatTestRuntime(t, 1)
	// The tick takes every known lane; this one stands for any division.
	rt.LockPublication("d")()
	lane := rt.operations.lane("d", &rt.maintenance)
	now := clock.Now().UnixMilli()
	rt.MonsterActionTickHook()(now)
	rt.TickHook()(now)
	if !lane.publication.TryLock() {
		t.Fatal("a tick hook kept the publication gate")
	}
	lane.publication.Unlock()
}
