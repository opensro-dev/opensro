/*
===========================================================================

pursuit_direction_test.go - boundaries of the outdoor pursuit step decision

===========================================================================
*/
package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

/*
================
headingWordForDegrees

The heading word whose wire bearing is the given angle in degrees.
================
*/
func headingWordForDegrees(degrees float64) uint16 {
	return uint16(math.Round(degrees / 360 * 65535))
}

/*
================
pursuitTestLive

A resting actor at the fixture nest point with the given facing.
================
*/
func pursuitTestLive(heading uint16) monster.Pose {
	return monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000, Heading: heading}
}

/*
================
pursuitDelta

The ground-plane delta and distance from live to a pose, for assertions
that must survive region folding.
================
*/
func pursuitDelta(live, pose monster.Pose) (float64, float64, float64) {
	dx, dz := monsterPoseDelta(live, pose)
	return dx, dz, math.Hypot(dx, dz)
}

/*
================
TestPursuitGateDistanceBoundary

The gate arms only beyond 30 from the steering point: at exactly 30 the
step is straight, just beyond it the step runs along the facing.
================
*/
func TestPursuitGateDistanceBoundary(t *testing.T) {
	live := pursuitTestLive(headingWordForDegrees(4))
	for _, test := range []struct {
		distance float64
		straight bool
	}{
		{30, true},
		{31, false},
	} {
		goal := pursuitPointAlong(live, 1, 0, test.distance)
		got, step := decidePursuitStep(live, goal, false, monster.Pose{}, false, nil)
		if !step {
			t.Fatalf("distance %v: no step issued", test.distance)
		}
		if test.straight && got != goal {
			t.Errorf("distance %v: got %+v, want the straight goal %+v", test.distance, got, goal)
		}
		if !test.straight {
			dx, dz, _ := pursuitDelta(live, got)
			headX, headZ := pursuitHeadingVector(live.Heading)
			if math.Abs(dx-test.distance*headX) > 0.01 || math.Abs(dz-test.distance*headZ) > 0.01 {
				t.Errorf("distance %v: step delta (%v, %v), want the heading step (%v, %v)",
					test.distance, dx, dz, test.distance*headX, test.distance*headZ)
			}
		}
	}
}

/*
================
TestPursuitGateAngleBoundary

The gate arms only inside 5 degrees of facing. Heading words quantise
the bearing, so the boundary is pinned at the neighbouring words: the
word for 5 degrees reads just under it and gates; the next word reads
just over it and steps straight.
================
*/
func TestPursuitGateAngleBoundary(t *testing.T) {
	for _, test := range []struct {
		word     uint16
		straight bool
	}{
		{910, false}, // 4.9989 degrees
		{911, true},  // 5.0044 degrees
	} {
		live := pursuitTestLive(test.word)
		goal := pursuitPointAlong(live, 1, 0, 40)
		got, step := decidePursuitStep(live, goal, false, monster.Pose{}, false, nil)
		if !step {
			t.Fatalf("word %d: no step issued", test.word)
		}
		if test.straight && got != goal {
			t.Errorf("word %d: got %+v, want the straight goal %+v", test.word, got, goal)
		}
		if !test.straight && got == goal {
			t.Errorf("word %d: got the straight goal, want a heading step", test.word)
		}
	}
}

/*
================
TestPursuitGateDivergenceBoundary

At a 4-degree facing offset the heading-projected point drifts from the
straight point by 2*d*sin(2 degrees); the step snaps straight once that
divergence passes 100, and runs along the facing at or under it.
================
*/
func TestPursuitGateDivergenceBoundary(t *testing.T) {
	word := headingWordForDegrees(4)
	live := pursuitTestLive(word)
	bearing := float64(word) / 65535 * 360
	critical := 100 / (2 * math.Sin(bearing/2*math.Pi/180))
	for _, test := range []struct {
		scale    float64
		straight bool
	}{
		{0.999, false},
		{1.001, true},
	} {
		goal := pursuitPointAlong(live, 1, 0, critical*test.scale)
		got, step := decidePursuitStep(live, goal, false, monster.Pose{}, false, nil)
		if !step {
			t.Fatalf("scale %v: no step issued", test.scale)
		}
		if test.straight && got != goal {
			t.Errorf("scale %v: got %+v, want the straight goal %+v", test.scale, got, goal)
		}
		if !test.straight && got == goal {
			t.Errorf("scale %v: got the straight goal, want a heading step", test.scale)
		}
	}
}

