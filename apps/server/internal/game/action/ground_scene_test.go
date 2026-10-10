/*
===========================================================================

ground_scene_test.go - a drop removed while the scene loads despawns at ready

Drives the real Hub through EnterWorld and game-ready. The TTL sweep runs
between the two, when ground routing has no published scope to reach, as
during an alt-tabbed client's resumed bootstrap.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

/*
================
groundDespawns
================
*/
func groundDespawns(frames []transport.Frame) map[uint32]int {
	out := make(map[uint32]int)
	for _, f := range frames {
		if f.Opcode == wire.OpObjectDespawn && len(f.Payload) >= 4 {
			out[binary.LittleEndian.Uint32(f.Payload)]++
		}
	}
	return out
}

/*
================
TestGameReadyDespawnsGroundRemovedWhileLoading
================
*/
func TestGameReadyDespawnsGroundRemovedWhileLoading(t *testing.T) {
	h := newPublicationHarness(t, false, 1)
	h.deps.ObjectListRows = func(divisionID string, c *enterworld.Character, _ *enterworld.LocalPlayerEntry) []enterworld.Packet {
		return enterworld.GroundObjectListRows(h.rt.CharacterGroundItems(divisionID, c))
	}
	h.deps.ReconcileSceneObjects = h.rt.ReconcileGroundScope
	gold := GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2}
	at := simulation.SeedWorldState(h.character).Spawn
	expiring := h.rt.Ground.Add(testDivision, PlanGoldDrop(gold, 100, at, "someone", h.clock.Now()))
	h.clock.Advance(grounditem.FixtureLifetime / 2)
	staying := h.rt.Ground.Add(testDivision, PlanGoldDrop(gold, 200, at, "someone", h.clock.Now()))

	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	// The sweep commits while the scene loads; its routed despawn finds no
	// published scope, exactly as the ticker delivers it.
	h.clock.Advance(grounditem.FixtureLifetime/2 + grounditem.SweepInterval)
	h.rt.SweepExpired(h.clock.NowMs())
	if _, exists := h.rt.Ground.Get(testDivision, expiring.Gid); exists {
		t.Fatal("sweep kept the expired drop")
	}

	despawns := groundDespawns(h.ready(t, conn))
	if despawns[expiring.Gid] != 1 {
		t.Fatalf("game-ready despawned the expired drop %d times, want 1", despawns[expiring.Gid])
	}
	if despawns[staying.Gid] != 0 {
		t.Fatal("game-ready despawned a live drop")
	}
}
