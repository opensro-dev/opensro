/*
===========================================================================

monsternavigation_memo_test.go - the exact failed-route memo (#1820 B)

A stuck mover used to re-run the identical failed route search every
retry second. The memo answers a retry from the mover's failure record
when the inputs are bit-identical and the plane's routes are pure, and
must never change a single emitted frame.

===========================================================================
*/

package simulation

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
)

// memoFixture is a chasing mover whose route planner answers with the
// scripted status and counts its searches.
func memoFixture(t *testing.T, status monster.NavigationRouteStatus) (*MonsterMoverOps, monster.Instance, monster.MoverState, monster.Pose, *int) {
	t.Helper()
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	goal := mover.Pose
	goal.X += 120
	calls := new(int)
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		*calls++
		if status == monster.NavigationRouteReady {
			return monster.NewNavigationRoute(to, []monster.Pose{to})
		}
		return monster.UnresolvedNavigationRoute(to, status)
	}
	ops.RouteMemo = func(from, goal monster.Pose) bool { return true }
	ops.Navigation = &NavigationMetrics{}
	return ops, instance, mover, goal, calls
}

func TestRouteMemoStationaryTargetSearchesOnce(t *testing.T) {
	ops, instance, mover, goal, calls := memoFixture(t, monster.NavigationRouteBlocked)

	mover, frames := ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if *calls != 1 || len(frames) != 0 || !mover.NavigationWaiting(10001) {
		t.Fatalf("first failure: calls=%d frames=%d waiting=%v", *calls, len(frames), mover.NavigationWaiting(10001))
	}
	if _, _, status, failed := mover.RouteFailure(); !failed || status != monster.NavigationRouteBlocked {
		t.Fatalf("failure not recorded: %v %v", status, failed)
	}
	// Retries at the unchanged 1000 ms cadence are answered from the
	// record: no new search, still waiting, no frames.
	for _, retry := range []int64{11000, 12000, 13000} {
		mover, frames = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, retry)
		if *calls != 1 {
			t.Fatalf("retry at %d searched again (calls=%d)", retry, *calls)
		}
		if len(frames) != 0 || !mover.NavigationWaiting(retry+1) {
			t.Fatalf("memo retry at %d emitted frames or stopped waiting", retry)
		}
	}
	if got := ops.Navigation.Snapshot()["plan.chase.memo"]; got != 3 {
		t.Fatalf("plan.chase.memo = %d, want 3", got)
	}
}

// The failure record also keys the retained nav owner (design deviation
// 1: the wired cave route starts from the owner's cell). A retry from the
// same pose and goal under a different owner must search again - here the
// wait itself drops the owner, which is exactly the production cave flow:
// the record made under the deck owner never hits, one real search
// re-records under the unresolved owner, and only then does the memo hit.
func TestRouteMemoOwnerIsPartOfTheKey(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	goal := mover.Pose
	goal.X += 120
	deck := worldgeom.NavOwner{Kind: worldgeom.NavOwnerObject, Object: worldgeom.NavObjectCell{Surface: 0x5B87, Object: 4, Cell: 2}}
	rest := mover.Pose
	mover.AdoptNavigation(monster.NewNavigationPath(rest, rest, rest, 0,
		func(float64, monster.Pose) (float64, bool) { return 20, true }).
		WithOwners([]worldgeom.NavOwnerSpan{{From: 0, To: 1, Owner: deck}}))
	calls := 0
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		calls++
		return monster.UnresolvedNavigationRoute(to, monster.NavigationRouteBlocked)
	}
	ops.RouteMemo = func(from, goal monster.Pose) bool { return true }
	ops.Navigation = &NavigationMetrics{}

	mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)
	if calls != 1 {
		t.Fatalf("first failure: calls=%d, want 1", calls)
	}
	if _, owner, _, failed := mover.RouteFailure(); !failed || owner != deck {
		t.Fatalf("recorded owner = %+v (failed %v), want the deck owner", owner, failed)
	}
	// Same pose, same goal, owner now unresolved: not a memo hit.
	mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 11000)
	if calls != 2 {
		t.Fatalf("retry under a different owner: calls=%d, want 2 (the owner is part of the key)", calls)
	}
	if _, owner, _, _ := mover.RouteFailure(); owner != (worldgeom.NavOwner{}) {
		t.Fatalf("re-recorded owner = %+v, want unresolved", owner)
	}
	// The re-recorded failure answers the next retry.
	mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 12000)
	if calls != 2 || ops.Navigation.Snapshot()["plan.chase.memo"] != 1 {
		t.Fatalf("third retry: calls=%d memo=%d, want 2/1", calls, ops.Navigation.Snapshot()["plan.chase.memo"])
	}
}