/*
================
TestPursuitGateGuardVeto

A moving actor may not take a heading step once it has travelled more
than 30 from where its move sequence began; at exactly 30 it still
steps. The guard does not bind a resting actor or an actor with no
recorded sequence origin.
================
*/
func TestPursuitGateGuardVeto(t *testing.T) {
	live := pursuitTestLive(headingWordForDegrees(4))
	goal := pursuitPointAlong(live, 1, 0, 40)
	for _, test := range []struct {
		name     string
		moving   bool
		guard    monster.Pose
		hasGuard bool
		step     bool
	}{
		{"guard at 30 steps", true, pursuitPointAlong(live, -1, 0, 30), true, true},
		{"guard beyond 30 vetoes", true, pursuitPointAlong(live, -1, 0, 31), true, false},
		{"resting actor ignores the guard", false, pursuitPointAlong(live, -1, 0, 31), true, true},
		{"no recorded origin steps", true, monster.Pose{}, false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, step := decidePursuitStep(live, goal, test.moving, test.guard, test.hasGuard, nil)
			if step != test.step {
				t.Fatalf("got step %t, want %t", step, test.step)
			}
			if step && got == goal {
				t.Fatal("got the straight goal, want a heading step")
			}
		})
	}
}

/*
================
TestPursuitProbeAdoptsFreeBucket

A resting actor whose straight step is refused probes the 10-degree
buckets after the straight one at the clamped reach and steps 20 along
the first free bucket.
================
*/
func TestPursuitProbeAdoptsFreeBucket(t *testing.T) {
	live := pursuitTestLive(0)
	goal := pursuitPointAlong(live, 1, 0, 100)
	var queries []monster.Pose
	stepFree := func(candidate monster.Pose) bool {
		queries = append(queries, candidate)
		_, dz, _ := pursuitDelta(live, candidate)
		return dz > 0.5
	}
	got, step := decidePursuitStep(live, goal, false, monster.Pose{}, false, stepFree)
	if !step {
		t.Fatal("no step issued")
	}
	if len(queries) != 2 || queries[0] != goal {
		t.Fatalf("queries %v, want the straight goal then one bucket", queries)
	}
	if _, _, reach := pursuitDelta(live, queries[1]); math.Abs(reach-20) > 0.01 {
		t.Errorf("probe reach %v, want the clamped 20", reach)
	}
	dx, dz, distance := pursuitDelta(live, got)
	wantX, wantZ := 20*math.Cos(10*math.Pi/180), 20*math.Sin(10*math.Pi/180)
	if math.Abs(dx-wantX) > 0.01 || math.Abs(dz-wantZ) > 0.01 || math.Abs(distance-20) > 0.01 {
		t.Errorf("step delta (%v, %v) over %v, want (%v, %v) over 20", dx, dz, distance, wantX, wantZ)
	}
}

/*
================
TestPursuitProbeExhaustedStepsStraightTwenty

With every bucket refused the direction stays straight, but the probed
step still travels 20 instead of the full steering distance.
================
*/
func TestPursuitProbeExhaustedStepsStraightTwenty(t *testing.T) {
	live := pursuitTestLive(0)
	goal := pursuitPointAlong(live, 1, 0, 100)
	queries := 0
	stepFree := func(candidate monster.Pose) bool {
		queries++
		return false
	}
	got, step := decidePursuitStep(live, goal, false, monster.Pose{}, false, stepFree)
	if !step {
		t.Fatal("no step issued")
	}
	if queries != 36 {
		t.Errorf("%d queries, want the straight step plus 35 buckets", queries)
	}
	dx, dz, distance := pursuitDelta(live, got)
	if math.Abs(dx-20) > 0.01 || math.Abs(dz) > 0.01 || math.Abs(distance-20) > 0.01 {
		t.Errorf("step delta (%v, %v) over %v, want (20, 0) over 20", dx, dz, distance)
	}
}

