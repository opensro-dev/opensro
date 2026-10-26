package monster

import (
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
)

// The adopted path carries the walked surface owners, so the next plan starts
// from the cell under the monster (native source pNavCell) instead of
// re-guessing it from height. A segment the path does not match has no owner.
func TestLiveNavOwnerFollowsAdoptedPathOwners(t *testing.T) {
	deck := worldgeom.NavOwner{Kind: worldgeom.NavOwnerObject, Object: worldgeom.NavObjectCell{Surface: 0x5B87, Object: 4, Cell: 2}}
	from, to := Pose{RegionID: 0x5B87, X: 10, Y: 20}, Pose{RegionID: 0x5B87, X: 100, Y: 20}
	m := MoverState{From: from, To: to, DepartMs: 1000, ArriveMs: 2000}
	flat := func(float64, Pose) (float64, bool) { return 20, true }
	m.AdoptNavigation(NewNavigationPath(from, to, to, 0, flat).WithOwners([]worldgeom.NavOwnerSpan{
		{From: 0, To: .4, Owner: worldgeom.TerrainOwner()},
		{From: .4, To: 1, Owner: deck},
	}))
	if got := m.LiveNavOwner(1200); got != worldgeom.TerrainOwner() {
		t.Fatalf("owner at t=.2 = %+v", got)
	}
	if got := m.LiveNavOwner(1800); got != deck {
		t.Fatalf("owner at t=.8 = %+v", got)
	}
	if got := m.LiveNavOwner(5000); got != deck {
		t.Fatalf("arrived owner = %+v", got)
	}
	m.To.X = 90 // a replaced segment must not reuse the old path's owners
	if got := m.LiveNavOwner(1800); got.Resolved() {
		t.Fatalf("stale path owner leaked: %+v", got)
	}
}

// A settled mover (the simulation clears the segment and sets the pose to the
// rest before replanning) keeps the rest's owner while it stands there; a pose
// moved by anything else, or a new segment, drops it (#1732).
func TestLiveNavOwnerSurvivesTheSettle(t *testing.T) {
	deck := worldgeom.NavOwner{Kind: worldgeom.NavOwnerObject, Object: worldgeom.NavObjectCell{Surface: 0x5B87, Object: 4, Cell: 2}}
	from, to := Pose{RegionID: 0x5B87, X: 10, Y: 20}, Pose{RegionID: 0x5B87, X: 100, Y: 20, Heading: 1}
	m := MoverState{From: from, To: to, DepartMs: 1000, ArriveMs: 2000}
	flat := func(float64, Pose) (float64, bool) { return 20, true }
	m.AdoptNavigation(NewNavigationPath(from, to, to, 0, flat).WithOwners([]worldgeom.NavOwnerSpan{{From: 0, To: 1, Owner: deck}}))
	m.Pose = to
	m.Pose.Heading = 2
	m.From, m.To = Pose{}, Pose{}
	m.DepartMs, m.ArriveMs = 0, 0
	if got := m.LiveNavOwner(5000); got != deck {
		t.Fatalf("settled owner = %+v, want %+v", got, deck)
	}
	moved := m
	moved.Pose.X = 101
	if got := moved.LiveNavOwner(5000); got.Resolved() {
		t.Fatalf("a moved pose kept the rest owner: %+v", got)
	}
	next := m
	next.From, next.To, next.DepartMs, next.ArriveMs = to, Pose{RegionID: 0x5B87, X: 150, Y: 20}, 6000, 7000
	if got := next.LiveNavOwner(6500); got.Resolved() {
		t.Fatalf("a new unowned segment kept the rest owner: %+v", got)
	}
}
