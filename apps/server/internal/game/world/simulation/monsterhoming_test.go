/*
===========================================================================

monsterhoming_test.go - the walk home keeps one stored destination

Issue #1703: the destination is selected once when the walk home begins
(HOMING entry) and every leg re-reads the same point, instead of rolling a
fresh candidate inside the nest radius per leg. Arrival is within 5 units
(3-D) of the stored point, and only counts in the goal's own region.

===========================================================================
*/

package simulation

import (
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestMonsterHomingStoresOneGoalForTheWholeWalk

A monster 120 units from its nest loses its target and walks home. Across
every tick of the walk the stored goal must be the same point (the
alternating random source would move a re-rolled candidate by 3 units),
and the walk must end at that point - not at the first step inside the
nest radius. A later walk home selects a fresh goal.
================
*/
func TestMonsterHomingStoresOneGoalForTheWholeWalk(t *testing.T) {
	ops, instance, mover, _ := pursuitControlsFixture(t)
	mover.Pose.X = 1120
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	draws := 0
	ops.Rand = func() float64 {
		draws++
		if draws%2 == 1 {
			return 0
		}
		return 0.999
	}
	// Clip every leg to 30 units, so the 120-unit walk takes several legs
	// and re-enters the return planner from idle between them: the stored
	// goal has to survive every re-entry, not just the first plan.
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		clipped := goal
		if d := planarDistance(from, goal); d > 30 {
			f := float64(30) / d
			clipped.X = from.X + (goal.X-from.X)*f
			clipped.Z = from.Z + (goal.Z-from.Z)*f
			clipped.Y = from.Y + (goal.Y-from.Y)*f
		}
		return testShortNavPath(from, clipped)
	}

	nowMs := int64(10_000)
	var goal monster.Pose
	seenReturning := false
	arrived := false
	for step := 0; step < 80 && !arrived; step++ {
		current, _ := ops.Monsters.Get(monsterTestDivision, instance.Gid)
		ops.advanceInstance(monsterTestDivision, current, nil, nowMs)
		m, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		switch m.Mode() {
		case monster.MoverReturning:
			stored, ok := m.HomingGoal()
			if !ok {
				t.Fatal("returning mover holds no stored homing goal")
			}
			if !seenReturning {
				goal, seenReturning = stored, true
				if goal.X <= 1000 || goal.Z != 1000 {
					t.Fatalf("stored goal = %+v, want east of the anchor on its row", goal)
				}
				if d := tacticsDistance3D(anchorPose(instance), goal); d < 6 || d > 14 {
					t.Fatalf("stored goal distance from anchor = %v, want the nest scatter band", d)
				}
			} else if stored != goal {
				t.Fatalf("homing goal moved mid-walk: %+v, want %+v (one goal per walk)", stored, goal)
			}
		case monster.MoverIdle:
			if seenReturning {
				arrived = true
			}
		default:
			t.Fatalf("unexpected mode %v during walk home", m.Mode())
		}
		nowMs += 500
	}
	if !arrived {
		t.Fatal("walk home never ended in idle")
	}
	final, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if d := tacticsDistance3D(final.Pose, goal); d >= homingArrivalDistance {
		t.Fatalf("walk ended %v from the stored goal, want within %v (the point, not the radius)", d, homingArrivalDistance)
	}

	// A new walk home after leaving MoverReturning selects a fresh goal:
	// begun from the west side, it lands west of the anchor.
	final.Pose.X = 880
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, final)
	next, _ := ops.planReturnLeg(instance, final, monster.MoverEventHomingRequired, nowMs+100_000)
	fresh, ok := next.HomingGoal()
	if !ok {
		t.Fatal("second walk home stored no goal")
	}
	if fresh.X >= 1000 || fresh == goal {
		t.Fatalf("second walk goal = %+v, want a fresh selection west of the anchor (first was %+v)", fresh, goal)
	}
}

/*
================
TestMonsterHomingArrivalEndsWalkAtEntry

A monster that loses its target already standing within 5 units (3-D) of
the destination its walk would select ends the walk at entry: no segment,
idle with a deadline, goal discarded. The same coordinates in a different
region do not count - across regions the walk plans on.
================
*/
func TestMonsterHomingArrivalEndsWalkAtEntry(t *testing.T) {
	ops, instance, mover, _ := pursuitControlsFixture(t)
	ops.Rand = func() float64 { return 0 }
	mover.Pose.X = 1008

	next, frames := ops.planReturnLeg(instance, mover, monster.MoverEventTargetLost, 20_000)
	if len(frames) != 0 {
		t.Fatalf("arrival at entry emitted %d frames, want none", len(frames))
	}
	if next.Mode() != monster.MoverIdle {
		t.Fatalf("mode after arrival at entry = %v, want idle", next.Mode())
	}
	if next.ArriveMs != 0 || next.DepartMs != 0 {
		t.Fatalf("arrival at entry planned a segment: depart=%d arrive=%d", next.DepartMs, next.ArriveMs)
	}
	if _, stored := next.HomingGoal(); stored {
		t.Fatal("stored goal survived the end of the walk")
	}
	if next.BehaviorDeadlineMs != 20_000+ops.idleDelayMs() {
		t.Fatalf("idle deadline = %d, want %d", next.BehaviorDeadlineMs, 20_000+ops.idleDelayMs())
	}

	// Same XZ, another region: the arrival test must not fire.
	ops2, instance2, mover2, _ := pursuitControlsFixture(t)
	ops2.Rand = func() float64 { return 0 }
	ops2.PlanPath = testShortNavPath
	mover2.Pose = monster.Pose{RegionID: 0x62a9, X: 1008, Y: 20, Z: 1000}
	planned, _ := ops2.planReturnLeg(instance2, mover2, monster.MoverEventTargetLost, 20_000)
	if planned.Mode() != monster.MoverReturning {
		t.Fatalf("mode with goal in another region = %v, want returning (no cross-region arrival)", planned.Mode())
	}
}
