package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"testing"
	"time"
)

func pursuitControlsFixture(t *testing.T) (*MonsterMoverOps, monster.Instance, monster.MoverState, playerPose) {
	t.Helper()
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.Monsters.clock = func() time.Time { return time.UnixMilli(10000) }
	ops.TacticsFor = nil
	instance.Nest.HasControls = true
	instance.Nest.PolicyPinned = true
	instance.Nest.Radius = 20
	instance.Nest.Controls = monster.TacticsControls{TraceBoundary: 1, TraceData: 500}
	target := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1100, Y: 20, Z: 1000}, BodyRadius: 4}
	s := ops.Monsters
	s.mu.Lock()
	s.division(monsterTestDivision).instances.set(instance.Gid, instance)
	s.mu.Unlock()
	s.ArmRetaliation(monsterTestDivision, instance.Gid, target.Gid)
	mover, _ := s.Mover(monsterTestDivision, instance.Gid)
	mover.LastBattleActivityMs = 9999
	s.CommitMover(monsterTestDivision, instance.Gid, mover)
	return ops, instance, mover, target
}

func TestProductionPursuitUsesTargetDistanceNotNestRadius(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	mover.Pose.X = 1300 // 300 from a radius-20 nest, only 100 from target
	target.Pose.X = 1400
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 10000)
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if after.Mode() != monster.MoverChasing || len(frames) == 0 {
		t.Fatalf("nest radius still interrupts pursuit: %v, %v", after.Mode(), frames)
	}
}

func TestHomingBoundaryUsesNestNotRandomizedSpawn(t *testing.T) {
	_, instance, _, _ := pursuitControlsFixture(t)
	instance.Nest.Radius = 200
	instance.Spawn.X += 150
	inside := monster.Pose{RegionID: instance.Nest.RegionID, X: instance.Nest.X - 100, Y: instance.Nest.Y, Z: instance.Nest.Z}
	if needsHoming(instance, inside) {
		t.Fatal("random spawn shifted the nest's home boundary")
	}
	inside.Y += 1000
	if needsHoming(instance, inside) {
		t.Fatal("homing containment used combat's 3D helper")
	}
	outside := inside
	outside.X = instance.Nest.X + 200
	if !needsHoming(instance, outside) {
		t.Fatal("nest boundary became relative to randomized spawn")
	}
}

func TestProductionPursuitTimerAndExactBoundary(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	live := mover.Pose
	if _, handled := ops.advancePursuitControls(monsterTestDivision, instance, mover, target, live, 10000); handled {
		t.Fatal("recent nearby target interrupted")
	}
	target.Pose.X = live.X + 500
	if _, handled := ops.advancePursuitControls(monsterTestDivision, instance, mover, target, live, 11499); handled {
		t.Fatal("trace ignored timer")
	}
	frames, handled := ops.advancePursuitControls(monsterTestDivision, instance, mover, target, live, 11500)
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if !handled || after.Mode() != monster.MoverIdle || after.TargetGID() != 0 || len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("boundary must abandon to idle: %v, %v", after.Mode(), frames)
	}
}

func TestPursuitSpeedDecisionPreservesTargetAndReplansWire(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	s := ops.Monsters
	mover.LastBattleActivityMs = 0
	mover.Channel = wire.MoveStateRun
	mover.From = mover.Pose
	mover.To = mover.Pose
	mover.To.X += 100
	mover.DepartMs = 9000
	mover.ArriveMs = 19000
	s.CommitMover(monsterTestDivision, instance.Gid, mover)
	s.mu.Lock()
	row := s.division(monsterTestDivision).instances.get(instance.Gid)
	row.Opponents[0].LastHitMs = 0
	s.division(monsterTestDivision).instances.set(instance.Gid, row)
	s.mu.Unlock()
	live := mover.LivePoseAt(10000, nil)
	target.Pose.X = live.X + 150
	frames, handled := ops.advancePursuitControls(monsterTestDivision, instance, mover, target, live, 10000)
	after, _ := s.Mover(monsterTestDivision, instance.Gid)
	if !handled || after.TargetGID() != target.Gid || after.Mode() != monster.MoverChasing || after.Channel != wire.MoveStateWalk || len(frames) < 2 {
		t.Fatalf("speed branch discarded target/packets: %+v %v", after, frames)
	}
	state, err := wire.DecodeObjectStateRefresh(frames[0].Payload)
	if err != nil || state.Value != wire.MoveStateWalk {
		t.Fatalf("wrong speed packet: %+v %v", state, err)
	}
	if after.ArriveMs-after.DepartMs <= 9000 {
		t.Fatal("walk channel did not change segment duration")
	}
}

