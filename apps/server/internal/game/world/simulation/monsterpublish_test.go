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

import "testing"

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
