package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestAcquisitionGateSurvivesEmptyScanAndMoverCommit(t *testing.T) {
	const start = int64(100000)
	for _, flags := range []uint32{0, 4, 0x80, 0x200} {
		ops, instance := monsterLegFixture(t, aggressiveTactics())
		ops.Monsters.SetRandomSource(func() float64 { return 0 })
		state := ops.Monsters.division(monsterTestDivision)
		instance.Nest.NativeTacticsFlags = flags
		state.instances.set(instance.Gid, instance)
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		mover.BehaviorDeadlineMs = start + 10000
		ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
		// No viewer in sight: the production scan still consumes Timer 1.
		ops.advanceInstance(monsterTestDivision, instance, nil, start)
		interval := int64(1001)
		if flags&0x84 != 0 {
			interval = 201
		}
		gate := state.aiTimers[instance.Gid].GetTimer(1)
		if gate.ArmedImmediate || gate.LastCheckMs != uint32(start) || gate.IntervalMs != uint32(interval) {
			t.Fatalf("flags=%x unexpected production gate %+v", flags, gate)
		}
		// Whole-value movement commits cannot rewind the cadence owner.
		ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
		players := []playerPose{{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}, BodyRadius: 4}}
		ops.advanceInstance(monsterTestDivision, instance, players, start+interval-1)
		got, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if got.TargetGID() != 0 {
			t.Fatal("acquired before timer expiry")
		}
		ops.advanceInstance(monsterTestDivision, instance, players, start+interval)
		got, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if got.TargetGID() != players[0].Gid {
			t.Fatal("did not acquire at expiry")
		}
	}
}

func TestRetaliationDoesNotWaitForAcquisitionTimer(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	ops.Monsters.checkAITimer(monsterTestDivision, instance.Gid, 1, 100000)
	if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1)) {
		t.Fatal("retaliation refused")
	}
	players := []playerPose{{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}, BodyRadius: 4}}
	ops.advanceInstance(monsterTestDivision, instance, players, 100001)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if !mover.Retaliating() || !mover.InFlight(100001) {
		t.Fatal("closed sight gate stalled retaliation")
	}
}

func TestTargetStatusChangesInvalidateOwnedTargetPerObserver(t *testing.T) {
	for _, detects := range []bool{false, true} {
		ops, instance := monsterLegFixture(t, aggressiveTactics())
		if detects {
			instance.Nest.NativeTacticsFlags = 0x200
		}
		ops.Monsters.division(monsterTestDivision).instances.set(instance.Gid, instance)
		player := playerSessionAt(1, 1050, 1000)
		push := &fakePusher{}
		ops.RunMonsterLeg(100000, []SessionSnapshot{player}, push)
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.TargetGID() != PlayerObjectID(1) {
			t.Fatal("initial acquisition failed")
		}
		player.NativeBodyStatus = 6
		ops.RunMonsterLeg(100001, []SessionSnapshot{player}, push)
		mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if (mover.TargetGID() != 0) != detects {
			t.Fatalf("observer detection=%v target=%d", detects, mover.TargetGID())
		}
		player.NativeBodyStatus = 3
		ops.RunMonsterLeg(100002, []SessionSnapshot{player}, push)
		mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.TargetGID() != 0 {
			t.Fatal("detection incorrectly bypassed status 3")
		}
		if !ops.shownMonsters[player.SessionID][instance.Gid] {
			t.Fatal("target eligibility removed viewer scope")
		}
	}
}

func TestSummonFollowOwnsDistinctStateAcrossLeaderDeath(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(children) == 0 {
		t.Fatal("summon refused")
	}
	child := children[0]
	ops := &MonsterMoverOps{Monsters: s, Rand: func() float64 { return 0 }, TacticsFor: fixedTactics(passiveTactics())}
	leader, _ := s.Mover("summon", parent.Gid)
	leader.Pose.X += 900
	s.CommitMover("summon", parent.Gid, leader)
	mover, _ := s.Mover("summon", child.Gid)
	if _, ok := ops.followSummoner("summon", child, mover, *now); !ok {
		t.Fatal("follow not started")
	}
	mover, _ = s.Mover("summon", child.Gid)
	if mover.Mode() != monster.MoverFollowing || mover.TargetGID() != 0 {
		t.Fatalf("wrong follow ownership: %+v", mover)
	}
	if s.division("summon").aiTimers[child.Gid].GetTimer(0).IntervalMs != 100 {
		t.Fatal("FOLLOW did not replace Timer 0")
	}
	s.ApplyDamage("summon", parent.Gid, parent.CurrentHP)
	frames, _ := ops.advanceInstance("summon", child, nil, *now+1)
	mover, _ = s.Mover("summon", child.Gid)
	if len(frames) != 0 || mover.Mode() != monster.MoverFollowing || mover.InFlight(*now+1) {
		t.Fatal("dead leader changed FOLLOW state")
	}
}
