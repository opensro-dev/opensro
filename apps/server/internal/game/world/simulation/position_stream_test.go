/*
===========================================================================

position_stream_test.go - what a viewer receives for a walking player

Native (position_stream.go): one 0xB738 per accepted move, nothing per
tick and nothing on arrival. The port-only position stream adds the
0x3122 move state with each path, a live 0x30E3 every tick and one
0xB2F5 at arrival.

===========================================================================
*/
package simulation

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
framesNaming

The opcodes of the frames pushed to viewer that name gid, in push order.
0x30E3 carries its gid last; every other movement frame carries it first.
================
*/
func framesNaming(push *fakePusher, viewer string, gid uint32) []uint16 {
	var out []uint16
	for _, pushed := range push.toSession {
		if pushed.sessionID != viewer {
			continue
		}
		for _, f := range pushed.frames {
			named := uint32(0)
			switch {
			case f.Opcode == wire.OpObjectSourceMove && len(f.Payload) == wire.ObjectMoveSize:
				named = binary.LittleEndian.Uint32(f.Payload[16:20])
			case len(f.Payload) >= 4:
				named = binary.LittleEndian.Uint32(f.Payload[0:4])
			}
			if named == gid {
				out = append(out, f.Opcode)
			}
		}
	}
	return out
}

/*
================
countOpcode
================
*/
func countOpcode(opcodes []uint16, opcode uint16) int {
	n := 0
	for _, o := range opcodes {
		if o == opcode {
			n++
		}
	}
	return n
}

/*
================
walkSeenByViewer

A standing mover already shown to the viewer clicks a 4 s walk, clicks
again from its live point halfway (a new path, as a run/walk toggle's
re-timed walk is), and arrives; the tick runs every 100 ms throughout.
Returns the opcodes the viewer received naming the mover after the
first click.
================
*/
func walkSeenByViewer(t *testing.T, stream bool) []uint16 {
	t.Helper()
	const startMs = int64(1_784_000_000_000)
	standing := DefaultWorldState(EuropeStartProfile())
	source := &fakeSource{sessions: []SessionSnapshot{
		{SessionID: "mover", DivisionID: "DIV_A", CharacterID: 1, World: CloneWorldState(standing), Appearance: &PeerAppearance{RefObjID: 1907, Name: "Mover"}},
		peerSession("viewer", "DIV_A", 2, "Viewer"),
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.PositionStream = stream
	ticker.RunTick(startMs)
	if spawn := peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn); len(spawn) != 1 {
		t.Fatalf("the viewer was not shown the standing mover")
	}
	push.toSession = nil

	world := movingWorld(t, startMs+100)
	source.sessions[0].World = CloneWorldState(world)
	nowMs := startMs + 100
	for ; nowMs < startMs+2100; nowMs += 100 {
		ticker.RunTick(nowMs)
	}
	start := world.Spawn
	result := ApplyMove(&world, PlayerObjectID(1), MovementRequest{
		Mode: MovementAckDestinationMode, RegionID: start.RegionID, X: start.X, Y: start.Y, Z: start.Z + 100,
	}, RunMode, nowMs)
	if result.Segment == nil {
		t.Fatal("the second click made no segment")
	}
	source.sessions[0].World = CloneWorldState(world)
	for end := world.MoveSegment.ArrivesAtMs + 500; nowMs <= end; nowMs += 100 {
		ticker.RunTick(nowMs)
	}
	return framesNaming(push, "viewer", PlayerObjectID(1))
}

/*
================
TestNativePeerWalkSendsOnePathPerMove

Two clicks seen by one viewer are two 0xB738 and nothing else: no move
state, no per-tick sample, no arrival settle.
================
*/
func TestNativePeerWalkSendsOnePathPerMove(t *testing.T) {
	got := walkSeenByViewer(t, false)
	if len(got) != 2 || got[0] != OpMovementAck || got[1] != OpMovementAck {
		t.Fatalf("native viewer received %x for two clicks, want two 0xB738 and nothing else", got)
	}
}

/*
================
TestPositionStreamKeepsThePortStream

SRO_POSITION_STREAM=on: each path is 0x3122 + 0xB738, every in-flight
tick adds a live 0x30E3, and the arrival one 0xB2F5.
================
*/
func TestPositionStreamKeepsThePortStream(t *testing.T) {
	got := walkSeenByViewer(t, true)
	if countOpcode(got, OpMovementAck) != 2 || countOpcode(got, wire.OpObjectStateRefresh) != 2 {
		t.Fatalf("stream viewer received %x, want 0x3122 + 0xB738 per click", got)
	}
	if countOpcode(got, wire.OpObjectSourceCorrection) != 1 {
		t.Fatalf("stream viewer received %d arrival settles, want 1", countOpcode(got, wire.OpObjectSourceCorrection))
	}
	// The first walk ran 20 ticks, the second its own duration: a
	// sample on every in-flight tick.
	if samples := countOpcode(got, wire.OpObjectSourceMove); samples < 30 {
		t.Fatalf("stream viewer received %d live samples, want one per in-flight tick", samples)
	}
}

/*
================
TestPositionStreamFromEnv

Only an explicit on (or 1) turns the port-only stream on.
================
*/
func TestPositionStreamFromEnv(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "0": false, "yes": false, "on": true, "1": true} {
		t.Setenv(EnvPositionStream, value)
		if got := PositionStreamFromEnv(); got != want {
			t.Fatalf("%s=%q: stream %v, want %v", EnvPositionStream, value, got, want)
		}
	}
}
