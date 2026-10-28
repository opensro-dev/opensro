/*
===========================================================================

ground_scope_test.go - ground drops follow their viewer's range (#617)

The pure rule (groundScope), the bootstrap's filter, and the per-tick pass
through a real Hub session: a drop made out of sight appears when its
viewer comes into range and leaves when the viewer walks away.

===========================================================================
*/
package action

import (
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// groundScopeFarOffset puts a drop five message blocks (5 x 320) away.
const groundScopeFarOffset = 5 * worldgeom.InterestBlockSize

/*
================
TestGroundScopeSpawnsInRangeAndDespawnsTheRest
================
*/
func TestGroundScopeSpawnsInRangeAndDespawnsTheRest(t *testing.T) {
	const region = 0x6a9a
	at := func(gid uint32, x float32) grounditem.Item {
		return grounditem.Item{Gid: gid, Position: grounditem.Point{RegionID: region, X: x, Z: 100}}
	}
	base := domain.GroundItemGIDBase
	near, shownNear := at(base+1, 100), at(base+2, 300)
	far := at(base+3, 100+float32(groundScopeFarOffset))
	gone := base + 4
	const monster = 7
	viewer := worldgeom.InterestBlockAt(worldgeom.RegionXZ{RegionID: region, X: 100, Z: 100})

	spawn, despawn := groundScope(bucketDrops([]grounditem.Item{near, shownNear, far}), viewer,
		[]uint32{monster, shownNear.Gid, far.Gid, gone}, make(map[uint32]struct{}))
	if len(spawn) != 1 || spawn[0].Gid != near.Gid {
		t.Fatalf("spawn %+v, want only the unshown drop in range", spawn)
	}
	if !slices.Equal(despawn, []uint32{far.Gid, gone}) {
		t.Fatalf("despawn %v, want the out-of-range and the removed drop, never the monster", despawn)
	}
}

/*
================
TestGroundBootstrapListsOnlyDropsInRange
================
*/
func TestGroundBootstrapListsOnlyDropsInRange(t *testing.T) {
	h := newPublicationHarness(t, false, 1)
	gold := GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2}
	at := simulation.SeedWorldState(h.character).Spawn
	near := h.rt.Ground.Add(testDivision, PlanGoldDrop(gold, 100, at, "someone", h.clock.Now()))
	away := at
	away.X += groundScopeFarOffset
	h.rt.Ground.Add(testDivision, PlanGoldDrop(gold, 200, away, "someone", h.clock.Now()))
	entry := &enterworld.LocalPlayerEntry{}
	entry.StartProfile.RegionID, entry.StartProfile.X, entry.StartProfile.Z = int64(at.RegionID), at.X, at.Z

	rows := h.rt.GroundObjectRows(testDivision, h.character, entry)
	if len(rows) != 1 || len(rows[0].Scope) != 1 || rows[0].Scope[0].GID != near.Gid {
		t.Fatalf("bootstrap rows %+v, want only the drop in range", rows)
	}
}

/*
================
TestGroundScopeTickFollowsTheViewer

A drop committed with no route to the viewer (as one made out of sight) is
introduced by the tick while the viewer is in range, without its drop-in,
and despawned once the viewer has walked five blocks away.
================
*/
func TestGroundScopeTickFollowsTheViewer(t *testing.T) {
	h := newPublicationHarness(t, false, 1)
	h.deps.ObjectListRows = func(divisionID string, c *enterworld.Character, entry *enterworld.LocalPlayerEntry) []enterworld.Packet {
		return h.rt.GroundObjectRows(divisionID, c, entry)
	}
	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	h.ready(t, conn)
	sessions := h.server.Hub.SessionsInDivision(testDivision)
	if len(sessions) != 1 {
		t.Fatalf("%d sessions in the division, want 1", len(sessions))
	}
	s := sessions[0]
	scene := func(string) (*transport.Session, uint64, bool) {
		revision, live := s.SceneRevision()
		return s, revision, live
	}
	world := simulation.SeedWorldState(h.character)
	gold := GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2}
	drop := h.rt.Ground.Add(testDivision, PlanGoldDrop(gold, 100, world.Spawn, "someone", h.clock.Now()))

	tick := func() []transport.Frame {
		t.Helper()
		revision, _ := s.SceneRevision()
		published, current := s.PublishedObjects(revision)
		if !current {
			t.Fatal("the scene is not current")
		}
		h.rt.AdvanceGroundScope(h.clock.NowMs(), []simulation.SessionView{{SessionID: "viewer",
			DivisionID: testDivision, WorldInstance: domain.DefaultWorldInstance, PublishedObjects: published,
			World: world}}, scene)
		conn.in <- transport.Frame{Opcode: publicationFence}
		return publicationThrough(t, conn, publicationFence)
	}

	spawns := groundSpawns(t, tick(), drop.TypeFlags)
	if spawns[drop.Gid] != 1 {
		t.Fatalf("the tick introduced the drop in range %d times, want 1", spawns[drop.Gid])
	}
	if again := groundSpawns(t, tick(), drop.TypeFlags); again[drop.Gid] != 0 {
		t.Fatal("the tick introduced a drop the scene already shows")
	}

	world.Spawn.X += groundScopeFarOffset
	if despawns := groundDespawns(tick()); despawns[drop.Gid] != 1 {
		t.Fatalf("the tick despawned the drop left behind %d times, want 1", despawns[drop.Gid])
	}
	revision, _ := s.SceneRevision()
	published, _ := s.PublishedObjects(revision)
	if slices.Contains(published, drop.Gid) {
		t.Fatal("the despawned drop is still in the scene's scope")
	}
}
