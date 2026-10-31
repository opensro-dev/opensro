/*
===========================================================================

monsteridle_rest_test.go - the idle-rest early-out: guard matrix and
due-time transitions

#2136 item 3: a resting idle monster skips the pose, activity-gate and
AI-timer reads until its next due time. The guard matrix pins each
idleRestQuiescent guard on its own; the transition tests pin the
moments the skip must not disturb - the idle decision lands on its
deadline tick, and the activity gate never consumes an idle
monster's cadence.
===========================================================================
*/

package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestIdleRestQuiescentGuardMatrix

At rest every guard passes; flipping any single fact the predicate
reads must refuse the skip, so the full step runs for that monster
exactly as before.
================
*/
func TestIdleRestQuiescentGuardMatrix(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	ops.activity = ops.captureActivity(nil, 0)
	const now = int64(10000)
	seeded, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	seeded.BehaviorDeadlineMs = now + 60000
	seeded.Activity = monster.ActivityCadence{Interval: 60000, LastCheck: uint32(now)}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, seeded)
	// The fixture mover still owes its idle entry; one step completes
	// it and the monster is at rest from there.
	ops.advanceInstance(monsterTestDivision, instance, nil, now)
	rest, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	rest.BehaviorDeadlineMs = now + 60000
	rest.Activity = monster.ActivityCadence{Interval: 60000, LastCheck: uint32(now)}
	if !idleRestQuiescent(ops, instance, rest, passiveTactics(), now) {
		t.Fatal("resting idle monster was not quiescent")
	}

	flips := []struct {
		name   string
		mutate func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics)
	}{
		{"wandering mode", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			mustMoverTransition(mover, monster.MoverEventStartWander, 0)
		}},
		{"activity cadence due", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			mover.Activity.LastCheck = wrappedLastCheck(now)
		}},
		{"help pending", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			actor.Help = actor.Help.Replace(monster.HelpEvent{})
		}},
		{"summoned control", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			if err := mover.BindController(monster.ControlSummoned, 777); err != nil {
				t.Fatalf("bind summoned controller: %v", err)
			}
		}},
		{"tempted", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			actor.Abnormal = &abnormal.Block{}
			actor.Abnormal.Slots[abnormal.Confusion].Active = true
		}},
		{"aggressive tactics", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			*tactics = aggressiveTactics()
		}},
		{"idle deadline passed", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			mover.BehaviorDeadlineMs = now - 1
		}},
		{"navigation goal held", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			mover.WaitNavigation(mover.Pose, now+1000)
		}},
		{"segment endpoints held", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			mover.From = mover.Pose
		}},
		{"segment matured", func(mover *monster.MoverState, actor *monster.Instance, tactics *monster.Tactics) {
			mover.DepartMs, mover.ArriveMs = now-2000, now-1000
		}},
	}
	for _, flip := range flips {
		mover, actor, tactics := rest, instance, passiveTactics()
		flip.mutate(&mover, &actor, &tactics)
		if idleRestQuiescent(ops, actor, mover, tactics, now) {
			t.Fatalf("%s: monster still quiescent", flip.name)
		}
	}

	// Without an activity snapshot the gate is inert, so the cadence
	// guard rests on the mover's own clock alone.
	ops.activity = nil
	due := rest
	due.Activity.LastCheck = wrappedLastCheck(now)
	if !idleRestQuiescent(ops, instance, due, passiveTactics(), now) {
		t.Fatal("nil activity snapshot must leave the resting monster quiescent")
	}
}

/*
================
wrappedLastCheck

A cadence stamp 60001ms before now, wrapped in the gate's uint32
clock: the stamp a monster carries when its cadence fell due while
the test clock was still young.
================
*/
func wrappedLastCheck(now int64) uint32 {
	stamp := uint32(now)
	return stamp - 60001
}

/*
================
TestIdleRestDecidesAtDeadline

A resting monster publishes nothing and keeps its deadline until the
deadline tick; on that tick the idle decision runs (a repeated idle
re-arms the deadline, a wander changes mode), never earlier.
================
*/
func TestIdleRestDecidesAtDeadline(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.BehaviorDeadlineMs = 5000
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	for _, now := range []int64{1000, 4999, 5000} {
		if frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, now); len(frames) != 0 {
			t.Fatalf("now %d: resting monster published %d frames", now, len(frames))
		}
		stored, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if stored.Mode() != monster.MoverIdle || stored.BehaviorDeadlineMs != 5000 {
			t.Fatalf("now %d: rest disturbed: mode %v deadline %d", now, stored.Mode(), stored.BehaviorDeadlineMs)
		}
	}
	ops.advanceInstance(monsterTestDivision, instance, nil, 5001)
	stored, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if stored.Mode() == monster.MoverIdle && stored.BehaviorDeadlineMs == 5000 {
		t.Fatal("idle decision did not run on the deadline tick")
	}
}

/*
================
TestIdleRestGateLeavesCadenceUntouched

The gate may only suspend a wandering monster (MaySuspendWander), so
an idle monster's cadence is never consumed - not by the gate on the
full step, and not by the skip. Stepping far past the interval must
leave the stamp and the mode exactly as they were.
================
*/
func TestIdleRestGateLeavesCadenceUntouched(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	const now = int64(10000)
	// The only viewer stands far from the nest; even so, no gate may
	// suspend an idle monster.
	ops.activity = ops.captureActivity([]SessionSnapshot{playerSessionAt(1, 5000, 5000)}, now)
	ops.activity.world = activityWorld{division: monsterTestDivision, world: 65537, generation: 1}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.BehaviorDeadlineMs = now + 60000
	mover.Activity = monster.ActivityCadence{Interval: 1000, LastCheck: uint32(now)}
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	for _, at := range []int64{now + 500, now + 5000} {
		if frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, at); len(frames) != 0 {
			t.Fatalf("now %d: resting monster published %d frames", at, len(frames))
		}
		stored, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if stored.Mode() != monster.MoverIdle || stored.Activity.LastCheck != uint32(now) {
			t.Fatalf("now %d: gate touched the idle monster: mode %v lastCheck %d", at, stored.Mode(), stored.Activity.LastCheck)
		}
	}
}

/*
================
BenchmarkIdleRestStep

One resting idle monster stepped once per iteration: the #2136
item 3 skip path. Without the skip each iteration pays the full
step - the activity gate, the help handler and the temptation and
attack prologues over their locked storage reads - to return nil.
================
*/
func BenchmarkIdleRestStep(b *testing.B) {
	ops, instance := monsterLegFixture(b, passiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.BehaviorDeadlineMs = math.MaxInt64
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	// Complete the idle entry so every iteration starts at rest.
	ops.advanceInstance(monsterTestDivision, instance, nil, 1000)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ops.advanceInstance(monsterTestDivision, instance, nil, 1000)
	}
}
