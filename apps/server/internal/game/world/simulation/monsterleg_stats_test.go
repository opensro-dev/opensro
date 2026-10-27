/*
===========================================================================

monsterleg_stats_test.go - slow-leg attribution records (#1820 C)

Part C is metrics only: these tests pin that the record is populated
(plans, probes, memo hits, clips, woken/resumed) and that it never
outlives its leg. No wall-clock thresholds are asserted.
===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

func TestLegStatsRecordsPlansAndMemo(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	goal := mover.Pose
	goal.X += 120
	calls := 0
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		calls++
		return monster.UnresolvedNavigationRoute(to, monster.NavigationRouteBlocked).WithProbes(200)
	}
	ops.RouteMemo = func(from, goal monster.Pose) bool { return true }
	ops.Navigation = &NavigationMetrics{}
	ops.leg = &legStats{}

	mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)
	st := ops.leg
	if st.plans[navChase][navBlocked] != 1 || st.planProbes[navChase] != 200 || st.maxPlanProbes != 200 {
		t.Fatalf("plan record = plans %v probes %d max %d, want one blocked 200-probe chase plan",
			st.plans[navChase], st.planProbes[navChase], st.maxPlanProbes)
	}
	// The memoized retry records a memo hit, not another plan.
	mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 11000)
	if calls != 1 || st.memo[navChase] != 1 || st.plans[navChase][navBlocked] != 1 {
		t.Fatalf("memo retry: calls=%d memo=%d plans=%d, want 1/1/1", calls, st.memo[navChase], st.plans[navChase][navBlocked])
	}
}

func TestMonsterLegStatsWanderLeg(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, _ := monsterLegFixture(t, passiveTactics())
	ops.Rand = sequenceRand(0.5, 0, 39.0/32768, 90.0/32768, 1.0/32768, 999.0/32768)
	// The wander probe clips only with a planner wired (hasPlanner);
	// answer every probe with the straight path.
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		return monster.NewNavigationPath(from, goal, goal, 0, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	sessions := []SessionSnapshot{playerSessionAt(1, 1200, 1200)}
	push := &fakePusher{}

	recordTestMonsterBootstrap(ops, sessions, t0)
	ops.RunMonsterLeg(t0, sessions, push)

	st := ops.lastLeg
	if st == nil {
		t.Fatal("leg left no attribution record")
	}
	if ops.leg != nil {
		t.Fatal("leg stats stayed installed after the leg")
	}
	if st.actors != 2 {
		t.Fatalf("actors = %d, want the fixture's two nested monsters", st.actors)
	}
	if st.clips[navWander] == 0 {
		t.Fatal("wander leg recorded no path clip")
	}
	if st.dormancy+st.visibility+st.batch > st.total {
		t.Fatalf("phase walls %v+%v+%v exceed the leg total %v", st.dormancy, st.visibility, st.batch, st.total)
	}
	if st.advance > st.batch {
		t.Fatalf("advance %v exceeds the batch loop %v that contains it", st.advance, st.batch)
	}
}

func TestPrepareDormancyReturnsWokenSet(t *testing.T) {
	s, d := dormancyPhaseWorld(t, 5)
	session := SessionSnapshot{DivisionID: "sleep", Population: d.lease}
	session.World.Spawn.RegionID = 0x6061 // next sector, outside visible range

	woken := s.prepareDormancy(200000, "sleep", []SessionSnapshot{session})
	key := populationKey{division: "sleep", lease: d.lease}
	if len(woken[key]) != 5 {
		t.Fatalf("woken set = %v, want the five seeded gids", woken)
	}
	if again := s.prepareDormancy(200001, "sleep", []SessionSnapshot{session}); len(again) != 0 {
		t.Fatalf("second pass woke %v, want nobody", again)
	}
}

func TestMonsterLegWokenAndResumedCounts(t *testing.T) {
	const actors = 40
	s, d := dormancyPhaseWorld(t, actors)
	ops := &MonsterMoverOps{
		Monsters:   s,
		TacticsFor: fixedTactics(passiveTactics()),
		Rand:       func() float64 { return 0.5 },
		PlanPath: func(from, goal monster.Pose) *monster.NavigationPath {
			return nil // geometry-free fixture: an unavailable region
		},
	}
	ops.divisionID = "sleep"
	session := SessionSnapshot{DivisionID: "sleep", Population: d.lease}
	session.World.Spawn.RegionID = 0x6061
	sessions := []SessionSnapshot{session}
	push := &fakePusher{}

	ops.RunMonsterLeg(200000, sessions, push)
	if ops.lastLeg.woken != actors {
		t.Fatalf("woken = %d, want %d", ops.lastLeg.woken, actors)
	}
	// Part A's invariant, seen through part C: the phase-continuous wake
	// dispatches nobody on the wake tick itself.
	if ops.lastLeg.resumed != 0 {
		t.Fatalf("resumed = %d on the wake tick, want 0", ops.lastLeg.resumed)
	}
	resumed := 0
	for tick := int64(200100); tick <= 202100; tick += 100 {
		ops.RunMonsterLeg(tick, sessions, push)
		if ops.lastLeg.woken != 0 {
			t.Fatalf("leg at %d woke %d more, want 0", tick, ops.lastLeg.woken)
		}
		resumed += ops.lastLeg.resumed
	}
	if resumed != actors {
		t.Fatalf("resumed over the spread = %d, want %d", resumed, actors)
	}
	if len(ops.resumePending) != 0 {
		t.Fatalf("resume pending not drained: %v", ops.resumePending)
	}
}
