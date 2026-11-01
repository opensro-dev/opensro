package simulation

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"testing"
)

func TestGroundMaintenanceUsesPublishedObjects(t *testing.T) {
	a, b := peerSession("a", "A", 1, "A"), peerSession("b", "A", 2, "B")
	gid := domain.GroundItemGIDBase + 1
	a.PublishedObjects = []uint32{gid}
	source := &fakeSource{sessions: []SessionSnapshot{a, b}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames {
		return []DivisionFrames{{DivisionID: "A", SourceGID: gid, Frames: []Frame{{Opcode: wire.OpGroundOwnershipExpired}}}}
	}}}
	ticker.RunTick(100)
	if len(peerFramesTo(push, "a", wire.OpGroundOwnershipExpired)) != 1 || len(peerFramesTo(push, "b", wire.OpGroundOwnershipExpired)) != 0 || len(push.toDivision) != 0 {
		t.Fatal("ground event escaped its published scope")
	}
	push.toSession = nil
	source.sessions[0].PublishedObjects = nil
	ticker.RunTick(200)
	if len(peerFramesTo(push, "a", wire.OpGroundOwnershipExpired)) != 0 {
		t.Fatal("retired scene received ground event")
	}
}
