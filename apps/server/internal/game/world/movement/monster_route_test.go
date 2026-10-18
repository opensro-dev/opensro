/*
===========================================================================

monster_route_test.go - monster route detour and seam navigation tests

Verifies obstacle detour search budgets and sub-ULP outdoor sector-seam
navigation sampling in LivePoseAt.

===========================================================================
*/
package movement

import (
	"math"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

/*
================
TestMonsterRouteDetoursUsingValidatedEdges
================
*/
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

/*
================
TestMonsterRouteDirectAndUnavailableWorkBudgets
================
*/
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

/*
================
TestAdmittedCrossSectorMonsterPathResolvesSubULPSeamTick

Regression for #1210: when an admitted cross-sector monster path crosses
from an east sector (0x5c70, X=3) into the adjacent west sector (0x5c6f,
X=1549) at t = 6/748, float64 interpolation lands at -4.44e-16 before
folding into 0x5c6f. TerrainHeightAt on the folded pose must resolve
0x5c6f's offsetKey(0, 0) height grid instead of overflowing into
offsetKey(1, 0) and panicking LivePoseAt.
================
*/
func TestAdmittedCrossSectorMonsterPathResolvesSubULPSeamTick(t *testing.T) {
	v := realAuthorityValidator(t)
	const (
		east = uint16(0x5fa7)
		west = uint16(0x5fa6)
	)
	eastY, ok := v.TerrainHeightAt(east, 3, 1000)
	if !ok {
		t.Fatal("missing east terrain height")
	}
	westY, ok := v.TerrainHeightAt(west, 1913.6, 1000)
	if !ok {
		t.Fatal("missing west terrain height")
	}
	from := monster.Pose{RegionID: east, X: 3, Y: eastY, Z: 1000}
	goal := monster.Pose{RegionID: west, X: 1913.6, Y: westY, Z: 1000}
	path := v.PlanMonsterPath(from, goal)
	if path == nil {
		t.Fatal("expected admitted cross-sector path")
	}
	mover := monster.MoverState{
		From:     from,
		To:       path.Rest(),
		DepartMs: 1000,
		ArriveMs: 1047,
	}
	mover.AdoptNavigation(path)
	pose := mover.LivePoseAt(1015, nil)
	if pose.RegionID != west || pose.X < 0 || pose.X >= worldgeom.OutdoorRegionSize {
		t.Fatalf("seam tick pose=%+v, want west region with local X in [0, 1920)", pose)
	}
	wantY, ok := v.TerrainHeightAt(pose.RegionID, pose.X, pose.Z)
	if !ok || pose.Y != wantY {
		t.Fatalf("seam tick pose.Y=%v, want terrain height %v (ok=%v)", pose.Y, wantY, ok)
	}
}
