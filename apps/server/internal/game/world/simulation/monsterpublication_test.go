package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

func TestRejectedSegmentCannotPublishGoalOrChannel(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	stale, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mustMoverTransition(&stale, monster.MoverEventStartWander, 0)
	ops.Monsters.checkAITimer(monsterTestDivision, instance.Gid, 1, 100000)
	ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(2))
	frames := ops.commitSegment(monsterTestDivision, instance, stale,
		monster.Pose{RegionID: monsterTestRegion, X: 1100, Y: 20, Z: 1000}, 22, wire.MoveStateRun, 100001)
	if len(frames) != 0 {
		t.Fatalf("rejected plan published %d movement/channel frames", len(frames))
	}
	current, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if current.TargetGID() != PlayerObjectID(2) || !current.RetaliationPending() {
		t.Fatal("retaliation was overwritten")
	}
	if ops.Monsters.checkAITimer(monsterTestDivision, instance.Gid, 1, 100002) {
		t.Fatal("rejected movement rewound scan cadence")
	}
}

func TestRetaliationDuringAdmittedAttackPreservesCastAndCooldown(t *testing.T) {
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1))
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 1, Reach: 50, CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(2))
		// Stub cast proves publication ownership, not native serialization.
		return MonsterAttackResult{Accepted: true, TargetAlive: true, Frames: []Frame{{Opcode: 0xb245}}}
	}
	players := []playerPose{{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1010, Y: 20, Z: 1000}, BodyRadius: 4}}
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, players, 100000)
	if len(frames) == 0 || frames[len(frames)-1].Opcode != 0xb245 {
		t.Fatal("accepted attack was hidden by later retaliation")
	}
	current, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if current.TargetGID() != PlayerObjectID(2) || !current.RetaliationPending() || current.NextAttackMs != 100000+int64(monster.NextAttackInterval(0, 1000, 16384)) {
		t.Fatalf("later retaliation lost target or admitted cooldown: %+v", current)
	}
}

func TestRejectedArrivalCannotPublishCorrection(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
	mover.From = mover.Pose
	mover.To = mover.Pose
	mover.To.X += 30
	mover.DepartMs = 100000
	mover.ArriveMs = 100100
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	ops.TacticsFor = func(monster.Instance) monster.Tactics {
		ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(2))
		return passiveTactics()
	}
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, 100101)
	if len(frames) != 0 {
		t.Fatalf("rejected arrival published %d correction frames", len(frames))
	}
}

func TestRejectedAttackPlanCannotApplyDamageOrPublishCorrection(t *testing.T) {
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1))
	ops.AttackPlan = func(monster.Instance, uint32, AttackPick) (MonsterAttackPlan, bool) {
		ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(2))
		return MonsterAttackPlan{SkillID: 1, Reach: 50, CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	called := false
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		called = true
		return MonsterAttackResult{Accepted: true, TargetAlive: true}
	}
	players := []playerPose{{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1010, Y: 20, Z: 1000}, BodyRadius: 4}}
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, players, 100000)
	if called || len(frames) != 0 {
		t.Fatalf("rejected attack executed=%v frames=%d", called, len(frames))
	}
}

func TestRejectedFollowEntryCannotRearmFollowTimer(t *testing.T) {
	s, parent, wave, ranges, now := summonFixture(t)
	children, ok := s.CommitSummon("summon", parent, wave, *now, *now, ranges)
	if !ok || len(children) == 0 {
		t.Fatal("summon refused")
	}
	child := children[0]
	leader, _ := s.Mover("summon", parent.Gid)
	leader.Pose.X += 900
	s.CommitMover("summon", parent.Gid, leader)
	s.checkAITimer("summon", child.Gid, 0, *now)
	before := s.division("summon").aiTimers[child.Gid].GetTimer(0)
	mover, _ := s.Mover("summon", child.Gid)
	plan, ok := s.prepareFollow("summon", child, mover, *now+1)
	if !ok {
		t.Fatal("snapshot refused")
	}
	plan.timers.CheckTimer(7, uint32(*now+1))
	mustMoverTransition(&mover, monster.MoverEventFollowStarted, parent.Gid)
	s.ArmRetaliation("summon", child.Gid, PlayerObjectID(1))
	frames, accepted := s.commitFollow(plan, mover, nil)
	if accepted {
		t.Fatal("stale entry accepted")
	}
	after := s.division("summon").aiTimers[child.Gid].GetTimer(0)
	if len(frames) != 0 || before != after {
		t.Fatalf("rejected FOLLOW entered timer lifecycle: frames=%d before=%+v after=%+v", len(frames), before, after)
	}
}

func TestDeathDuringMovementPlanCannotPublishGoal(t *testing.T) {
	ops, instance := monsterLegFixture(t, passiveTactics())
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
	ops.TerrainHeight = func(uint16, float64, float64) (float64, bool) {
		ops.Monsters.ApplyDamage(monsterTestDivision, instance.Gid, instance.CurrentHP)
		return 20, true
	}
	destination := mover.Pose
	destination.X += 30
	if frames := ops.commitSegment(monsterTestDivision, instance, mover, destination, 15, wire.MoveStateWalk, 100000); len(frames) != 0 {
		t.Fatal("dead monster published a new movement goal")
	}
}
