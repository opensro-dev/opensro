/*
===========================================================================

peer_motion_test.go - peer path admission across stationary visibility

A viewer must receive new destinations without requiring a despawn/spawn,
while unchanged paths must not restart on every server tick.

===========================================================================
*/
package simulation

import "testing"

/*
================
peerGoals
================
*/
func peerGoals(push *fakePusher, viewer string) int {
	count := 0
	for _, batch := range push.toSession {
		if batch.sessionID != viewer {
			continue
		}
		for _, frame := range batch.frames {
			if frame.Opcode == OpMovementAck {
				count++
			}
		}
	}
	return count
}

/*
================
TestVisiblePeerReceivesEachNewDestinationOnce
================
*/
func TestVisiblePeerReceivesEachNewDestinationOnce(t *testing.T) {
	const startMs int64 = 1784000000000
	mover := peerSession("mover", "DIV_A", 1, "Mover")
	viewer := peerSession("viewer", "DIV_A", 2, "Viewer")
	source := &fakeSource{sessions: []SessionSnapshot{mover, viewer}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(startMs)
	if peerGoals(push, "viewer") != 0 {
		t.Fatal("idle spawn has a destination")
	}

	world := movingWorld(t, startMs)
	source.sessions[0].World = CloneWorldState(world)
	ticker.RunTick(startMs + 100)
	if peerGoals(push, "viewer") != 1 {
		t.Fatal("already visible peer did not receive the new path")
	}
	for delta := int64(200); delta <= 500; delta += 100 {
		source.sessions[0].World = CloneWorldState(world)
		ticker.RunTick(startMs + delta)
	}
	if peerGoals(push, "viewer") != 1 {
		t.Fatal("unchanged path restarted")
	}

	ApplyMove(&world, PlayerObjectID(1), MovementRequest{Mode: MovementAckDestinationMode, RegionID: world.Spawn.RegionID, X: world.Spawn.X - 100, Y: world.Spawn.Y, Z: world.Spawn.Z + 50}, RunMode, startMs+600)
	source.sessions[0].World = CloneWorldState(world)
	ticker.RunTick(startMs + 700)
	if peerGoals(push, "viewer") != 2 {
		t.Fatal("re-aim was not published")
	}
	if peerGoals(push, "mover") != 0 {
		t.Fatal("peer path echoed to local prediction owner")
	}
}

/*
================
TestNewlyVisibleMoverHasOneDestination
================
*/
func TestNewlyVisibleMoverHasOneDestination(t *testing.T) {
	const startMs int64 = 1784000000000
	mover := peerSession("mover", "DIV_A", 1, "Mover")
	mover.World = movingWorld(t, startMs)
	source := &fakeSource{sessions: []SessionSnapshot{mover, peerSession("viewer", "DIV_A", 2, "Viewer")}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(startMs + 100)
	ticker.RunTick(startMs + 200)
	if got := peerGoals(push, "viewer"); got != 1 {
		t.Fatalf("newly visible mover received %d destinations, want 1", got)
	}
}