/*
================
TestPursuitProbeReachClampAndFreeStraight

A short straight step probes its buckets at the minimum reach of 15
while the step itself travels 20; a free straight step is never probed.
================
*/
func TestPursuitProbeReachClampAndFreeStraight(t *testing.T) {
	live := pursuitTestLive(0)
	goal := pursuitPointAlong(live, 1, 0, 10)
	var queries []monster.Pose
	stepFree := func(candidate monster.Pose) bool {
		queries = append(queries, candidate)
		_, dz, _ := pursuitDelta(live, candidate)
		return dz > 0.5
	}
	if _, step := decidePursuitStep(live, goal, false, monster.Pose{}, false, stepFree); !step {
		t.Fatal("no step issued")
	}
	if len(queries) != 2 {
		t.Fatalf("queries %v, want the straight goal then one bucket", queries)
	}
	if _, _, reach := pursuitDelta(live, queries[1]); math.Abs(reach-15) > 0.01 {
		t.Errorf("probe reach %v, want the clamped minimum 15", reach)
	}

	queries = nil
	free := func(candidate monster.Pose) bool {
		queries = append(queries, candidate)
		return true
	}
	got, step := decidePursuitStep(live, goal, false, monster.Pose{}, false, free)
	if !step || got != goal || len(queries) != 1 {
		t.Errorf("free straight step: got %+v step %t after %d queries, want the goal after 1", got, step, len(queries))
	}
}

/*
================
TestPursuitProbeSkipsMovingActor

The probe is a start-of-movement decision: a moving actor is never
queried, and its gate runs on the straight direction.
================
*/
func TestPursuitProbeSkipsMovingActor(t *testing.T) {
	live := pursuitTestLive(headingWordForDegrees(4))
	goal := pursuitPointAlong(live, 1, 0, 40)
	queries := 0
	stepFree := func(candidate monster.Pose) bool {
		queries++
		return true
	}
	got, step := decidePursuitStep(live, goal, true, monster.Pose{}, false, stepFree)
	if !step || queries != 0 {
		t.Fatalf("step %t after %d queries, want a heading step with no probe", step, queries)
	}
	if got == goal {
		t.Fatal("got the straight goal, want a heading step")
	}
}

/*
================
expectedChaseGoal

The goal the leg author will steer to for this target before the step
decision: the approach point, replaced by the squad slot point when the
approach admission enrols the actor. Mirrors startChaseLeg's own
construction so a test can aim the actor relative to the real goal.
================
*/
func expectedChaseGoal(t *testing.T, ops *MonsterMoverOps, instance monster.Instance, mover monster.MoverState, target playerPose, live monster.Pose) monster.Pose {
	t.Helper()
	spacing := CombatSpacing{
		ActorBodyRadius:  BodyRadius(instance.BodyRadius()),
		TargetBodyRadius: target.BodyRadius,
		ActionReach:      ActionReach(mover.AttackReach),
	}
	goal, disposition := spacing.ApproachGoal(poseToSpawn(live), target.Pose)
	if disposition != CombatApproachMove {
		t.Fatalf("approach disposition %v, want an advancing goal", disposition)
	}
	before := ops.Monsters.prepareApproachNavigation(monsterTestDivision, instance, mover, target, 10000)
	if before.approach != nil {
		goal = squadApproachGoal(live, target, spacing, before.approach.slot)
	}
	return spawnToPose(goal)
}

/*
================
headingWordAiming

The heading word facing the goal from live, offset by the given degrees.
================
*/
func headingWordAiming(live, goal monster.Pose, offsetDegrees float64) uint16 {
	dx, dz := monsterPoseDelta(live, goal)
	bearing := math.Atan2(dz, dx) + offsetDegrees*math.Pi/180
	for bearing < 0 {
		bearing += 2 * math.Pi
	}
	return uint16(math.Round(bearing / (2 * math.Pi) * 65535))
}

