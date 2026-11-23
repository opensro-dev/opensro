/*
===========================================================================

position_resync_test.go - the 5 s position resync (position_resync.go)

A living, unmounted character whose position viewers were last told
diverges from where it stands (> 10, or another region) or where it
is walking (> 100) is re-broadcast as one 0x3028 to its viewers and
to itself, at most once per 5 s per character.

	10|

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
resyncPair

A mover shown to a viewer, both standing at the Europe start, one tick
in (the mover's resync book is seeded, the viewer's spawn row is
delivered). The pushes so far are discarded.
================
*/
func resyncPair(t *testing.T, startMs int64) (*fakeSource, *fakePusher, *Ticker) {
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
standMoverAt

Stands the mover at spawn without any packet carrying it there: the
drift an unpacketed move (a knockback, a slide) leaves behind.
================
*/
func standMoverAt(source *fakeSource, spawn Spawn) {
	world := DefaultWorldState(spawn)
	source.sessions[0].World = CloneWorldState(world)
}

/*
================
TestPeerResyncCorrectsStandingDrift

A mover that ends up 50 units from where viewers were told it stands,
with no packet saying so, is re-broadcast once at the 5 s tick - to
the viewer and to itself - carrying the true position; afterwards the
reference has moved, so the next cycle is quiet.
================
*/
func TestPeerResyncCorrectsStandingDrift(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source, push, ticker := resyncPair(t, startMs)
	start := EuropeStartProfile()

	drifted := start
	drifted.X += 50
	standMoverAt(source, drifted)

	for _, at := range []int64{startMs + 1000, startMs + 4900} {
		ticker.RunTick(at)
		if got := peerFramesTo(push, "viewer", wire.OpObjectPositionUpdate); len(got) != 0 {
			t.Fatalf("viewer received %d resyncs before the 5 s tick", len(got))
		}
	}
	ticker.RunTick(startMs + ResyncIntervalMs)

	viewerGot := peerFramesTo(push, "viewer", wire.OpObjectPositionUpdate)
	selfGot := peerFramesTo(push, "mover", wire.OpObjectPositionUpdate)
	if len(viewerGot) != 1 || len(selfGot) != 1 {
		t.Fatalf("resyncs at 5 s: viewer %d, mover %d, want 1 each", len(viewerGot), len(selfGot))
	}
	update, err := wire.DecodeObjectPositionUpdate(viewerGot[0])
	if err != nil {
		t.Fatalf("decode resync: %v", err)
	}
	if update.Gid != PlayerObjectID(1) || update.RegionID != drifted.RegionID {
		t.Fatalf("resync names gid %d region %d, want %d/%d", update.Gid, update.RegionID, PlayerObjectID(1), drifted.RegionID)
	}
	if dx := float64(update.X) - drifted.X; dx < -0.01 || dx > 0.01 {
		t.Fatalf("resync x = %v, want the drifted %v", update.X, drifted.X)
	}

	push.toSession = nil
	ticker.RunTick(startMs + 2*ResyncIntervalMs)
	if got := peerFramesTo(push, "viewer", wire.OpObjectPositionUpdate); len(got) != 0 {
		t.Fatalf("viewer received %d further resyncs with no new drift", len(got))
	}
}

/*
================
TestPeerResyncStandingThresholdAndRegion

Five units of standing drift stay uncorrected; a region change alone
corrects at the next due tick even with identical local coordinates.
================
*/
func TestPeerResyncStandingThresholdAndRegion(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source, push, ticker := resyncPair(t, startMs)
	start := EuropeStartProfile()

	small := start
	small.X += 5
	standMoverAt(source, small)
	ticker.RunTick(startMs + ResyncIntervalMs)
	if got := peerFramesTo(push, "mover", wire.OpObjectPositionUpdate); len(got) != 0 {
		t.Fatalf("a 5-unit standing drift drew %d resyncs, want 0", len(got))
	}

	otherRegion := small
	otherRegion.RegionID = start.RegionID + 1
	standMoverAt(source, otherRegion)
	ticker.RunTick(startMs + 2*ResyncIntervalMs)
	if got := peerFramesTo(push, "mover", wire.OpObjectPositionUpdate); len(got) != 1 {
		t.Fatalf("a bare region change drew %d resyncs, want 1", len(got))
	}
}

/*
================
TestPeerResyncBoundsMovingDrift

A 1000-unit run crosses the 100-unit moving threshold repeatedly:
each due tick that finds the mover that far from the last broadcast
position re-broadcasts it mid-walk, and nothing fires before the
first 5 s tick.
================
*/
func TestPeerResyncBoundsMovingDrift(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source, push, ticker := resyncPair(t, startMs)
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

	for nowMs := startMs + 100; nowMs < startMs+4900; nowMs += 100 {
		ticker.RunTick(nowMs)
	}
	if got := peerFramesTo(push, "mover", wire.OpObjectPositionUpdate); len(got) != 0 {
		t.Fatalf("the mover received %d resyncs before the first 5 s tick", len(got))
	}

	push.toSession = nil
	for nowMs := startMs + 4900; nowMs <= startMs+12000; nowMs += 100 {
		ticker.RunTick(nowMs)
	}
	selfGot := peerFramesTo(push, "mover", wire.OpObjectPositionUpdate)
	if len(selfGot) < 2 {
		t.Fatalf("the mover received %d mid-walk resyncs in 12 s, want at least 2", len(selfGot))
	}
	previous := start.X
	for i, payload := range selfGot {
		update, err := wire.DecodeObjectPositionUpdate(payload)
		if err != nil {
			t.Fatalf("decode resync %d: %v", i, err)
		}
		if float64(update.X) <= previous || float64(update.X) >= start.X+1000 {
			t.Fatalf("resync %d x = %v, want a mid-walk point past %v", i, update.X, previous)
		}
		previous = float64(update.X)
	}
	if first, _ := wire.DecodeObjectPositionUpdate(selfGot[0]); float64(first.X)-start.X <= ResyncMovingDistance {
		t.Fatalf("first mid-walk resync fired at %v units of drift, want past %v", float64(first.X)-start.X, ResyncMovingDistance)
	}
	if viewerGot := peerFramesTo(push, "viewer", wire.OpObjectPositionUpdate); len(viewerGot) == 0 {
		t.Fatal("the viewer received no mid-walk resync while the mover was in view")
	}
}

/*
================
TestPeerResyncGates

The native gates: a character that is not alive, and a player mounted
on a COS (whose own sync owns the rider), draw no resync however far
they have drifted.
================
*/
func TestPeerResyncGates(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	start := EuropeStartProfile()
	drifted := start
	drifted.X += 50

	dead := peerSession("mover", "DIV_A", 1, "Mover")
	dead.CombatEligible = false
	mounted := peerSession("mover", "DIV_A", 1, "Mover")
	mounted.CombatEligible = true
	mounted.COS = &PeerCOS{Mounted: true}

	for name, mover := range map[string]SessionSnapshot{"dead": dead, "mounted": mounted} {
		source := &fakeSource{sessions: []SessionSnapshot{mover, peerSession("viewer", "DIV_A", 2, "Viewer")}}
		push := &fakePusher{}
		ticker := newTestTicker(source, push)
		ticker.RunTick(startMs)
		push.toSession = nil
		standMoverAt(source, drifted)
		ticker.RunTick(startMs + ResyncIntervalMs)
		if got := peerFramesTo(push, "mover", wire.OpObjectPositionUpdate); len(got) != 0 {
			t.Errorf("%s: drew %d resyncs, want 0", name, len(got))
		}
		if got := peerFramesTo(push, "viewer", wire.OpObjectPositionUpdate); len(got) != 0 {
			t.Errorf("%s: viewer drew %d resyncs, want 0", name, len(got))
		}
	}
}

/*
================
TestPeerResyncForgetsADepartedCharacter

A character that leaves the division takes its resync book with it:
when it comes back far from where it left, long after its old book
fell due, it is seeded afresh (the spawn row carries its position), so
the first ticks after the return draw no resync; the next 5 s tick
judges it against the return position.
================
*/
func TestPeerResyncForgetsADepartedCharacter(t *testing.T) {
	const startMs = int64(1_784_000_000_000)
	source, push, ticker := resyncPair(t, startMs)
	mover := source.sessions[0]

	// The mover leaves; the viewer keeps the division ticking.
	source.sessions = source.sessions[1:]
	ticker.RunTick(startMs + 1000)

	// It returns 60 s later, 50 units from where it left.
	returned := EuropeStartProfile()
	returned.X += 50
	mover.World = CloneWorldState(DefaultWorldState(returned))
	source.sessions = append([]SessionSnapshot{mover}, source.sessions...)
	push.toSession = nil
	backMs := startMs + 60_000
	ticker.RunTick(backMs)
	ticker.RunTick(backMs + 1000)
	if got := peerFramesTo(push, "viewer", wire.OpObjectPositionUpdate); len(got) != 0 {
		t.Fatalf("viewer drew %d resyncs on the return, want 0 (stale book judged the return)", len(got))
	}

	// The fresh book is live: the next due tick still finds no drift.
	ticker.RunTick(backMs + ResyncIntervalMs)
	if got := peerFramesTo(push, "viewer", wire.OpObjectPositionUpdate); len(got) != 0 {
		t.Fatalf("viewer drew %d resyncs at the first due tick after the return, want 0", len(got))
	}
}
