/*
===========================================================================

monsterpublish_test.go - what a stepped monster sends its viewers

A step that emitted nothing sends nothing: no empty batch reaches a
session, however many viewers have the monster shown. The serial leg
used to push that empty batch to every shown viewer of every stepped
actor, which the transport then dropped after resolving the session.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestMonsterFramesForViewerHoldsRideStatesToRiderHolders

#4112's exact sequence: a slain mounted pet's death batch carries the
owner's dismount (the incident payload 01ca8a0100002a04c000: rider
0x00018aca, flag 0, mount 0x00c0042a) to every viewer of the monster
that dealt the blow. A viewer that was never sent the rider must not
take the ride state; a viewer holding the rider, and the rider's own
session, take the whole batch in order.
================
*/
func TestMonsterFramesForViewerHoldsRideStatesToRiderHolders(t *testing.T) {
	incident := []byte{0x01, 0xca, 0x8a, 0x01, 0x00, 0x00, 0x2a, 0x04, 0xc0, 0x00}
	frames := []Frame{
		{Opcode: 0xB245, Payload: []byte{9}},
		{Opcode: wire.OpCosRideState, Payload: incident},
		{Opcode: wire.OpObjectDespawn, Payload: []byte{1}},
	}
	holder := &SessionSnapshot{PublishedObjects: []uint32{0x00018ACA, 0x00C0042A}}
	if got := monsterFramesForViewer(frames, holder); len(got) != 3 {
		t.Fatalf("a viewer holding the rider lost frames: %v", got)
	}
	stranger := &SessionSnapshot{PublishedObjects: []uint32{0x00C0042A}}
	got := monsterFramesForViewer(frames, stranger)
	if len(got) != 2 || got[0].Opcode != 0xB245 || got[1].Opcode != wire.OpObjectDespawn {
		t.Fatalf("a viewer without the rider took the ride state: %v", got)
	}
	owner := &SessionSnapshot{Companions: []*PeerCOS{{Row: wire.CosSpawnBand2{Gid: 0x00C0042A, OwnerGid: 0x00018ACA}}}}
	if got := monsterFramesForViewer(frames, owner); len(got) != 3 {
		t.Fatalf("the rider's own session lost its dismount: %v", got)
	}
	// No ride state in the batch: the frames pass through untouched,
	// and an undecodable ride state stays shared for everyone.
	if got := monsterFramesForViewer(frames[:1], stranger); len(got) != 1 {
		t.Fatalf("a rideless batch changed: %v", got)
	}
	broken := []Frame{{Opcode: wire.OpCosRideState, Payload: []byte{2, 4}}}
	if got := monsterFramesForViewer(broken, stranger); len(got) != 1 {
		t.Fatalf("an undecodable ride state was filtered: %v", got)
	}
}

/*
================
TestQuietMonsterStepPushesNoEmptyBatch

A passive monster standing in a viewer's scope is stepped over a
second of ticks. Its quiet steps must not reach the pusher at all, and
the steps that do emit must still arrive.
================
*/
func TestQuietMonsterStepPushesNoEmptyBatch(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	viewer := playerSessionAt(1, 1050, 1000)
	push := &fakePusher{}
	const firstTickMs, tickMs, ticks = int64(100000), int64(100), 10
	for tick := int64(0); tick < ticks; tick++ {
		ops.RunMonsterLeg(firstTickMs+tick*tickMs, []SessionSnapshot{viewer}, push)
	}
	if !ops.scopeShown(viewer.SessionID)[instance.Gid] {
		t.Fatal("the fixture's viewer was never shown the monster: the test proves nothing")
	}
	if len(push.toSession) == 0 {
		t.Fatal("the viewer received nothing, not even the monster's entry")
	}
	for i, pushed := range push.toSession {
		if len(pushed.frames) == 0 {
			t.Fatalf("push %d of %d to %q carried no frames", i, len(push.toSession), pushed.sessionID)
		}
	}
}