/*
================
TestChaseLegStepsAlongHeading

Through the leg author: an ordinary outdoor chaser far from its goal and
facing it within 5 degrees is authored along its facing, not at the
goal.
================
*/
func TestChaseLegStepsAlongHeading(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	live := mover.Pose
	goal := expectedChaseGoal(t, ops, instance, mover, target, live)
	word := headingWordAiming(live, goal, 4)
	mover.Pose.Heading = word
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	var goals []monster.Pose
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		goals = append(goals, goal)
		return monster.NewNavigationPath(from, goal, goal, 0, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	frames := ops.startChaseLeg(monsterTestDivision, instance, aggressiveTactics(), mover, target, 10000)
	if len(frames) == 0 || len(goals) < 2 {
		t.Fatalf("no chase leg authored: %v frames, %v goals", len(frames), goals)
	}
	authored := goals[len(goals)-1]
	headX, headZ := pursuitHeadingVector(word)
	_, _, distance := pursuitDelta(live, goal)
	want := pursuitPointAlong(live, headX, headZ, distance)
	if math.Abs(authored.X-math.Round(want.X)) > 0.51 || math.Abs(authored.Z-math.Round(want.Z)) > 0.51 {
		t.Fatalf("authored goal %+v, want the heading step %+v (straight goal %+v)", authored, want, goal)
	}
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if after.To.RegionID != authored.RegionID || after.To.X != authored.X || after.To.Z != authored.Z {
		t.Fatalf("committed segment ends at %+v, want the authored %+v", after.To, authored)
	}
}

/*
================
TestChaseLegProbesFromRest

Through the leg author: a resting chaser whose straight step is refused
sidesteps 20 along the first free 10-degree bucket instead of pressing
into the block.
================
*/
func TestChaseLegProbesFromRest(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	live := mover.Pose
	var goals []monster.Pose
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		goals = append(goals, goal)
		if goal.Z <= live.Z+0.5 {
			return monster.NewNavigationPath(from, goal, from, monster.NavResultBlocked, func(float64, monster.Pose) (float64, bool) { return 20, true })
		}
		return monster.NewNavigationPath(from, goal, goal, 0, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10000)
	if len(frames) == 0 || len(goals) < 3 {
		t.Fatalf("no probed leg authored: %v frames, %v goals", len(frames), goals)
	}
	authored := goals[len(goals)-1]
	want := pursuitPointAlong(live, math.Cos(10*math.Pi/180), math.Sin(10*math.Pi/180), 20)
	if math.Abs(authored.X-math.Round(want.X)) > 0.01 || math.Abs(authored.Z-math.Round(want.Z)) > 0.01 {
		t.Fatalf("authored goal %+v, want the first free bucket step %+v", authored, want)
	}
}

/*
================
TestChaseLegGuardVetoKeepsSegment

Through the leg author: a moving chaser that has travelled more than 30
from where its move sequence began gets no new step; its current segment
runs on untouched.
================
*/
func TestChaseLegGuardVetoKeepsSegment(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	mover.From = monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	mover.To = monster.Pose{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}
	mover.DepartMs, mover.ArriveMs = 9000, 11000
	live := mover.LivePoseAt(10000, nil)
	mover.Pose = live
	goal := expectedChaseGoal(t, ops, instance, mover, target, live)
	mover.To.Heading = headingWordAiming(live, goal, 4)
	mover.NoteMoveIssued(monster.Pose{RegionID: monsterTestRegion, X: 990, Y: 20, Z: 1000}, false)
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	frames := ops.startChaseLeg(monsterTestDivision, instance, aggressiveTactics(), mover, target, 10000)
	if len(frames) != 0 {
		t.Fatalf("guard veto authored frames: %v", frames)
	}
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if after.From != mover.From || after.To != mover.To || after.DepartMs != mover.DepartMs || after.ArriveMs != mover.ArriveMs {
		t.Fatalf("vetoed authoring replaced the segment: %+v", after)
	}
}
