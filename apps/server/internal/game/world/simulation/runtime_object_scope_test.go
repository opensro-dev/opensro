/*
===========================================================================

runtime_object_scope_test.go - routed frames of the port-only server actors

The AI caravan actors (SRO_AI_CARAVANS) are player-band gids no peer
lane shows; viewers see them through the object journal, so their routed
frames follow the viewer's published objects, as a ground item's do. The
native world wires no RuntimeObject, and a player-band source a viewer
merely published (and is not shown as a peer) then reaches no one.

===========================================================================
*/
package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

/*
==================
TestRuntimeObjectFramesFollowPublishedObjects
==================
*/
func TestRuntimeObjectFramesFollowPublishedObjects(t *testing.T) {
	actor := PlayerObjectID(90_001)
	for _, wired := range []bool{false, true} {
		a, b := peerSession("a", "A", 1, "A"), peerSession("b", "A", 2, "B")
		a.PublishedObjects = []uint32{actor}
		source := &fakeSource{sessions: []SessionSnapshot{a, b}}
		push := &fakePusher{}
		ticker := newTestTicker(source, push)
		if wired {
			ticker.RuntimeObject = func(gid uint32) bool { return gid == actor }
		}
		ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
			return []DivisionFrames{{DivisionID: "A", SourceGID: actor, Frames: []Frame{{Opcode: wire.OpSkillEffectControl}}}}
		}}}
		ticker.RunTick(100)
		want := 0
		if wired {
			want = 1
		}
		if got := len(peerFramesTo(push, "a", wire.OpSkillEffectControl)); got != want {
			t.Fatalf("wired=%v: the publishing viewer got %d frames, want %d", wired, got, want)
		}
		if len(peerFramesTo(push, "b", wire.OpSkillEffectControl)) != 0 || len(push.toDivision) != 0 {
			t.Fatalf("wired=%v: the actor's frame escaped its published scope", wired)
		}
	}
}
