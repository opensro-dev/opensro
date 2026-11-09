/*
===========================================================================

bridge_snapshot_retained_test.go - the full walk fills retained slots

The snapshot publish reuses its snapshot slots across ticks (#2989):
two walks return the same slot memory, and a changed provider is never
served stale from a retained slot.

===========================================================================
*/
package worldsession

import (
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestSnapshotSessionsRetainsSlots
================
*/
func TestSnapshotSessionsRetainsSlots(t *testing.T) {
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

	first := bridge.SnapshotSessions()
	if len(first) != 1 || first[0].CharacterID != 7 {
		t.Fatalf("first walk = %+v, want the one session at character 7", first)
	}
	second := bridge.SnapshotSessions()
	if len(second) != 1 {
		t.Fatalf("second walk holds %d sessions, want 1", len(second))
	}
	if &first[0] != &second[0] {
		t.Fatalf("second walk allocated a fresh snapshot slice (%p vs %p); the slots are retained across ticks",
			&first[0], &second[0])
	}

	// The retained slot is refilled, never served stale: a changed
	// provider shows up on the next walk, in the same memory.
	snap.CharacterID = 8
	sess.SetWorldSnapshot("DIV_A", countingProvider{snap: snap, full: &full, light: &light})
	third := bridge.SnapshotSessions()
	if len(third) != 1 || third[0].CharacterID != 8 {
		t.Fatalf("third walk = %+v, want character 8 from the changed provider", third)
	}
	if &third[0] != &first[0] {
		t.Fatalf("third walk moved the slot (%p vs %p)", &third[0], &first[0])
	}
	if views := bridge.SnapshotSessionViews(); len(views) != 1 || views[0].CharacterID != 8 {
		t.Fatalf("tick views after the third walk = %+v, want character 8", views)
	}
}
