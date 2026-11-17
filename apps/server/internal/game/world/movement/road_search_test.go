/*
===========================================================================

road_search_test.go - bounded incremental road searches

===========================================================================
*/
package movement

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestRoadSearchResumesAndWalksAroundAWall
================
*/
func TestRoadSearchResumesAndWalksAroundAWall(t *testing.T) {
	from := monster.Pose{RegionID: 25000, X: 500, Z: 500}
	goal := monster.Pose{RegionID: 25000, X: 700, Z: 500}
	probe := func(a, b monster.Pose, _ bool) (float64, bool, bool) {
		if (a.X < 540 && b.X >= 540) || (b.X < 540 && a.X >= 540) {
			z := a.Z + (b.Z-a.Z)*(540-a.X)/(b.X-a.X)
			if math.Abs(z-500) < 50 {
				return 0, false, true
			}
		}
		return 0, true, true
	}
	search := newRoadSearch(from, goal, probe)
	if search.Advance(1) != nil {
		t.Fatal("search did not yield after one expansion")
	}
	var route *monster.NavigationRoute
	for i := 0; i < 100 && route == nil; i++ {
		route = search.Advance(1)
	}
	if route == nil || route.Status() != monster.NavigationRouteReady || route.Len() < 2 {
		t.Fatal("search did not find the detour")
	}
	previous := from
	for i := 0; i < route.Len(); i++ {
		next := route.Point(i)
		if _, clear, _ := probe(previous, next, false); !clear {
			t.Fatal("road crossed the wall")
		}
		previous = next
	}
}

/*
================
TestRoadSearchRefusesMissingGeometry
================
*/
func TestRoadSearchRefusesMissingGeometry(t *testing.T) {
	search := newRoadSearch(monster.Pose{}, monster.Pose{}, func(monster.Pose, monster.Pose, bool) (float64, bool, bool) { return 0, false, false })
	if result := search.Advance(1); result == nil || result.Status() != monster.NavigationGeometryUnavailable {
		t.Fatal("unknown geometry admitted a road")
	}
}
