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

/*
================
TestSnapshotSessionViewsReuseTheTickWalk

Within a tick the hooks share the full walk's light face: after
SnapshotSessions, SnapshotSessionViews asks the provider for nothing
again and returns the same sessions the full walk saw. The next full
walk replaces them, so a changed world is never served stale.
================
*/
func TestSnapshotSessionViewsReuseTheTickWalk(t *testing.T) {
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

	snaps := bridge.SnapshotSessions()
	if full != 1 || light != 0 {
		t.Fatalf("full walk built %d full and %d light, want 1 and 0", full, light)
	}
	for range 2 {
		views := bridge.SnapshotSessionViews()
		if full != 1 || light != 0 {
			t.Fatalf("views re-walked the provider: %d full and %d light, want 1 and 0", full, light)
		}
		if len(views) != 1 || views[0].SessionID != snaps[0].SessionID ||
			views[0].CharacterID != 7 || views[0].World.Spawn != snaps[0].World.Spawn {
			t.Fatalf("tick views = %+v, snapshots = %+v", views, snaps)
		}
	}

	snap.CharacterID = 8
	sess.SetWorldSnapshot("DIV_A", countingProvider{snap: snap, full: &full, light: &light})
	bridge.SnapshotSessions()
	if views := bridge.SnapshotSessionViews(); len(views) != 1 || views[0].CharacterID != 8 {
		t.Fatalf("views after the next walk = %+v, want character 8", views)
	}
}