func TestRouteMemoMovedTargetOrMoverSearchesAgain(t *testing.T) {
	ops, instance, mover, goal, calls := memoFixture(t, monster.NavigationRouteBlocked)
	mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)

	// The target moved: the changed goal cancels the intent, record and
	// all, and the retry searches.
	movedGoal := goal
	movedGoal.X += 40
	mover, _ = ops.planSegment(instance, mover, movedGoal, 20, wire.MoveStateRun, 11000)
	if *calls != 2 {
		t.Fatalf("moved target: calls=%d, want 2", *calls)
	}
	// The record now tracks the new goal: its retry is memoized.
	mover, _ = ops.planSegment(instance, mover, movedGoal, 20, wire.MoveStateRun, 12000)
	if *calls != 2 {
		t.Fatalf("retry at moved goal searched (calls=%d)", *calls)
	}

	// Fresh failure at the new goal, then the mover itself is pushed:
	// the recorded pose no longer matches and the retry searches.
	mover, _ = ops.planSegment(instance, mover, movedGoal, 20, wire.MoveStateRun, 12000)
	if *calls != 2 {
		t.Fatalf("memoized retry searched (calls=%d)", *calls)
	}
	pushed := mover
	pushed.Pose.X += 5
	pushed, _ = ops.planSegment(instance, pushed, movedGoal, 20, wire.MoveStateRun, 13000)
	if *calls != 3 {
		t.Fatalf("pushed mover: calls=%d, want 3", *calls)
	}
}

func TestRouteMemoGates(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    monster.NavigationRouteStatus
		nilRoute  bool
		memo      func(from, goal monster.Pose) bool
		wantCalls int // searches after the first retry
	}{
		{"blocked is recorded", monster.NavigationRouteBlocked, false, func(monster.Pose, monster.Pose) bool { return true }, 1},
		{"exhausted is recorded", monster.NavigationSearchExhausted, false, func(monster.Pose, monster.Pose) bool { return true }, 1},
		{"unavailable is not recorded", monster.NavigationGeometryUnavailable, false, func(monster.Pose, monster.Pose) bool { return true }, 2},
		{"nil route is not recorded", monster.NavigationRouteBlocked, true, func(monster.Pose, monster.Pose) bool { return true }, 2},
		{"nil gate means no memo", monster.NavigationRouteBlocked, false, nil, 2},
		{"false gate means no memo", monster.NavigationRouteBlocked, false, func(monster.Pose, monster.Pose) bool { return false }, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, instance, mover := routedFixture(t, monster.MoverChasing)
			goal := mover.Pose
			goal.X += 120
			calls := 0
			ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
				calls++
				if tc.nilRoute {
					return nil
				}
				return monster.UnresolvedNavigationRoute(to, tc.status)
			}
			ops.RouteMemo = tc.memo
			mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)
			mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 11000)
			if calls != tc.wantCalls {
				t.Fatalf("searches after first retry = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

// memoFrameStream drives one chaser through a partial route, a blocked
// replan and two retries, collecting every emitted frame.
func memoFrameStream(t *testing.T, memoOn bool) (string, int) {
	t.Helper()
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	goal := mover.Pose
	goal.X += 120
	waypoint := mover.Pose
	waypoint.X += 60
	calls := 0
	ops.PlanRoute = func(_ monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		calls++
		if calls == 1 {
			// Corners end short of the goal, so arriving exhausts the
			// route and forces the blocked search below.
			return monster.NewNavigationRoute(to, []monster.Pose{waypoint})
		}
		return monster.UnresolvedNavigationRoute(to, monster.NavigationRouteBlocked)
	}
	if memoOn {
		ops.RouteMemo = func(from, goal monster.Pose) bool { return true }
	}
	var stream string
	emit := func(frames []Frame) {
		for _, f := range frames {
			stream += fmt.Sprintf("%04x:%x;", f.Opcode, f.Payload)
		}
	}
	mover, frames := ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)
	emit(frames)
	arrive := mover.ArriveMs
	mover, frames = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, arrive)
	emit(frames)
	for _, retry := range []int64{arrive + 1000, arrive + 2000} {
		mover, frames = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, retry)
		emit(frames)
	}
	return stream, calls
}

func TestRouteMemoFrameStreamIdenticalOnOff(t *testing.T) {
	onStream, onCalls := memoFrameStream(t, true)
	offStream, offCalls := memoFrameStream(t, false)
	if onStream == "" {
		t.Fatal("scenario emitted no frames; the comparison proves nothing")
	}
	if onStream != offStream {
		t.Fatalf("frame stream diverged:\n memo: %s\nplain: %s", onStream, offStream)
	}
	// First route + blocked replan search in both; the two retries are
	// the only difference, and only in searches, never in frames.
	if onCalls != 2 || offCalls != 4 {
		t.Fatalf("searches memo=%d plain=%d, want 2 vs 4", onCalls, offCalls)
	}
}
