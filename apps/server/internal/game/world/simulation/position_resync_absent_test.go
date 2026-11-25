/*
===========================================================================

position_resync_absent_test.go - no 5 s position resync on the v1.150 wire

The v1.188 server re-broadcasts a drifted character as 0x3028 every
5 s, but the v1.150 client registers no handler for it (its packet
table, client CPSMission_RegisterPacketHandlers 0x74D330, has none), so
the packet is outside the v1.150 wire. The port sent
it for a while, to the mover too, and every resync pulled a walking
player back by the client's lead over the server (#4418). These tests
walk and drift a character for many resync periods and require that no
0x3028 reaches the mover or a viewer.

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

// v1188PositionResyncOpcode is the v1.188 server's resync packet, which
// the v1.150 client cannot read.
const v1188PositionResyncOpcode uint16 = 0x3028

// resyncWatchMs covers several of the v1.188 server's 5 s resync periods.
const resyncWatchMs = 25_000

/*
================
moverInViewOfViewer

A mover shown to a viewer, both standing at the Europe start, one tick
in. The pushes so far are discarded.
================
*/
func moverInViewOfViewer(t *testing.T, startMs int64) (*fakeSource, *fakePusher, *Ticker) {
	t.Helper()
	mover := peerSession("mover", "DIV_A", 1, "Mover")
	mover.CombatEligible = true
	source := &fakeSource{sessions: []SessionSnapshot{
		mover,
		peerSession("viewer", "DIV_A", 2, "Viewer"),
	}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(startMs)
	if spawn := peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn); len(spawn) != 1 {
		t.Fatalf("the viewer was not shown the mover")
	}
	push.toSession = nil
	return source, push, ticker
}

/*
================
requireNoResync
================
*/
func requireNoResync(t *testing.T, push *fakePusher, what string) {
	t.Helper()
	for _, session := range []string{"mover", "viewer"} {
		if got := peerFramesTo(push, session, v1188PositionResyncOpcode); len(got) != 0 {
			t.Fatalf("%s: %s received %d 0x3028 resyncs, want none (no v1.150 handler)", what, session, len(got))
		}
	}
}

/*
================
TestNoPositionResyncDuringALongWalk

A 1000-unit run lasts 20 s, four v1.188 resync periods, each one past
the 100-unit moving threshold. Neither the walking player nor its viewer
is sent a 0x3028: the walk's own move packets are the whole wire.
================
*/
func TestNoPositionResyncDuringALongWalk(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source, push, ticker := moverInViewOfViewer(t, startMs)
	start := EuropeStartProfile()

	world := DefaultWorldState(start)
	result := ApplyMove(&world, PlayerObjectID(1), MovementRequest{
		Mode: MovementAckDestinationMode, RegionID: start.RegionID,
		X: start.X + 1000, Y: start.Y, Z: start.Z,
	}, RunMode, startMs)
	if result.Segment == nil {
		t.Fatal("the long click made no segment")
	}
	source.sessions[0].World = CloneWorldState(world)

	for nowMs := startMs + 100; nowMs <= startMs+resyncWatchMs; nowMs += 100 {
		ticker.RunTick(nowMs)
	}
	requireNoResync(t, push, "long walk")
}

/*
================
TestNoPositionResyncForStandingDrift

A character left 50 units from where its viewers were told it stands,
with no packet saying so, is past the v1.188 standing threshold (10) at
every resync period; it is still not re-broadcast.
================
*/
func TestNoPositionResyncForStandingDrift(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source, push, ticker := moverInViewOfViewer(t, startMs)

	drifted := EuropeStartProfile()
	drifted.X += 50
	source.sessions[0].World = CloneWorldState(DefaultWorldState(drifted))

	for nowMs := startMs + 100; nowMs <= startMs+resyncWatchMs; nowMs += 100 {
		ticker.RunTick(nowMs)
	}
	requireNoResync(t, push, "standing drift")
}