func TestPursuitRejectsStalePacketsAndTimerAfterSameTargetHit(t *testing.T) {
	ops, instance, mover, target := pursuitControlsFixture(t)
	target.Pose.X = mover.Pose.X + 500
	// Geometry/planning executes outside the owner lock. The RNG callback is
	// an intentional interleaving point between decision and final admission.
	ops.Rand = func() float64 {
		s := ops.Monsters
		s.mu.Lock()
		defer s.mu.Unlock()
		row := s.division(monsterTestDivision).instances.get(instance.Gid)
		row.Opponents[0].LastHitMs = 10001
		s.division(monsterTestDivision).instances.set(instance.Gid, row)
		return 0.5
	}
	frames, _ := ops.advancePursuitControls(monsterTestDivision, instance, mover, target, mover.Pose, 10000)
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if len(frames) != 0 || after != mover {
		t.Fatal("stale abandonment published")
	}
	s := ops.Monsters
	s.mu.Lock()
	timer := s.division(monsterTestDivision).aiTimers[instance.Gid].GetTimer(6)
	s.mu.Unlock()
	if !timer.ArmedImmediate {
		t.Fatal("rejected plan consumed trace timer")
	}
}

func TestAuthoredHomingPublishesRunAndMatchesDuration(t *testing.T) {
	ops, instance, mover, _ := pursuitControlsFixture(t)
	mover.Pose.X += 110
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	frames := ops.startReturnLeg(monsterTestDivision, instance, monster.ResolveTactics(instance), mover, monster.MoverEventTargetLost, 10000)
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	// radius 20, CRT sample 16384: 20/3 + (16384 % 7) = 10.6667,
	// wire goal rounds to 1011. Native homing does not choose the center.
	if after.Channel != wire.MoveStateRun || after.To.X != 1011 || after.ArriveMs-after.DepartMs != 4500 || len(frames) < 2 {
		t.Fatalf("homing run diverged: %+v %v", after, frames)
	}
}

func TestHomingAcquisitionDelayAndExpiryBranches(t *testing.T) {
	for _, tc := range []struct {
		name    string
		elapsed uint32
		want    monster.MoverMode
	}{
		{"delay equality remains homing", 1000, monster.MoverReturning},
		{"after delay acquires while moving", 1001, monster.MoverAttacking},
		{"duration equality still evaluates", 50000, monster.MoverAttacking},
		{"duration expired enters idle and immediately retries home", 50001, monster.MoverReturning},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ops, instance, mover, target := pursuitControlsFixture(t)
			instance.Nest.Aggressive = true
			instance.Nest.SightRange = 100
			mover.Pose.X += 200
			mustMoverTransition(&mover, monster.MoverEventTargetLost, 0)
			mover.HomingStartedMs = 10000
			mover.HomingAcquireAfterMs = 1000
			mover.From = mover.Pose
			mover.To = mover.Pose
			mover.To.X -= 100
			mover.DepartMs = 10000
			mover.ArriveMs = 70000
			s := ops.Monsters
			s.mu.Lock()
			s.division(monsterTestDivision).instances.set(instance.Gid, instance)
			s.mu.Unlock()
			s.CommitMover(monsterTestDivision, instance.Gid, mover)
			now := int64(10000 + tc.elapsed)
			target.Pose = poseToSpawn(mover.LivePoseAt(now, nil))
			target.Pose.X += 5
			ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, now)
			after, _ := s.Mover(monsterTestDivision, instance.Gid)
			if after.Mode() != tc.want {
				t.Fatalf("got %v want %v", after.Mode(), tc.want)
			}
			if tc.elapsed > 50000 && (after.HomingStartedMs != uint32(now) || after.TargetGID() != 0) {
				t.Fatal("expired homing did not re-enter through idle before acquisition")
			}
		})
	}
}

func TestIdleHomeAdmissionRunsOnEntryNotEveryTick(t *testing.T) {
	ops, actor, mover, _ := pursuitControlsFixture(t)
	mustMoverTransition(&mover, monster.MoverEventTraceAbandoned, 0)
	mover.BehaviorDeadlineMs = 100000
	mover, _ = ops.planIdleEntry(actor, mover, 10000)
	if mover.Mode() != monster.MoverIdle || mover.IdleEntryPending() {
		t.Fatal("initial entry not consumed")
	}
	mover.From = mover.Pose
	mover.To = mover.Pose
	mover.To.X += 100
	mover.DepartMs = 10000
	mover.ArriveMs = 20000
	ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, mover)
	ops.advanceInstance(monsterTestDivision, actor, nil, 15000)
	after, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.Mode() != monster.MoverIdle || after.From != mover.From || after.To != mover.To {
		t.Fatal("idle tick reran home admission after inherited movement crossed boundary")
	}
	// Re-entering IDLE must perform the check at the live (outside) position.
	mustMoverTransition(&after, monster.MoverEventIdleRepeated, 0)
	after, frames := ops.planIdleEntry(actor, after, 15000)
	if after.Mode() != monster.MoverReturning || len(frames) == 0 {
		t.Fatal("new idle entry failed to run homing")
	}
}
