package simulation

import (
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestHomingClipsFromNestThenPlansFromLiveAndRetainsGoal(t *testing.T) {
	ops, instance, mover, _ := pursuitControlsFixture(t)
	mover.Pose.X += 110
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	calls := 0
	ops.PlanPath = func(from, goal monster.Pose) *monster.NavigationPath {
		calls++
		rest := goal
		if calls == 1 {
			if from.X != instance.Nest.X || goal.X <= from.X {
				t.Fatalf("home clipping used actor origin: %+v %+v", from, goal)
			}
			rest.X = 1008.3
		} else {
			if from != mover.Pose || goal.X != 1008 {
				t.Fatalf("actual move did not consume clipped home: %+v %+v", from, goal)
			}
			rest.X = 1050.25 // fractional blocking contact must survive wire rounding
		}
		return monster.NewNavigationPath(from, goal, rest, monster.NavResultClipped, func(float64, monster.Pose) (float64, bool) { return 20, true })
	}
	frames := ops.startReturnLeg(monsterTestDivision, &instance, monster.ResolveTactics(instance), mover, monster.MoverEventTargetLost, 10000)
	after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if calls != 2 || after.To.X != 1050.25 || after.MovementGoal().X != 1008 || len(frames) < 2 {
		t.Fatalf("lost navigation: %+v calls=%d", after, calls)
	}
	replay := monsterInFlightSnapshotFrames(instance, after, nil)
	if string(replay[len(replay)-1].Payload[:13]) != string(frames[len(frames)-1].Payload[:13]) {
		t.Fatal("scope entry changed wire goal to clipped rest")
	}
}

func TestMissingNavigationSettlesAndRejectedPlanPublishesNothing(t *testing.T) {
	for _, stale := range []bool{false, true} {
		ops, instance, mover, _ := pursuitControlsFixture(t)
		mover.From = mover.Pose
		mover.To = mover.Pose
		mover.To.X += 100
		mover.DepartMs = 9000
		mover.ArriveMs = 19000
		ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
		ops.PlanPath = func(monster.Pose, monster.Pose) *monster.NavigationPath {
			if stale {
				ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(2))
			}
			return nil
		}
		frames := ops.startReturnLeg(monsterTestDivision, &instance, monster.ResolveTactics(instance), mover, monster.MoverEventTargetLost, 10000)
		after, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if stale {
			if len(frames) != 0 || after.TargetGID() != PlayerObjectID(2) {
				t.Fatal("stale navigation published")
			}
			continue
		}
		if after.InFlight(10000) || after.Mode() != monster.MoverIdle || len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
			t.Fatalf("missing geometry retained motion: %+v %v", after, frames)
		}
	}
}
