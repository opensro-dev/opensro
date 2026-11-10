/*
===========================================================================

navigation_metrics_test.go - route plans and path clips are counted by caller

===========================================================================
*/

package simulation

import (
	"strings"
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
			ops.PlanRoute = func(from monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
				return monster.NewNavigationRoute(to, []monster.Pose{corner, to}).WithProbes(17)
			}
			if _, frames := ops.planSegment(&instance, mover, goal, 20, wire.MoveStateRun, 10000); len(frames) == 0 {
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
TestLegCensusSnapshot

A finished leg folds into the monster_leg document (its own block
since #3119): the steps carry the modes they read, the mode partition
sums to the step count, and no leg.* key rides the navigation
snapshot. The mode mapper names the issue's six buckets and folds
every other mover mode into other.
================
*/
func TestLegCensusSnapshot(t *testing.T) {
	metrics := &NavigationMetrics{}
	stats := &legStats{actors: 2, rechecked: 1, cadenceArms: 1, gateSuspended: 1}
	stats.noteStep(stepModeIdle)
	stats.noteStep(stepModeIdle)
	stats.noteStep(stepModeChasing)
	stats.noteStep(stepModeOther)
	stats.restGuards[restGuardMode] = 2
	stats.restGuards[restGuardPass] = 1
	metrics.leg(stats)

	got := metrics.LegSnapshot()
	want := map[string]uint64{
		"count":        1,
		"actors":       2,
		"steps":        4,
		"rechecked":    1,
		"cadence_arms": 1,
		"suspended":    1,
		"rest.mode":    2,
		"rest.pass":    1,
		"mode.idle":    2,
		"mode.chasing": 1,
		"mode.other":   1,
	}
	if len(got) != len(want) {
		t.Fatalf("monster_leg = %v, want exactly %v", got, want)
	}
	var modeSum uint64
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("%s = %d, want %d (all: %v)", key, got[key], value, got)
		}
	}
	for mode := range stepModeCount {
		modeSum += got["mode."+stepModeNames[mode]]
	}
	if modeSum != got["steps"] {
		t.Fatalf("mode partition sums to %d, steps = %d", modeSum, got["steps"])
	}
	for key := range metrics.Snapshot() {
		if strings.HasPrefix(key, "leg.") {
			t.Fatalf("navigation snapshot still carries %q", key)
		}
	}

	modes := map[monster.MoverMode]stepMode{
		monster.MoverIdle:       stepModeIdle,
		monster.MoverWandering:  stepModeWandering,
		monster.MoverChasing:    stepModeChasing,
		monster.MoverAttacking:  stepModeAttacking,
		monster.MoverReturning:  stepModeReturning,
		monster.MoverPending:    stepModePending,
		monster.MoverSpawning:   stepModeOther,
		monster.MoverRecovering: stepModeOther,
		monster.MoverFollowing:  stepModeOther,
	}
	for mode, bucket := range modes {
		if got := stepModeFor(mode); got != bucket {
			t.Fatalf("stepModeFor(%v) = %v, want %v", mode, got, bucket)
		}
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
