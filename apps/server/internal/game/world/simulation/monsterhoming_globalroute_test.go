/*
===========================================================================

monsterhoming_globalroute_test.go - a homing mover whose march stalls
at a terrain wall walks the global route home (#4124)

The bounded detour search is local and the march is greedy: at the
Taklamakan gorge rim both fail - the search exhausts its probes and the
straight-line march gains nothing against the wall - so the mover used
to wait until the stuck policy detached its nest and parked it forever.
The homing mover instead gets one global search (the same A* with the
homing budget) and walks its waypoints home. Chase and follow never
consult it: their detour economics stay pinned.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// wallPath is a local path planner walled at the mover's feet: the path
// can only rest where it stands, so the march gains nothing.
func wallPath(from, goal monster.Pose) *monster.NavigationPath {
	return monster.NewNavigationPath(from, goal, from, monster.NavResultBlocked,
		func(float64, monster.Pose) (float64, bool) { return 20, true })
}

/*
================
segmentCrossesGorge

Whether the straight segment from -> goal passes through the gorge box:
the vertical strip west..east around the home row's z.
================
*/
func segmentCrossesGorge(from, goal monster.Pose, west, east, rowZ float64) bool {
	lo, hi := from.X, goal.X
	if lo > hi {
		lo, hi = hi, lo
	}
	if hi < west || lo > east {
		return false
	}
	// The z values where the segment enters and leaves the strip.
	zAt := func(x float64) float64 {
		if goal.X == from.X {
			return from.Z
		}
		return from.Z + (goal.Z-from.Z)*(x-from.X)/(goal.X-from.X)
	}
	clamp := func(x float64) float64 {
		if x < lo {
			return lo
		}
		if x > hi {
			return hi
		}
		return x
	}
	zIn, zOut := zAt(clamp(west)), zAt(clamp(east))
	if zIn > zOut {
		zIn, zOut = zOut, zIn
	}
	return zOut >= rowZ-10 && zIn <= rowZ+10
}

// gorgePath is marchPath with a gorge in the way: a stride whose
// straight segment crosses the gorge box rests where it stands (the
// rim wall), so the march stalls at the rim while a route around the
// box stays walkable leg by leg.
func gorgePath(west, east, rowZ float64) func(from, goal monster.Pose) *monster.NavigationPath {
	return func(from, goal monster.Pose) *monster.NavigationPath {
		if segmentCrossesGorge(from, goal, west, east, rowZ) {
			return wallPath(from, goal)
		}
		return marchPath(from, goal)
	}
}

func TestSegmentUsesGlobalRouteWhenMarchStalls(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverReturning)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	// The straight line home crosses the gorge; the legs around it walk.
	ops.PlanPath = gorgePath(mover.Pose.X+45, mover.Pose.X+85, mover.Pose.Z)
	globalCalls := 0
	ops.PlanGlobalRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		globalCalls++
		around := to
		around.X -= 85
		around.Z += 55
		return monster.NewNavigationRoute(to, []monster.Pose{around, to})
	}
	goal := mover.Pose
	goal.X += 120
	next, frames := ops.planSegment(&instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if globalCalls != 1 {
		t.Fatalf("global search consulted %d times, want 1", globalCalls)
	}
	if len(frames) == 0 || !next.InFlight(10000) {
		t.Fatal("a ready global route parked the homing mover instead of walking it")
	}
	point, ready := next.NavigationWaypoint()
	if !ready || planarDistance(point, goal) >= 120 {
		t.Fatalf("walking waypoint %v (ready %v) does not head for the goal %v", point, ready, goal)
	}
}

// The global search is the homing state's answer, not a new chase rule:
// a chaser whose detour search fails keeps the pinned wait and never
// pays for the global search.
func TestSegmentChaseNeverConsultsGlobalRoute(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	ops.PlanPath = wallPath
	globalCalls := 0
	ops.PlanGlobalRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		globalCalls++
		return monster.NewNavigationRoute(to, []monster.Pose{to})
	}
	goal := mover.Pose
	goal.X += 120
	next, frames := ops.planSegment(&instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if globalCalls != 0 {
		t.Fatalf("a chaser consulted the global search %d times", globalCalls)
	}
	if len(frames) != 0 || !next.NavigationWaiting(10001) || next.Mode() != monster.MoverChasing {
		t.Fatal("a chaser must keep the detour wait; the global route belongs to homing")
	}
}

func TestSegmentStillWaitsWhenGlobalRouteFails(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverReturning)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	ops.PlanPath = wallPath
	ops.PlanGlobalRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	goal := mover.Pose
	goal.X += 120
	next, frames := ops.planSegment(&instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if len(frames) != 0 || !next.NavigationWaiting(10001) || next.Mode() != monster.MoverReturning {
		t.Fatal("a failed global search must keep the wait for the stuck policy to answer")
	}
}

func TestHomingWalksHomeOnGlobalRoute(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	ops.PlanPathFrom = nil
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		return exhaustedRoute(to)
	}
	anchor := anchorPose(&instance)
	ops.PlanPath = gorgePath(anchor.X+35, anchor.X+75, anchor.Z)
	globalCalls := 0
	ops.PlanGlobalRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		globalCalls++
		// Waypoints a stride apart, the way the real search's grid
		// neighbours arrive: every leg clips exactly to its point.
		around := to
		around.X = anchor.X + 65
		around.Z = anchor.Z + 60
		mid := to
		mid.X = anchor.X + 38
		mid.Z = anchor.Z + 30
		return monster.NewNavigationRoute(to, []monster.Pose{around, mid, to})
	}
	mover.Pose.X += 110
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
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
	if globalCalls == 0 {
		t.Fatal("the walk home never consulted the global search; the march alone cannot cross the gorge")
	}
	if d := planarDistance(mover.Pose, anchor); d > 25 {
		t.Fatalf("settled %.0f units from home, want the walk to reach the nest (pose %v, anchor %v)", d, mover.Pose, anchor)
	}
	s := ops.Monsters
	s.mu.Lock()
	stored := s.division(monsterTestDivision).instances.get(instance.Gid)
	s.mu.Unlock()
	if stored.NestDetached {
		t.Fatal("the walk home detached the nest; the stuck policy answered a walkable route")
	}
}
