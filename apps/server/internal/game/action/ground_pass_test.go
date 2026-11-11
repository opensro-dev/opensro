/*
===========================================================================

ground_pass_test.go - the per-tick ground pass walks the tick's sessions in place

AdvanceGroundScope used to copy every session view into a per-division
slice each tick (#3164). It now walks the sessions where they lie, once
per division. These tests pin what the grouping guaranteed: divisions in
name order, each division's sessions in the tick's order, a session with
no published set passed over, and a settled-marks table that never
outgrows the sessions it serves.

===========================================================================
*/
package action

import (
	"slices"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

const groundPassRegion = 0x6a9a

/*
================
groundPassViewer

A main-world session standing at (x, z) of the test region.
================
*/
func groundPassViewer(id, division string, x float64, published []uint32) simulation.SessionView {
	return simulation.SessionView{
		SessionID:        id,
		DivisionID:       division,
		WorldInstance:    domain.DefaultWorldInstance,
		PublishedObjects: published,
		World:            simulation.WorldState{Spawn: simulation.Spawn{RegionID: groundPassRegion, X: x, Z: 100}},
	}
}

/*
================
TestGroundPassWalksEachDivisionsOwnSessions

Two divisions with one drop each, their sessions interleaved in the
tick's list, and one session that has no published set yet. Every
viewer stands on its division's drop, so each owes a spawn and the
pass asks the scene for it: in division name order, then in the
tick's order, and never for the session with no published set.
================
*/
func TestGroundPassWalksEachDivisionsOwnSessions(t *testing.T) {
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	for _, division := range []string{"alpha", "beta"} {
		rt.Ground.Add(division, grounditem.Item{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02",
			Position: grounditem.Point{RegionID: groundPassRegion, X: 100, Z: 100}})
	}
	sessions := []simulation.SessionView{
		groundPassViewer("beta-1", "beta", 100, []uint32{}),
		groundPassViewer("alpha-1", "alpha", 100, []uint32{}),
		groundPassViewer("alpha-unpublished", "alpha", 100, nil),
		groundPassViewer("beta-2", "beta", 100, []uint32{}),
		groundPassViewer("alpha-2", "alpha", 100, []uint32{}),
	}
	var asked []string
	scene := func(sessionID string) (*transport.Session, uint64, bool) {
		asked = append(asked, sessionID)
		return nil, 0, false
	}
	rt.AdvanceGroundScope(1000, sessions, scene)
	if want := []string{"alpha-1", "alpha-2", "beta-1", "beta-2"}; !slices.Equal(asked, want) {
		t.Fatalf("the pass asked the scene for %v, want %v", asked, want)
	}
}

/*
================
TestSettledMarksNeverOutgrowTheSessions

A viewer with nothing to gain or lose settles and leaves a mark. The
marks of sessions that are gone are dropped once the table holds more
marks than the tick has sessions, so it never outgrows them; a mark of
a session still present is kept.
================
*/
func TestSettledMarksNeverOutgrowTheSessions(t *testing.T) {
	rt := &Runtime{Ground: grounditem.NewRegistry()}
	scene := func(string) (*transport.Session, uint64, bool) { return nil, 0, false }
	pass := func(ids ...string) {
		t.Helper()
		sessions := make([]simulation.SessionView, 0, len(ids))
		for _, id := range ids {
			sessions = append(sessions, groundPassViewer(id, "main", 100, []uint32{}))
		}
		rt.AdvanceGroundScope(1000, sessions, scene)
		if len(rt.groundScopeMarks) > len(sessions) {
			t.Fatalf("after a pass over %v the table holds %d marks", ids, len(rt.groundScopeMarks))
		}
		for _, id := range ids {
			if _, kept := rt.groundScopeMarks[id]; !kept {
				t.Fatalf("after a pass over %v the mark of %q is missing", ids, id)
			}
		}
	}
	pass("a", "b", "c")
	pass("a", "d", "e")
	pass("f")
	pass("f", "g")
	pass("g")
}
