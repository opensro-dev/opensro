package simulation

import (
	"math"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func routedFixture(t *testing.T, mode monster.MoverMode) (*MonsterMoverOps, monster.Instance, monster.MoverState) {
	ops, instance, mover, _ := pursuitControlsFixture(t)
	if mode != monster.MoverChasing {
		mustMoverTransition(&mover, monster.MoverEventTargetLost, 0)
		if mode == monster.MoverFollowing {
			mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
			mustMoverTransition(&mover, monster.MoverEventFollowStarted, 123)
		}
	}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	ops.PlanPath = func(from, to monster.Pose) *monster.NavigationPath {
		return monster.NewNavigationPath(from, to, to, 0, func(f float64, _ monster.Pose) (float64, bool) {
			return from.Y + (to.Y-from.Y)*f + 7*math.Sin(math.Pi*f), true
		})
	}
	return ops, instance, mover
}

func TestSharedNavigationCachesCornersAndKeepsSurface(t *testing.T) {
	for _, mode := range []monster.MoverMode{monster.MoverChasing, monster.MoverFollowing, monster.MoverReturning} {
		t.Run(mode.String(), func(t *testing.T) {
			ops, instance, mover := routedFixture(t, mode)
			goal := mover.Pose
			goal.X += 120
			corner := mover.Pose
			corner.X += 60
			corner.Z += 32
			calls := 0
			ops.PlanRoute = func(from monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
				calls++
				return monster.NewNavigationRoute(to, []monster.Pose{corner, to})
			}
			next, frames := ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)
			if len(frames) == 0 || next.To.X != corner.X || next.To.Z != corner.Z || next.Mode() != mode {
				t.Fatal("first corner lost behavior")
			}
			_, _, x, _, z, _ := decodeGoalPayload(t, frames[len(frames)-1].Payload)
			if float64(x) != corner.X || float64(z) != corner.Z {
				t.Fatal("packet exposed final goal through obstacle")
			}
			middle := next.LivePoseAt((next.DepartMs+next.ArriveMs)/2, nil)
			if math.Abs(middle.Y-27) > .01 {
				t.Fatal("navigation surface became endpoint chord")
			}
			movingGoal := goal
			movingGoal.X += 8
			retained, repeat := ops.planSegment(instance, next, movingGoal, 20, wire.MoveStateRun, 10050)
			if len(repeat) != 0 || calls != 1 || retained.From != next.From || retained.To != next.To {
				t.Fatal("small target update resent/researched detour leg")
			}
			next, frames = ops.planSegment(instance, next, goal, 20, wire.MoveStateRun, next.ArriveMs)
			if calls != 1 || next.To.X != goal.X || next.To.Z != goal.Z || next.Mode() != mode || len(frames) == 0 {
				t.Fatal("corner arrival lost cached route or behavior")
			}
			next, frames = ops.planSegment(instance, next, goal, 20, wire.MoveStateRun, next.ArriveMs)
			if next.InFlight(100000) || len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
				t.Fatal("final arrival did not settle once")
			}
			if _, active := next.NavigationGoal(); active {
				t.Fatal("completed intent retained")
			}
		})
	}
}

func TestSharedNavigationBlockedRetryAndCancellation(t *testing.T) {
	for _, mode := range []monster.MoverMode{monster.MoverChasing, monster.MoverFollowing, monster.MoverReturning} {
		t.Run(mode.String(), func(t *testing.T) {
			ops, instance, mover := routedFixture(t, mode)
			goal := mover.Pose
			goal.X += 120
			calls := 0
			ops.PlanRoute = func(monster.Pose, NavOwner, monster.Pose) *monster.NavigationRoute { calls++; return nil }
			mover, frames := ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 10000)
			if calls != 1 || len(frames) != 0 || mover.Mode() != mode || !mover.NavigationWaiting(10001) {
				t.Fatal("blocked path discarded behavior or emitted move")
			}
			for tick := int64(10100); tick < 11000; tick += 100 {
				goal.X++ // movement of the same target cannot force 10 searches/sec
				mover, frames = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, tick)
				if calls != 1 || len(frames) != 0 {
					t.Fatal("failed route flooded geometry or packets")
				}
			}
			mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 11000)
			if calls != 2 {
				t.Fatal("retry deadline never reopened")
			}
			mustMoverTransition(&mover, monster.MoverEventRetaliationArmed, 456)
			if _, active := mover.NavigationGoal(); active {
				t.Fatal("retaliation retained old route")
			}
			mover, _ = ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 11001)
			if calls != 3 {
				t.Fatal("new owner inherited old retry deadline")
			}
		})
	}
}

func TestNavigationRejectsChangedActorDuringRouteSearch(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	goal := mover.Pose
	goal.X += 120
	ops.PlanRoute = func(from monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		s := ops.Monsters
		s.mu.Lock()
		defer s.mu.Unlock()
		state := s.populationForObject(monsterTestDivision, instance.Gid)
		row := state.instances.get(instance.Gid)
		row.Spawn.X++
		state.instances.set(instance.Gid, row)
		return monster.NewNavigationRoute(to, []monster.Pose{to})
	}
	frames := ops.commitSegment(monsterTestDivision, instance, mover, goal, 20, wire.MoveStateRun, 10000)
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if len(frames) != 0 || after != mover {
		t.Fatal("stale route published")
	}
}

