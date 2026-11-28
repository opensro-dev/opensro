/*
===========================================================================

monster_route_global_test.go - the homing search crosses terrain the
bounded detour search cannot see around (#4124)

The reported wedge: Lord Yarkan, kited out of his nest region (26246)
into 26244, marches home and stops at the Taklamakan gorge rim in
26245 (local 1580/801; the last standable rim tile is 1560/801 - the
reported tile itself is the blocked slope the mover's anchor overhangs).
The bounded detour search (256 probes) cannot
see around the rim and the straight-line march gains nothing there, so
the mover waits forever. The homing search - the same A* with the
homing budget - walks the rim to the pass and into the home region.

===========================================================================
*/
package movement

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

// gorgePose is a standing pose on the real terrain.
func gorgePose(t *testing.T, v *WaterValidator, region uint16, x, z float64) monster.Pose {
	t.Helper()
	y, ok := v.TerrainHeightAt(region, x, z)
	if !ok {
		t.Fatalf("no terrain at %d %.0f/%.0f", region, x, z)
	}
	return monster.Pose{RegionID: region, X: x, Y: y, Z: z}
}

/*
================
TestGlobalRouteCrossesTheGorgeRim
================
*/
func TestGlobalRouteCrossesTheGorgeRim(t *testing.T) {
	v := realAuthorityValidator(t)
	// The boot preload, so the probes see the whole outdoor geometry
	// the way the live planners do.
	if _, err := v.PreloadOutdoorNavigation(); err != nil {
		t.Fatalf("preload outdoor navigation: %v", err)
	}
	// The wedge the report names, and a goal just inside the nest
	// region past the rim: the short hop the march cannot make.
	from := gorgePose(t, v, 26245, 1560, 801)
	goal := gorgePose(t, v, 26246, 120, 801)

	start := time.Now()
	bounded := v.PlanMonsterRoute(from, goal)
	t.Logf("bounded: status=%v probes=%d in %v", bounded.Status(), bounded.Probes(), time.Since(start))
	if bounded.Status() == monster.NavigationRouteReady {
		t.Fatalf("the bounded search unexpectedly crosses the rim; the wedge premise is gone")
	}

	// The homing search runs in slices: each call answers pending
	// until it settles, and no call spends much more than a slice.
	start = time.Now()
	global, calls := v.PlanMonsterGlobalRoute(from, goal), 1
	for global.Status() == monster.NavigationSearchPending {
		if global.Probes() > monsterGlobalRouteSliceProbes+8 {
			t.Fatalf("one homing call spent %d probes, over the slice %d", global.Probes(), monsterGlobalRouteSliceProbes)
		}
		global, calls = v.PlanMonsterGlobalRoute(from, goal), calls+1
	}
	t.Logf("global: status=%v probes=%d points=%d in %d calls, %v", global.Status(), global.Probes(), global.Len(), calls, time.Since(start))
	if calls < 2 {
		t.Fatalf("the crossing settled in one call; the search is not sliced")
	}
	whole := findMonsterRouteWithBudget(from, goal, v.probeMonsterRouteStep, monsterGlobalRouteProbeBudget)
	if whole.Probes() != global.Probes() || whole.Len() != global.Len() {
		t.Fatalf("sliced crossing %d probes/%d points, one-shot %d/%d", global.Probes(), global.Len(), whole.Probes(), whole.Len())
	}
	if global.Status() != monster.NavigationRouteReady || global.Len() == 0 {
		t.Fatalf("the homing search does not cross the rim: status=%v", global.Status())
	}
	last := global.Point(global.Len() - 1)
	if last.RegionID != goal.RegionID {
		t.Fatalf("route ends in region %d, want the home region %d", last.RegionID, goal.RegionID)
	}
	if global.Probes() <= monsterRouteProbeBudget {
		t.Fatalf("crossing spent only %d probes; the homing budget did no work", global.Probes())
	}
	if global.Probes() > monsterGlobalRouteProbeBudget {
		t.Fatalf("crossing spent %d probes over the homing budget %d", global.Probes(), monsterGlobalRouteProbeBudget)
	}
}
