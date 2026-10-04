package simulation

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestWanderProbeAndMovementHaveSeparateDistances(t *testing.T) {
	for _, result := range []uint32{0, monster.NavResultBlocked, monster.NavResultClipped} {
		ops, instance := monsterLegFixture(t, passiveTactics())
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		var goals []monster.Pose
		ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
			goals = append(goals, goal)
			return monster.NewNavigationPath(from, goal, goal, result, func(float64, monster.Pose) (float64, bool) { return 20, true })
		}
		ops.startWanderLeg(monsterTestDivision, instance, passiveTactics(), mover, 1000)
		if len(goals) != 2 {
			t.Fatalf("need a probe then a movement plan, got %d", len(goals))
		}
		if math.Abs(planarDistance(mover.Pose, goals[0])-30) > 0.01 || math.Abs(planarDistance(mover.Pose, goals[1])-61) > 1 {
			t.Fatalf("probe and request collapsed: %+v", goals)
		}
		dot := (goals[0].X-mover.Pose.X)*(goals[1].X-mover.Pose.X) + (goals[0].Z-mover.Pose.Z)*(goals[1].Z-mover.Pose.Z)
		if (dot < 0) != (result != 0) {
			t.Fatalf("collision reversal lost: result=%x dot=%v", result, dot)
		}
	}
}

func TestWanderExpiryPreservesMovementAndDefersAcquisition(t *testing.T) {
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	ops.startWanderLeg(monsterTestDivision, instance, passiveTactics(), mover, 1000)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	deadline := mover.BehaviorDeadlineMs
	if mover.ArriveMs <= deadline {
		t.Fatal("fixture must expire before arrival")
	}
	// Equality does not expire (558B90 JBE); no player yet.
	ops.advanceInstance(monsterTestDivision, instance, nil, deadline)
	current, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if current.Mode() != monster.MoverWandering {
		t.Fatal("expired at equality")
	}
	live := mover.LivePoseAt(deadline+1, nil)
	players := []playerPose{{Gid: PlayerObjectID(1), Pose: poseToSpawn(monster.Pose{RegionID: live.RegionID, X: live.X + 60, Y: live.Y, Z: live.Z}), BodyRadius: 4}}
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, players, deadline+1)
	current, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if len(frames) != 0 || current.Mode() != monster.MoverIdle || current.LastEvent() != monster.MoverEventWanderExpired || current.From != mover.From || current.To != mover.To || current.ArriveMs != mover.ArriveMs {
		t.Fatalf("expiry must change AI state only: %+v frames=%v", current, frames)
	}
	// The next IDLE tick can acquire while the old movement is still active.
	// The equality tick consumed Timer 1; wait for that timer, not arrival.
	ops.advanceInstance(monsterTestDivision, instance, players, deadline+1501)
	current, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if current.Mode() != monster.MoverChasing || current.TargetGID() != players[0].Gid {
		t.Fatalf("moving IDLE skipped acquisition: %+v", current)
	}
}

func TestIdleExpiryPrecedesSightAcquisition(t *testing.T) {
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mover.BehaviorDeadlineMs = 1000
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	players := []playerPose{{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1060, Y: 20, Z: 1000}, BodyRadius: 4}}
	ops.advanceInstance(monsterTestDivision, instance, players, 1001)
	current, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if current.Mode() != monster.MoverWandering || current.TargetGID() != 0 {
		t.Fatalf("expired IDLE scanned before its state decision: %+v", current)
	}
}

func TestWanderTimeoutDoesNotRestartIdleTimerOnMovementArrival(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	ops.startWanderLeg(monsterTestDivision, instance, passiveTactics(), mover, 1000)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	ops.advanceInstance(monsterTestDivision, instance, nil, mover.BehaviorDeadlineMs+1)
	idle, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	ops.advanceInstance(monsterTestDivision, instance, nil, mover.ArriveMs+1)
	settled, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if settled.BehaviorDeadlineMs != idle.BehaviorDeadlineMs || settled.Pose != mover.To || settled.ArriveMs != 0 {
		t.Fatalf("movement arrival restarted AI state: idle=%+v settled=%+v", idle, settled)
	}
}
