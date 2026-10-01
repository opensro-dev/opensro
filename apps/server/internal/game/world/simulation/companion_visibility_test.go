/*
===========================================================================

companion_visibility_test.go - independent visibility for sibling companions

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
TestSiblingCompanionsHaveIndependentVisibilityAndLifetimes
================
*/
func TestSiblingCompanionsHaveIndependentVisibilityAndLifetimes(t *testing.T) {
	owner := peerSession("owner", "A", 1, "Owner")
	viewer := peerSession("viewer", "A", 2, "Viewer")
	for i := 0; i < 3; i++ {
		pet := &PeerCOS{Row: wire.CosSpawnBand2{Band: uint8(i + 2), Gid: uint32(100 + i), RefObjID: uint32(10 + i)}, World: owner.World, Session: 1, Generation: 1}
		owner.Companions = append(owner.Companions, pet)
	}
	owner.Companions[2].World.Spawn.X += 100000
	source := &fakeSource{sessions: []SessionSnapshot{owner, viewer}}
	push := &fakePusher{}
	ticker := newTestTicker(source, push)
	ticker.RunTick(1000)
	rows := peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)
	if len(rows) != 3 {
		t.Fatalf("want player and two nearby companions, got %d", len(rows))
	}
	push.toSession = nil
	source.sessions[0].Companions = owner.Companions[1:]
	ticker.RunTick(1100)
	despawns := peerFramesTo(push, "viewer", wire.OpObjectDespawn)
	if len(despawns) != 1 || binary.LittleEndian.Uint32(despawns[0]) != 100 {
		t.Fatal("sibling cancellation changed another actor", despawns)
	}
	if len(peerFramesTo(push, "viewer", wire.OpSingleObjectSpawn)) != 0 {
		t.Fatal("unchanged sibling respawned")
	}
}
