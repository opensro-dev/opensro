package simulation

import (
	"slices"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestPeerInstanceAndBlockTransitionsOwnMovementDelivery

A peer that enters a viewer's instance mid-walk arrives as its spawn row,
its move state and its path; the port-only position stream adds the
live sample.
================
*/
func TestPeerInstanceAndBlockTransitionsOwnMovementDelivery(t *testing.T) {
	for _, stream := range []bool{false, true} {
		peerInstanceAndBlockTransitions(t, stream)
	}
}

/*
================
peerInstanceAndBlockTransitions
================
*/
func peerInstanceAndBlockTransitions(t *testing.T, stream bool) {
	const now = int64(1_784_000_000_000)
	viewer := peerSession("viewer", "A", 1, "Viewer")
	peer := peerSession("peer", "A", 2, "Peer")
	viewer.WorldInstance, peer.WorldInstance = 0x10001, 0x20001
	peer.World = movingWorld(t, now)
	source := &fakeSource{sessions: []SessionSnapshot{peer, viewer}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.PositionStream = stream
	ticker.RunTick(now)
	if len(push.toSession) != 0 || len(push.toDivision) != 0 {
		t.Fatal("cross-instance peer publication")
	}
	source.sessions[0].WorldInstance = viewer.WorldInstance
	ticker.RunTick(now + 100)
	var opcodes []uint16
	for _, p := range push.toSession {
		if p.sessionID == "viewer" {
			for _, f := range p.frames {
				opcodes = append(opcodes, f.Opcode)
			}
		}
	}
	want := []uint16{wire.OpSingleObjectSpawn, wire.OpObjectStateRefresh, OpMovementAck}
	if stream {
		want = append(want, wire.OpObjectSourceMove)
	}
	if !slices.Equal(opcodes, want) {
		t.Fatalf("position stream %v: spawn-before-movement %x, want %x", stream, opcodes, want)
	}
	push.toSession = nil
	source.sessions[1].World.Spawn.X += 1000
	ticker.RunTick(now + 200)
	if len(peerFramesTo(push, "viewer", wire.OpObjectDespawn)) != 1 || len(peerFramesTo(push, "viewer", wire.OpObjectSourceMove)) != 0 {
		t.Fatal("block exit retained movement recipient")
	}
	push.toSession = nil
	source.sessions[1].World.Spawn = viewer.World.Spawn
	ticker.RunTick(now + 300)
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 1 {
		t.Fatal("block reentry missing spawn")
	}
	push.toSession = nil
	source.sessions[0].WorldInstance = 0x30001
	ticker.RunTick(now + 400)
	if len(peerFramesTo(push, "viewer", wire.OpObjectDespawn)) != 1 || len(peerFramesTo(push, "viewer", wire.OpObjectSourceMove)) != 0 || len(push.toDivision) != 0 {
		t.Fatal("instance departure retained movement recipient")
	}
}
