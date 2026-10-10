/*
===========================================================================

bridge_views_test.go - the hooks' light views of the in-world sessions

SnapshotSessionViews walks the same sessions as SnapshotSessions, with the
same filters and canonical session ids, but asks a ViewProvider for its
light view and never builds the full snapshot (its peer presentation).

===========================================================================
*/
package worldsession

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
countingProvider

Both provider faces, counting each.
================
*/
type countingProvider struct {
	snap        simulation.SessionSnapshot
	full, light *int
}

func (p countingProvider) WorldSnapshot() simulation.SessionSnapshot {
	*p.full++
	return p.snap
}

func (p countingProvider) WorldView() simulation.SessionView {
	*p.light++
	return p.snap.View()
}

/*
================
TestSnapshotSessionViewsUseTheLightFace
================
*/
func TestSnapshotSessionViewsUseTheLightFace(t *testing.T) {
	t.Parallel()
	srv := startServer(t)
	_, w := dialAndHello(t, srv)
	sess, ok := srv.Hub.Session(w.SessionID)
	if !ok {
		t.Fatal("session missing from hub")
	}
	full, light := 0, 0
	snap := simulation.SessionSnapshot{
		DivisionID:    "DIV_A",
		CharacterID:   7,
		WorldInstance: 3,
		World:         simulation.DefaultWorldState(simulation.EuropeStartProfile()),
		Appearance:    &simulation.PeerAppearance{Name: "hero", ActionSpeed: 125},
	}
	sess.SetWorldSnapshot("DIV_A", countingProvider{snap: snap, full: &full, light: &light})
	bridge := New(srv.Hub)

	views := bridge.SnapshotSessionViews()
	if full != 0 || light != 1 {
		t.Fatalf("views built %d full snapshot(s) and %d light view(s), want 0 and 1", full, light)
	}
	snaps := bridge.SnapshotSessions()
	if full != 1 {
		t.Fatalf("the full walk built %d full snapshot(s), want 1", full)
	}
	if len(views) != 1 || len(snaps) != 1 {
		t.Fatalf("views %d and snapshots %d, want one session each", len(views), len(snaps))
	}
	if want := snaps[0].View(); views[0].SessionID != want.SessionID || views[0].World.Spawn != want.World.Spawn ||
		len(views[0].PublishedObjects) != len(want.PublishedObjects) {
		t.Fatalf("view %+v disagrees with the snapshot's view %+v", views[0], want)
	}
	if views[0].SessionID != SessionIDString(w.SessionID) || views[0].DivisionID != "DIV_A" ||
		views[0].CharacterID != 7 || views[0].WorldInstance != 3 {
		t.Fatalf("view identity = %+v", views[0])
	}
}
