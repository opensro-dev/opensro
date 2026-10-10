/*
===========================================================================

navigation_metrics_test.go - route plans and path clips are counted by caller

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestNavigationMetricsCountPlansAndClipsByCaller

A routed leg counts one plan (its caller, outcome and probe cost) and the
clip of the leg it emits; a nil owner counts nothing and costs nothing.
================
*/
func TestNavigationMetricsCountPlansAndClipsByCaller(t *testing.T) {
	for _, mode := range []monster.MoverMode{monster.MoverChasing, monster.MoverFollowing, monster.MoverReturning} {
		t.Run(mode.String(), func(t *testing.T) {
			ops, instance, mover := routedFixture(t, mode)
			metrics := &NavigationMetrics{}
			ops.Navigation = metrics
			goal := mover.Pose
			goal.X += 120
			corner := mover.Pose
			corner.X += 60
			corner.Z += 32
			ops.PlanRoute = func(from, to monster.Pose) *monster.NavigationRoute {
				return monster.NewNavigationRoute(to, []monster.Pose{corner, to}).WithProbes(17)
			}
			if _, frames := ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000); len(frames) == 0 {
				t.Fatal("the routed leg was not emitted")
			}
			caller := navCallerNames[callerForMode(mode)]
			got := metrics.Snapshot()
			want := map[string]uint64{
				"plan." + caller + ".ready_detour": 1,
				"route_probes":                     17,
				"plan_probes.le32":                 1,
				"clip." + caller:                   1,
			}
			for key, value := range want {
				if got[key] != value {
					t.Fatalf("%s = %d, want %d (all: %v)", key, got[key], value, got)
				}
			}
			if len(got) != len(want) {
				t.Fatalf("unexpected counters: %v", got)
			}
		})
	}
	var none *NavigationMetrics
	none.plan(navChase, nil)
	none.clip(navChase)
	if len(none.Snapshot()) != 0 {
		t.Fatal("a nil owner counted")
	}
}

/*
================
TestNavigationMetricsOutcomes
================
*/
func TestNavigationMetricsOutcomes(t *testing.T) {
	metrics := &NavigationMetrics{}
	goal := monster.Pose{RegionID: 25000, X: 10, Z: 10}
	metrics.plan(navChase, monster.NewNavigationRoute(goal, []monster.Pose{goal}).WithProbes(1))
	metrics.plan(navChase, monster.UnresolvedNavigationRoute(goal, monster.NavigationRouteBlocked).WithProbes(40))
	metrics.plan(navChase, monster.UnresolvedNavigationRoute(goal, monster.NavigationSearchExhausted).WithProbes(256))
	metrics.plan(navChase, monster.UnresolvedNavigationRoute(goal, monster.NavigationGeometryUnavailable).WithProbes(1))
	metrics.plan(navChase, nil)
	got := metrics.Snapshot()
	want := map[string]uint64{
		"plan.chase.ready_direct": 1,
		"plan.chase.blocked":      1,
		"plan.chase.exhausted":    1,
		"plan.chase.unavailable":  2,
		"route_probes":            298,
		"plan_probes.le1":         3,
		"plan_probes.le128":       1,
		"plan_probes.gt128":       1,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %d, want %d (all: %v)", key, got[key], value, got)
		}
	}
}
