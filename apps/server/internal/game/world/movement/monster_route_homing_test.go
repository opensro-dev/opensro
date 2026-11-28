/*
===========================================================================

monster_route_homing_test.go - the sliced homing search

A sliced search must settle on the one-shot search's route, probe for
probe, and never spend much more than a slice per call. The open-search
table must stay bounded.

===========================================================================
*/
package movement

import (
	"math"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

// wallStep is a synthetic step probe: open ground except a wall on
// x = 1200..1232 with a gap above z = gapZ (no gap when gapZ is +Inf).
func wallStep(gapZ float64) func(monster.Pose, monster.Pose, bool) (float64, bool, bool) {
	return func(a, b monster.Pose, _ bool) (float64, bool, bool) {
		lo, hi := math.Min(a.X, b.X), math.Max(a.X, b.X)
		if hi >= 1200 && lo <= 1232 {
			// The segment crosses the wall band: clear only through the gap.
			if math.Min(a.Z, b.Z) < gapZ {
				return a.Y, false, true
			}
		}
		return b.Y, true, true
	}
}

// slicedRoute runs search to settlement in slices, returning the route,
// the calls it took and the most probes one call spent.
func slicedRoute(t *testing.T, search *routeSearch, slice int) (*monster.NavigationRoute, int, int) {
	t.Helper()
	calls, most := 0, 0
	for {
		calls++
		route, spent := search.advance(slice)
		if spent > most {
			most = spent
		}
		if route != nil {
			return route, calls, most
		}
		if calls > 100000 {
			t.Fatal("the sliced search never settled")
		}
	}
}

func routePoints(r *monster.NavigationRoute) []monster.Pose {
	points := make([]monster.Pose, r.Len())
	for i := range points {
		points[i] = r.Point(i)
	}
	return points
}

func TestSlicedSearchSettlesOnTheOneShotRoute(t *testing.T) {
	from := monster.Pose{RegionID: 25000, X: 1100, Z: 900}
	goal := monster.Pose{RegionID: 25000, X: 1350, Z: 900}
	for _, tc := range []struct {
		name   string
		gapZ   float64
		budget int
		want   monster.NavigationRouteStatus
	}{
		{"detour through the gap", 1500, 20000, monster.NavigationRouteReady},
		{"unreachable goal exhausts", math.Inf(1), 6000, monster.NavigationSearchExhausted},
	} {
		t.Run(tc.name, func(t *testing.T) {
			whole := findMonsterRouteWithBudget(from, goal, wallStep(tc.gapZ), tc.budget)
			if whole.Status() != tc.want {
				t.Fatalf("one-shot status %v, want %v: the fixture lost its premise", whole.Status(), tc.want)
			}
			const slice = 64
			sliced, calls, most := slicedRoute(t, newRouteSearch(from, goal, wallStep(tc.gapZ), tc.budget), slice)
			if calls < 3 {
				t.Fatalf("settled in %d calls; the slice never paused the search", calls)
			}
			if most > slice+8 {
				t.Fatalf("one call spent %d probes, over the slice %d plus one expansion", most, slice)
			}
			if sliced.Status() != whole.Status() || sliced.Probes() != whole.Probes() {
				t.Fatalf("sliced %v/%d probes, one-shot %v/%d", sliced.Status(), sliced.Probes(), whole.Status(), whole.Probes())
			}
			if !reflect.DeepEqual(routePoints(sliced), routePoints(whole)) {
				t.Fatalf("sliced route %v differs from the one-shot route %v", routePoints(sliced), routePoints(whole))
			}
		})
	}
}

func TestHomingSearchAnswersPendingUntilSettled(t *testing.T) {
	var h homingSearches
	from := monster.Pose{RegionID: 25000, X: 1100, Z: 900}
	goal := monster.Pose{RegionID: 25000, X: 1350, Z: 900}
	key := homingKey{from, goal}
	opened := 0
	start := func() *routeSearch {
		opened++
		return newRouteSearch(from, goal, wallStep(1500), 20000)
	}
	pending := 0
	var route *monster.NavigationRoute
	for i := 0; i < 1000; i++ {
		route = h.plan(key, start, 64)
		if route.Status() != monster.NavigationSearchPending {
			break
		}
		pending++
		if route.Probes() > 64+8 {
			t.Fatalf("a pending call spent %d probes", route.Probes())
		}
	}
	if pending == 0 || route.Status() != monster.NavigationRouteReady {
		t.Fatalf("pending %d times, then %v; want pending first, then the detour", pending, route.Status())
	}
	if opened != 1 {
		t.Fatalf("the search was opened %d times, want once for one mover's retries", opened)
	}
	if len(h.open) != 0 {
		t.Fatalf("a settled search stays open (%d entries)", len(h.open))
	}
}

func TestHomingSearchTableStaysBounded(t *testing.T) {
	var h homingSearches
	goal := monster.Pose{RegionID: 25000, X: 1350, Z: 900}
	for i := 0; i < maxHomingSearches+3; i++ {
		from := monster.Pose{RegionID: 25000, X: 1100, Z: 900 + float64(i)}
		h.plan(homingKey{from, goal}, func() *routeSearch {
			return newRouteSearch(from, goal, wallStep(math.Inf(1)), 1<<20)
		}, 16)
	}
	if len(h.open) > maxHomingSearches {
		t.Fatalf("%d open searches, over the bound %d", len(h.open), maxHomingSearches)
	}
	// A search nobody advances is dropped after the idle bound.
	idle := homingKey{monster.Pose{RegionID: 25000, X: 1, Z: 1}, goal}
	h.open[idle] = &homingEntry{search: &routeSearch{}, lastCall: h.calls}
	h.calls += homingSearchIdleCalls + 1
	h.mu.Lock()
	h.dropIdleLocked()
	h.mu.Unlock()
	if _, ok := h.open[idle]; ok {
		t.Fatal("an idle search outlived the idle bound")
	}
}

func TestHomingSearchBusyEntryIsNotAdvancedTwice(t *testing.T) {
	var h homingSearches
	from := monster.Pose{RegionID: 25000, X: 1100, Z: 900}
	goal := monster.Pose{RegionID: 25000, X: 1350, Z: 900}
	key := homingKey{from, goal}
	h.open = map[homingKey]*homingEntry{key: {busy: true}}
	route := h.plan(key, func() *routeSearch {
		t.Fatal("a busy search was opened again")
		return nil
	}, 64)
	if route.Status() != monster.NavigationSearchPending || route.Probes() != 0 {
		t.Fatalf("busy entry answered %v/%d, want pending with no probes", route.Status(), route.Probes())
	}
}
