package simulation

import (
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func activityFixture(t *testing.T) (*MonsterMoverOps, monster.Instance, int64) {
	t.Helper()
	ops, actor := monsterLegFixture(t, passiveTactics())
	const now int64 = 10000
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	ops.startWanderLeg(monsterTestDivision, actor, passiveTactics(), m, now)
	m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	m.Activity = monster.ActivityCadence{Interval: 1000, LastCheck: uint32(now)}
	m.BehaviorDeadlineMs = now + 500 // activity gate must take precedence when both expire
	ops.Monsters.CommitMover(monsterTestDivision, actor.Gid, m)
	return ops, actor, now
}

func TestActivitySuspendsUnobservedActorAndPreservesMovement(t *testing.T) {
	ops, actor, now := activityFixture(t)
	original, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	ops.RunMonsterLeg(now+1001, nil, &fakePusher{})
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverPending || m.From != original.From || m.To != original.To || m.ArriveMs != original.ArriveMs {
		t.Fatalf("activity did not suspend AI independently of movement: %+v", m)
	}
	ops.RunMonsterLeg(original.ArriveMs, nil, &fakePusher{})
	m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverPending || m.ArriveMs != 0 || m.Pose != original.To {
		t.Fatalf("PENDING arrival incorrectly resumed AI: %+v", m)
	}
}

func TestActivityDeadPlayerResumesAtCadenceNotVisibilityAdmission(t *testing.T) {
	ops, actor, now := activityFixture(t)
	ops.RunMonsterLeg(now+1001, nil, &fakePusher{})
	asleep, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if asleep.Mode() != monster.MoverPending {
		t.Fatal("not pending")
	}
	p := playerSessionAt(1, 1000, 1000)
	p.CombatEligible = false
	ops.RunMonsterLeg(now+2001, []SessionSnapshot{p}, &fakePusher{})
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverPending {
		t.Fatal("resumed at equality")
	}
	ops.RunMonsterLeg(now+2002, []SessionSnapshot{p}, &fakePusher{})
	m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverWandering || m.DepartMs != now+2002 || m.BehaviorDeadlineMs <= now+2002 {
		t.Fatalf("resume must re-enter WANDER with new movement/deadline: %+v", m)
	}
}

func TestActivityForeignPartitionCannotKeepMonsterAwake(t *testing.T) {
	ops, actor, now := activityFixture(t)
	p := playerSessionAt(1, 1000, 1000)
	p.WorldInstance = 0x20001
	ops.RunMonsterLeg(now+1001, []SessionSnapshot{p}, &fakePusher{})
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverPending {
		t.Fatalf("foreign world kept actor active: %v", m.Mode())
	}
}

func TestActivityRetaliationInvalidatesSuspensionAndWakesScheduledActor(t *testing.T) {
	ops, actor, now := activityFixture(t)
	before, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	after := before
	after.Activity.Due(uint32(now + 1001))
	mustMoverTransition(&after, monster.MoverEventActivityLost, 0)
	if !ops.Monsters.ArmRetaliation(monsterTestDivision, actor.Gid, PlayerObjectID(1)) {
		t.Fatal("hit refused")
	}
	if ops.Monsters.commitActivity(monsterTestDivision, actor.Gid, before, after, now+1001) {
		t.Fatal("stale suspension replaced retaliation")
	}
	batches := ops.Monsters.behaviorBatches(now + 1)
	found := false
	for _, b := range batches {
		for _, a := range b.actors {
			found = found || a.gid == actor.Gid
		}
	}
	if !found {
		t.Fatal("retaliation did not wake queue entry")
	}
}

/*
================
TestCommitActivityCadenceOnlyStampsCadence

An idle re-check commit that moves only the activity clock stores
exactly the committed value (stamped in place, not rebuilt: the
interest bounds and the pending row's shared handle never carried
the cadence), and a commit that moves anything else still lands in
full (#2136). Read back through Mover, the read the gate trusts.
================
*/
func TestCommitActivityCadenceOnlyStampsCadence(t *testing.T) {
	ops, actor, now := activityFixture(t)
	before, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	after := before
	if !after.Activity.Due(uint32(now + 1001)) {
		t.Fatal("fixture cadence did not fire")
	}
	if !ops.Monsters.commitActivity(monsterTestDivision, actor.Gid, before, after, now+1001) {
		t.Fatal("cadence-only commit rejected")
	}
	got, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if got != after {
		t.Fatalf("stored mover differs from cadence-only commit:\n got %+v\nwant %+v", got, after)
	}
	moved := after
	moved.Pose.X += 25
	if !ops.Monsters.commitActivity(monsterTestDivision, actor.Gid, after, moved, now+1001) {
		t.Fatal("pose-carrying commit rejected")
	}
	got, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if got != moved {
		t.Fatal("pose change did not land in full")
	}
}

func TestActivityProductionTickerRetainsUnobservedDivision(t *testing.T) {
	ops, actor, now := activityFixture(t)
	source := &fakeSource{}
	ticker := newTestTicker(source, &fakePusher{})
	ticker.Monsters = ops
	ticker.RunTick(now + 1001)
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverPending {
		t.Fatal("last viewer removal dropped AI work before PENDING")
	}
	source.sessions = []SessionSnapshot{playerSessionAt(1, 1000, 1000)}
	ticker.RunTick(now + 2002)
	m, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverWandering {
		t.Fatal("returning PC did not resume retained population")
	}
}

func TestActivityOldGenerationCannotResumePopulation(t *testing.T) {
	ops, actor, now := activityFixture(t)
	p := playerSessionAt(1, 1000, 1000)
	p.Population.Generation++
	ops.RunMonsterLeg(now+1001, []SessionSnapshot{p}, &fakePusher{})
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverPending {
		t.Fatal("another lifetime of the same world kept actor awake")
	}
}

func TestSummonFactoryBindsControllerAndExemptsActivity(t *testing.T) {
	ops, parent, child, now := followFixture(t)
	m, _ := ops.Monsters.Mover("summon", child.Gid)
	if m.ControllerGID() != parent.Gid || m.ControlMode() != monster.ControlSummoned {
		t.Fatal("summon omitted CSNM 11 mode 2")
	}
	mustMoverTransition(&m, monster.MoverEventStartWander, 0)
	m.Activity = monster.ActivityCadence{Interval: 1000, LastCheck: uint32(now)}
	m.BehaviorDeadlineMs = now + 10000
	ops.Monsters.CommitMover("summon", child.Gid, m)
	ops.activity = ops.captureActivity(nil, now+1001)
	frames, _, handled := ops.runActivityGate("summon", child, m, now+1001)
	after, _ := ops.Monsters.Mover("summon", child.Gid)
	if handled || len(frames) != 0 || after != m {
		t.Fatal("controlled actor ran unobserved suspension/cadence")
	}
}

func TestPendingResumeRefusedWanderFallsBackThroughEntryOwner(t *testing.T) {
	ops, actor, now := activityFixture(t)
	ops.RunMonsterLeg(now+1001, nil, &fakePusher{})
	ops.Monsters.mu.Lock()
	state := ops.Monsters.populationForObject(monsterTestDivision, actor.Gid)
	row := state.instances.get(actor.Gid)
	row.Ref.RunSpeed = 0
	state.instances.set(actor.Gid, row)
	ops.Monsters.mu.Unlock()
	ops.RunMonsterLeg(now+2002, []SessionSnapshot{playerSessionAt(1, 1000, 1000)}, &fakePusher{})
	m, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if m.Mode() != monster.MoverIdle || m.LastEvent() != monster.MoverEventEntryRefused {
		t.Fatal("failed WANDER entry did not enter IDLE")
	}
}

func TestPendingInboxPrecedesResumeAndWakesSleepingActor(t *testing.T) {
	ops, actor, now := activityFixture(t)
	ops.RunMonsterLeg(now+1001, nil, &fakePusher{})
	before, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if err := ops.Monsters.DeliverMonsterHelp(monsterTestDivision, actor.Gid, helpPayload(1, 999999, 0, 0, 999998)); err != nil {
		t.Fatal(err)
	}
	// A rejected command still consumes this tick, before the due PENDING
	// callback could see the returning PC and resume WANDER.
	p := playerSessionAt(1, 1000, 1000)
	ops.RunMonsterLeg(now+2002, []SessionSnapshot{p}, &fakePusher{})
	after, _ := ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	row, _ := ops.Monsters.Get(monsterTestDivision, actor.Gid)
	if _, pending := row.Help.Pending(); pending {
		t.Fatal("PENDING stranded its inbox")
	}
	if after.Mode() != monster.MoverPending || after.Activity != before.Activity {
		t.Fatal("state callback ran before queued command")
	}
	ops.RunMonsterLeg(now+2003, []SessionSnapshot{p}, &fakePusher{})
	after, _ = ops.Monsters.Mover(monsterTestDivision, actor.Gid)
	if after.Mode() != monster.MoverWandering {
		t.Fatal("consumed command prevented subsequent resume")
	}
}

func TestPendingResumePreservesIndependentOpponentLedger(t *testing.T) {
	ops, actor, now := activityFixture(t)
	ops.RunMonsterLeg(now+1001, nil, &fakePusher{})
	ledger := [2]monster.Opponent{{GID: PlayerObjectID(1), LastHitMs: 500}, {GID: PlayerObjectID(2), LastHitMs: 501}}
	ops.Monsters.mu.Lock()
	state := ops.Monsters.populationForObject(monsterTestDivision, actor.Gid)
	row := state.instances.get(actor.Gid)
	row.Opponents = ledger
	state.instances.set(actor.Gid, row)
	ops.Monsters.mu.Unlock()
	ops.RunMonsterLeg(now+2002, []SessionSnapshot{playerSessionAt(3, 1000, 1000)}, &fakePusher{})
	row, _ = ops.Monsters.Get(monsterTestDivision, actor.Gid)
	if row.Opponents != ledger {
		t.Fatal("WANDER resume cleared opponent memory without a release event")
	}
}
