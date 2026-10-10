package movement

import (
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestMonsterRouteDetoursUsingValidatedEdges(t *testing.T) {
	from := monster.Pose{RegionID: 25000, X: 500, Y: 20, Z: 500}
	goal := from
	goal.X += 128
	coords := func(p monster.Pose) (float64, float64) {
		return worldgeom.Delta(worldgeom.RegionXZ{RegionID: from.RegionID, X: from.X, Z: from.Z}, worldgeom.RegionXZ{RegionID: p.RegionID, X: p.X, Z: p.Z})
	}
	calls := 0
	probe := func(a, b monster.Pose) *monster.NavigationPath {
		calls++
		x, z := coords(a)
		xx, zz := coords(b)
		// Exact segment/rectangle interval intersection, including diagonals.
		lo, hi := 0.0, 1.0
		for _, axis := range [][4]float64{{x, xx - x, 40, 88}, {z, zz - z, -48, 48}} {
			if math.Abs(axis[1]) < 1e-9 {
				if axis[0] < axis[2] || axis[0] > axis[3] {
					lo = 2
				}
				continue
			}
			u, v := (axis[2]-axis[0])/axis[1], (axis[3]-axis[0])/axis[1]
			if u > v {
				u, v = v, u
			}
			lo = math.Max(lo, u)
			hi = math.Min(hi, v)
		}
		result := uint32(0)
		rest := b
		if lo <= hi {
			result = monster.NavResultClipped
			rest = a
		}
		return monster.NewNavigationPath(a, b, rest, result, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	route := findMonsterRoute(from, goal, probe)
	if route == nil || route.Len() < 2 || calls > monsterRouteProbeBudget {
		t.Fatalf("detour missing or over budget: %v calls=%d", route, calls)
	}
	if route.Probes() != calls {
		t.Fatalf("the route reports %d probes, the search made %d", route.Probes(), calls)
	}
	last := from
	for i := 0; i < route.Len(); i++ {
		p := route.Point(i)
		if !routeClear(probe(last, p), p) {
			t.Fatal("route cut through obstacle")
		}
		last = p
	}
	if routeDistance(last, goal) > .01 {
		t.Fatal("route lost goal")
	}
}

func TestMonsterRouteDirectAndUnavailableWorkBudgets(t *testing.T) {
	from := monster.Pose{RegionID: 25000, X: 500, Y: 20, Z: 500}
	goal := from
	goal.X += 128
	for _, mode := range []string{"direct", "unavailable", "sealed", "barrier"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			route := findMonsterRoute(from, goal, func(a, b monster.Pose) *monster.NavigationPath {
				calls++
				if mode == "unavailable" {
					return nil
				}
				result := uint32(0)
				rest := b
				if mode == "sealed" || (mode == "barrier" && (a.X-564)*(b.X-564) <= 0) {
					result = monster.NavResultBlocked
					rest = a
				}
				return monster.NewNavigationPath(a, b, rest, result, func(float64, monster.Pose) (float64, bool) { return 20, true })
			})
			if mode == "direct" {
				if route == nil || route.Len() != 1 || calls != 1 {
					t.Fatal("direct path searched")
				}
			} else if route == nil || route.Status() == monster.NavigationRouteReady || calls > monsterRouteProbeBudget {
				t.Fatal("invented route or unbounded search")
			}
			if mode == "unavailable" && calls != 1 {
				t.Fatal("searched unavailable geometry")
			}
			if mode == "unavailable" && route.Status() != monster.NavigationGeometryUnavailable {
				t.Fatal("unavailable geometry became obstacle")
			}
			if mode == "sealed" && route.Status() != monster.NavigationRouteBlocked {
				t.Fatal("sealed start became unavailable")
			}
			if mode == "barrier" && route.Status() != monster.NavigationSearchExhausted {
				t.Fatal("budget exhaustion claimed no path exists")
			}
			if mode == "barrier" && calls != monsterRouteProbeBudget {
				t.Fatalf("budget not exercised: %d", calls)
			}
			if route.Probes() != calls {
				t.Fatalf("the route reports %d probes, the search made %d", route.Probes(), calls)
			}
		})
	}
}
