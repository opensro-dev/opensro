/*
===========================================================================

monsterhoming_march_test.go - a homing monster marches home when the
bounded detour search cannot see its distant goal (#4124)

The outdoor route owner is a bounded local detour search: beyond a few
hundred units its probe budget exhausts and it reports no route. The
native homing mover instead re-aims at its stored home goal every tick
and walks the ground toward it. These tests pin the port's equivalent:
when the detour search fails, the mover falls back to the local path's
own march (each leg commits to the furthest pose the path can walk
toward the goal) instead of waiting in place until the stuck policy
detaches its nest and parks it on the battlefield forever. A march that
makes no progress (a wall) still waits, so the native stuck answer
keeps its meaning.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// marchPath is a local path planner that walks up to a stride toward
// the goal and clips there, the way real geometry clips a long march;
// a goal inside the stride is reached exactly.
func marchPath(from, goal monster.Pose) *monster.NavigationPath {
	rest := goal
	result := uint32(0)
	if d := planarDistance(from, goal); d > 80 {
		rest.X = from.X + (goal.X-from.X)*80/d
		rest.Z = from.Z + (goal.Z-from.Z)*80/d
		result = monster.NavResultClipped
	}
	return monster.NewNavigationPath(from, goal, rest, result,
		func(float64, monster.Pose) (float64, bool) { return 20, true })
}

// exhaustedRoute is the bounded detour search at a distant goal: every
// probe spent, no route (the Taklamakan 26244 -> 26246 answer).
func exhaustedRoute(goal monster.Pose) *monster.NavigationRoute {
	return monster.UnresolvedNavigationRoute(goal, monster.NavigationSearchExhausted).WithProbes(256)
}

func TestHomingMarchesHomeWhenRouteSearchExhausts(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	ops.PlanPath = marchPath
	mover.Pose.X += 110
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	anchor := anchorPose(&instance)
	ops.startReturnLeg(monsterTestDivision, &instance, monster.ResolveTactics(&instance), mover, monster.MoverEventTargetLost, 10000)
	now := int64(10000)
	for i := 0; i < 160; i++ {
		now += 600
		ops.advanceInstance(monsterTestDivision, instance, nil, now)
		mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.Mode() == monster.MoverIdle {
			break
		}
	}
	if mover.Mode() != monster.MoverIdle {
		t.Fatalf("mode = %v, want idle after walking home", mover.Mode())
	}
	if d := planarDistance(mover.Pose, anchor); d > 25 {
		t.Fatalf("settled %.0f units from home, want the walk to reach the nest (pose %v, anchor %v)", d, mover.Pose, anchor)
	}
	s := ops.Monsters
	s.mu.Lock()
	stored := s.division(monsterTestDivision).instances.get(instance.Gid)
	s.mu.Unlock()
	if stored.NestDetached {
		t.Fatal("the march home detached the nest; the stuck policy answered a walkable route")
	}
}

func TestSegmentMarchesWhenRouteSearchFails(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverReturning)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	ops.PlanPath = marchPath
	goal := mover.Pose
	goal.X += 120
	next, frames := ops.planSegment(&instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if len(frames) == 0 || !next.InFlight(10000) {
		t.Fatal("an exhausted detour search parked the homing mover instead of marching")
	}
	if d := planarDistance(next.To, goal); d >= 120 || next.To.X <= mover.Pose.X {
		t.Fatalf("marched leg lands at %v, no closer to the goal %v", next.To, goal)
	}
}

// The march is the homing state's answer, not a new chase rule: a
// chaser whose detour search fails keeps the pinned wait even when
// the ground ahead is walkable.
func TestSegmentChaseStillWaitsWhenRouteSearchFails(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	ops.PlanPath = marchPath
	goal := mover.Pose
	goal.X += 120
	next, frames := ops.planSegment(&instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if len(frames) != 0 || !next.NavigationWaiting(10001) || next.Mode() != monster.MoverChasing {
		t.Fatal("a chaser must keep the detour wait; the march belongs to homing")
	}
}

func TestSegmentStillWaitsWhenMarchMakesNoProgress(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverReturning)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	// A wall at the mover's feet: the path can only rest where it stands.
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		return monster.NewNavigationPath(from, goal, from, monster.NavResultBlocked,
			func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	goal := mover.Pose
	goal.X += 120
	next, frames := ops.planSegment(&instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if len(frames) != 0 || !next.NavigationWaiting(10001) || next.Mode() != monster.MoverReturning {
		t.Fatal("a march with no progress must still wait for the stuck policy to answer")
	}
}