func TestFollowRouteSurvivesClosedTimerAndControllerDeath(t *testing.T) {
	ops, parent, child, now := followFixture(t)
	calls := 0
	ops.PlanPath = func(from, to monster.Pose) *monster.NavigationPath {
		return monster.NewNavigationPath(from, to, to, 0, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	ops.PlanRoute = func(from monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
		calls++
		corner := from
		corner.X += 200
		corner.Z += 32
		return monster.NewNavigationRoute(to, []monster.Pose{corner, to})
	}
	mover := startFixtureFollow(t, ops, child, now)
	ops.stopOrAdvanceFollow("summon", child, mover, now+1)
	mover, _ = ops.Monsters.Mover("summon", child.Gid)
	if calls != 1 || !mover.InFlight(now+1) {
		t.Fatal("first callback did not plan route")
	}
	ops.Monsters.ApplyDamage("summon", parent.Gid, parent.CurrentHP)
	frames, _ := ops.stopOrAdvanceFollow("summon", child, mover, now+2)
	after, _ := ops.Monsters.Mover("summon", child.Gid)
	if len(frames) != 0 || calls != 1 || after != mover {
		t.Fatal("controller death canceled owned route")
	}
	frames, _ = ops.stopOrAdvanceFollow("summon", child, after, after.ArriveMs)
	continued, _ := ops.Monsters.Mover("summon", child.Gid)
	if len(frames) == 0 || calls != 1 || !continued.InFlight(after.ArriveMs) || continued.Mode() != monster.MoverFollowing {
		t.Fatal("dead controller stranded movement at a route corner")
	}
}

func TestHomingRouteProductionTickContinuesAndRetainsRunOnRetry(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "corners", true: "retry"}[blocked], func(t *testing.T) {
			ops, instance, mover := routedFixture(t, monster.MoverChasing)
			mover.Pose.X += 110
			ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
			calls := 0
			ops.PlanRoute = func(from monster.Pose, _ NavOwner, to monster.Pose) *monster.NavigationRoute {
				calls++
				if blocked && calls == 1 {
					return nil
				}
				corner := from
				corner.X -= 30
				corner.Z += 32
				return monster.NewNavigationRoute(to, []monster.Pose{corner, to})
			}
			ops.startReturnLeg(monsterTestDivision, instance, monster.ResolveTactics(instance), mover, monster.MoverEventTargetLost, 10000)
			mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if blocked {
				if !mover.NavigationWaiting(10001) || mover.Mode() != monster.MoverReturning {
					t.Fatal("failed home plan discarded return intent")
				}
				frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, 10500)
				if len(frames) != 0 || calls != 1 {
					t.Fatal("blocked home flooded")
				}
				ops.advanceInstance(monsterTestDivision, instance, nil, 11000)
				mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
				if mover.Channel != wire.MoveStateRun || !mover.InFlight(11000) {
					t.Fatal("retry inherited old walking channel")
				}
			}
			before := calls
			frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, mover.ArriveMs)
			mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if calls != before || mover.Mode() != monster.MoverReturning || len(frames) == 0 {
				t.Fatal("home corner lost cached goal")
			}
			frames, _ = ops.advanceInstance(monsterTestDivision, instance, nil, mover.ArriveMs)
			mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			if mover.Mode() != monster.MoverIdle || len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
				t.Fatal("final home arrival not settled")
			}
		})
	}
}

/*
================
TestRouteReplanCarriesTheLiveOwner

A replan hands the route planner the mover's live surface owner (#1732):
the original's route reads the actor's cell from its position record, so
the cave route must start from the cell the last walk reached, not one
re-guessed from the position.
================
*/
func TestRouteReplanCarriesTheLiveOwner(t *testing.T) {
	ops, instance, mover := routedFixture(t, monster.MoverChasing)
	cell := NavOwner{Kind: NavOwnerDungeon, Object: NavObjectCell{Surface: 0x8001, Object: 67, Cell: 2}}
	from := mover.Pose
	arrived := from
	arrived.X += 4
	mover.From, mover.To, mover.DepartMs, mover.ArriveMs = from, arrived, 1000, 2000
	mover.AdoptNavigation(monster.NewNavigationPath(from, arrived, arrived, 0, func(float64, monster.Pose) (float64, bool) { return from.Y, true }).
		WithOwners([]NavOwnerSpan{{From: 0, To: 1, Owner: cell}}))
	if got := mover.LiveNavOwner(5000); got != cell {
		t.Fatalf("fixture: live owner %+v, want %+v", got, cell)
	}
	goal := arrived
	goal.X += 120
	var planned NavOwner
	calls := 0
	ops.PlanRoute = func(_ monster.Pose, fromOwner NavOwner, to monster.Pose) *monster.NavigationRoute {
		calls++
		planned = fromOwner
		return monster.NewNavigationRoute(to, []monster.Pose{to})
	}
	ops.planSegment(instance, mover, goal, 20, wire.MoveStateRun, 5000)
	if calls == 0 {
		t.Fatal("the route planner was never asked")
	}
	if planned != cell {
		t.Fatalf("the route planner got owner %+v, want the live owner %+v", planned, cell)
	}
}
