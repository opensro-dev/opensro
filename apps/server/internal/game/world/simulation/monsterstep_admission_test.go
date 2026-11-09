/*
===========================================================================

monsterstep_admission_test.go - the planning step prepares no navigation admission

A planning step (#2205) commits nothing itself: its navigation
funnels record the move and the serial replay re-prepares the
admission against the row at commit (commitSteppedNavigation).
Preparing one at plan time would be a population-lock read whose
value is never read. This test drives the four navigation funnels
on planning ops with no MonsterState at all: any storage read on
the way panics, so the recorded plan is the proof none happened.

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
admissionTestInstance

One ordinary walking monster at a fixed home point.
================
*/
func admissionTestInstance() monster.Instance {
	return monster.Instance{
		Gid:   4242,
		Ref:   monster.MonsterRef{TidWord: 0x0101, WalkSpeed: 3, RunSpeed: 6},
		Spawn: monster.SpawnPoint{RegionID: 25001, X: 100, Z: 100},
	}
}

/*
================
admissionTestIdleMover

The instance's spawn mover carried past its hold into idle.
================
*/
func admissionTestIdleMover(t *testing.T, instance monster.Instance) monster.MoverState {
	t.Helper()
	mover := monster.NewSpawnMover(instance, 0)
	if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
		t.Fatalf("idle mover transition: %v", err)
	}
	return mover
}

/*
================
TestPlanningStepPreparesNoNavigationAdmission

Each navigation funnel a step can end in - the wander start, the
segment seam, the idle entry, the return leg - must record its
navigation on planning ops without touching storage. The ops
carry a plan and no MonsterState, so a prepareNavigation (or any
other storage read) on the planning path panics here; the serial
step's prepare is untouched and covered by the serial golden.
================
*/
func TestPlanningStepPreparesNoNavigationAdmission(t *testing.T) {
	const division = "admission"
	nowMs := int64(100_000)

	funnels := []struct {
		name string
		run  func(ops *MonsterMoverOps, instance *monster.Instance, mover monster.MoverState)
	}{
		{"wander leg", func(ops *MonsterMoverOps, instance *monster.Instance, mover monster.MoverState) {
			ops.startWanderLeg(division, instance, monster.Tactics{WanderProbeDistance: 12}, mover, nowMs)
		}},
		{"segment", func(ops *MonsterMoverOps, instance *monster.Instance, mover monster.MoverState) {
			dest := mover.Pose
			dest.X += 10
			ops.commitSegment(division, instance, mover, dest, 3, wire.MoveStateWalk, nowMs)
		}},
		{"idle entry", func(ops *MonsterMoverOps, instance *monster.Instance, mover monster.MoverState) {
			ops.commitIdleEntry(division, instance, mover, nil, nowMs)
		}},
		{"return leg", func(ops *MonsterMoverOps, instance *monster.Instance, mover monster.MoverState) {
			mover.Pose.X += 40
			ops.startReturnLeg(division, instance, monster.Tactics{}, mover, monster.MoverEventHomingRequired, nowMs)
		}},
	}
	for _, funnel := range funnels {
		plan := &legPlan{}
		ops := &MonsterMoverOps{plan: plan}
		instance := admissionTestInstance()
		funnel.run(ops, &instance, admissionTestIdleMover(t, instance))
		if len(plan.ops) != 1 || plan.ops[0].kind != legPlanOpNavigation {
			t.Fatalf("%s: planning funnel recorded %v, want one navigation op", funnel.name, plan.ops)
		}
	}
}
