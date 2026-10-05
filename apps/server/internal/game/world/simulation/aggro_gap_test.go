package simulation

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// TestDeadPlayerExcludedFromAggroCandidateList verifies that deceased players
// (CombatEligible = false) are never acquired as targets by aggressive monsters,
// while physical packets (spawn deltas and movement frames) are genuinely delivered
// to the dead session via PushToSession.
func TestDeadPlayerExcludedFromAggroCandidateList(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}

	// Dead player sits within sight range (50 units from anchor 1000, 1000).
	deadPlayer := playerSessionAt(1, 1050, 1000)
	deadPlayer.CombatEligible = false

	// Run monster tick. The dead player must be tracked as a world viewer and
	// receive the delivered movement/spawn frames, but the monster must NEVER
	// acquire aggro on them.
	ops.RunMonsterLeg(t0, []SessionSnapshot{deadPlayer}, push)
	// After the unexpired sight scan, explicitly expire IDLE to produce
	// movement. Receiving traffic is independent of target eligibility.
	idle, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	idle.BehaviorDeadlineMs = t0
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, idle)
	ops.RunMonsterLeg(t0+1, []SessionSnapshot{deadPlayer}, push)

	// Dead player must be tracked in the viewer map.
	if !ops.shownMonsters[deadPlayer.SessionID][instance.Gid] {
		t.Fatal("dead player was excluded from world viewer scope visibility")
	}

	// Dead player must have actually received delivered packets via PushToSession.
	delivered := sessionFrames(push, deadPlayer.SessionID)
	if len(delivered) == 0 {
		t.Fatal("dead viewer did not receive any delivered packets via PushToSession")
	}
	hasMoveFrame := false
	for _, f := range delivered {
		if f.Opcode == wire.OpObjectStateRefresh || f.Opcode == OpMovementAck {
			hasMoveFrame = true
			break
		}
	}
	if !hasMoveFrame {
		t.Fatalf("dead viewer did not receive movement frames: %+v", delivered)
	}

	// Monster must NOT acquire aggro on the dead player.
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() == monster.MoverChasing {
		t.Fatalf("monster acquired aggro on a dead player (mode=%v, target=%d)", mover.Mode(), mover.TargetGID())
	}
	if mover.TargetGID() != 0 {
		t.Fatalf("dead player target retained: %d", mover.TargetGID())
	}
}

// TestTargetDeathReleasesRetainedTargetAndReturnsHome verifies that when an
// acquired combat target dies, the monster immediately loses its target, releases
// combat ownership, and initiates a return leg home.
func TestTargetDeathReleasesRetainedTargetAndReturnsHome(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}

	// 1. Alive player within sight acquires aggro and starts chase.
	player := playerSessionAt(1, 1050, 1000)
	player.CombatEligible = true

	ops.RunMonsterLeg(t0, []SessionSnapshot{player}, push)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverChasing || mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("initial aggro failed: mode=%v, target=%d", mover.Mode(), mover.TargetGID())
	}

	// 2. Player dies. Next tick must observe target loss and start return leg.
	player.CombatEligible = false
	push.toSession, push.toDivision = nil, nil

	ops.RunMonsterLeg(t0+1000, []SessionSnapshot{player}, push)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverReturning {
		t.Fatalf("dead target did not trigger return leg (mode=%v)", mover.Mode())
	}
	if mover.TargetGID() != 0 {
		t.Fatalf("target was not released on death: %d", mover.TargetGID())
	}
}

// TestWanderingMonsterAcquiresAggroInFlight verifies that an aggressive monster
// currently moving on a wander leg immediately acquires aggro when an eligible
// player enters sight range, interrupting the wander walk and chasing at run speed.
func TestWanderingMonsterAcquiresAggroInFlight(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}

	// 1. Force the monster into MoverWandering with an in-flight walk segment.
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	dest := monster.Pose{RegionID: monsterTestRegion, X: 1030, Y: 20, Z: 1000}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatalf("transition wander: %v", err)
	}
	mover.From = from
	mover.To = dest
	mover.DepartMs = t0
	mover.ArriveMs = t0 + 5000
	mover.Pose = from
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	// Precondition: segment is in flight.
	if !mover.InFlight(t0 + 1000) {
		t.Fatal("precondition failed: wander segment not in flight")
	}

	// 2. An eligible player enters sight range (at 1040, 1000).
	player := playerSessionAt(1, 1040, 1000)
	player.CombatEligible = true

	// 3. Tick mid-flight: monster must acquire aggro and switch to chasing.
	ops.RunMonsterLeg(t0+1000, []SessionSnapshot{player}, push)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverChasing {
		t.Fatalf("in-flight wandering monster failed to acquire aggro (mode=%v)", mover.Mode())
	}
	if mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("chase target = %d, want player GID %d", mover.TargetGID(), PlayerObjectID(1))
	}

	// Frames should include run channel push (0x3122) and chase movement goal (0xB738).
	frames := monsterMovementAckFrames(push)
	if len(frames) == 0 {
		t.Fatal("no chase movement frames emitted when interrupting wander leg")
	}
	allFrames := monsterFrames(push)
	hasRunState := false
	for _, f := range allFrames {
		if f.Opcode == wire.OpObjectStateRefresh {
			refresh, err := wire.DecodeObjectStateRefresh(f.Payload)
			if err == nil && refresh.Value == wire.MoveStateRun {
				hasRunState = true
				break
			}
		}
	}
	if !hasRunState {
		t.Fatal("wandering aggro did not publish run channel state")
	}
}

// TestWanderingMonsterAcquiresAggroInAttackRangeInterruptsWithImmediateAttack verifies
// requirement 4: when an in-flight wandering monster encounters a player already inside
// attack reach:
// 1. It cancels the in-flight wander segment (From, To, DepartMs, ArriveMs cleared).
// 2. It transitions directly to MoverAttacking without an unnecessary chase leg.
// 3. It emits a correction frame (0xB2F5) before the attack frame (0xB245).
func TestWanderingMonsterAcquiresAggroInAttackRangeInterruptsWithImmediateAttack(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	ops.AttackPlan = func(monster.Instance, uint32, float64) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(50), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		return MonsterAttackResult{Frames: []Frame{{Opcode: wire.OpSkillCastResult}}, Accepted: true, TargetAlive: true}
	}
	push := &fakePusher{}

	// 1. Put monster in MoverWandering with in-flight segment (from (1000, 1000) toward (1050, 1000)).
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	dest := monster.Pose{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatalf("transition wander: %v", err)
	}
	mover.From = from
	mover.To = dest
	mover.DepartMs = t0
	mover.ArriveMs = t0 + 5000
	mover.Pose = from
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)

	// Precondition: segment is in flight.
	if !mover.InFlight(t0 + 1000) {
		t.Fatal("precondition failed: wander segment not in flight")
	}

	// 2. On tick 0, player is far away (outside sight range) to seed scope.
	// Both ticks stay in interest block (3,3), so the fixture's stationary
	// monster is visible throughout and never enters scope mid-assertion.
	player := playerSessionAt(1, 1250, 1000)
	player.CombatEligible = true
	ops.RunMonsterLeg(t0, []SessionSnapshot{player}, push)
	push.toSession, push.toDivision = nil, nil

	// 3. After the maximum Timer 1 interval (1500ms), the player is inside
	// attack reach. The preceding empty scan legitimately consumed the gate.
	player.World.Spawn.X = 1015
	ops.RunMonsterLeg(t0+1500, []SessionSnapshot{player}, push)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)

	// Assertions:
	// a. Wander leg must be completely canceled
	if mover.From != (monster.Pose{}) || mover.To != (monster.Pose{}) || mover.DepartMs != 0 || mover.ArriveMs != 0 {
		t.Fatalf("wander segment was not cleared on immediate attack interruption: %+v", mover)
	}
	// b. Mover mode must be MoverAttacking
	if mover.Mode() != monster.MoverAttacking {
		t.Fatalf("monster did not immediately enter MoverAttacking (mode=%v)", mover.Mode())
	}
	if mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("attack target = %d, want player GID %d", mover.TargetGID(), PlayerObjectID(1))
	}
	// c. Packet ordering: correction frame (0xB2F5) MUST precede attack frame (0xB245)
	frames := monsterFrames(push)
	if len(frames) < 2 {
		t.Fatalf("expected at least 2 frames (correction + attack), got %d: %+v", len(frames), frames)
	}
	if frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("first frame must be correction frame (0xB2F5), got opcode 0x%04X", frames[0].Opcode)
	}
	if frames[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("second frame must be attack frame (0xB245), got opcode 0x%04X", frames[1].Opcode)
	}
}

// TestAggroLifecycleEndToEnd demonstrates requirement 5:
// 1. Acquisition without attacking first (target in sight, outside reach -> chases without attacking)
// 2. Wandering interruption (in-flight wander interrupted by sight aggro -> switches to chase)
// 3. Death-triggered disengagement (target dies mid-chase -> drops target and returns home)
// 4. Leash break return (target flees beyond ChaseLeash -> drops target and returns home)
func TestAggroLifecycleEndToEnd(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	tactics := aggressiveTactics()
	tactics.SightRange = 200
	tactics.ChaseLeash = 300
	ops, instance := monsterLegFixture(t, tactics)
	ops.AttackPlan = func(monster.Instance, uint32, float64) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(20), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
		return MonsterAttackResult{Frames: []Frame{{Opcode: wire.OpSkillCastResult}}, Accepted: true, TargetAlive: true}
	}
	push := &fakePusher{}

	// --- PHASE 1: Acquisition without attacking first ---
	// Player is 100 units away (within SightRange 200, outside Reach 20).
	player := playerSessionAt(1, 1100, 1000)
	player.CombatEligible = true

	ops.RunMonsterLeg(t0, []SessionSnapshot{player}, push)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverChasing {
		t.Fatalf("phase 1: expected MoverChasing, got %v", mover.Mode())
	}
	// Verify no attack frames emitted (acquisition without attacking)
	for _, f := range monsterFrames(push) {
		if f.Opcode == wire.OpSkillCastResult {
			t.Fatal("phase 1: monster attacked target that was out of reach!")
		}
	}

	// --- PHASE 2: Wandering interruption ---
	// Cleanly return and transition monster to wandering mode with in-flight segment
	mustMoverTransition(&mover, monster.MoverEventTargetLost, 0)
	mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
	mover.BehaviorDeadlineMs = t0 + 6500
	mustMoverTransition(&mover, monster.MoverEventStartWander, 0)
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	dest := monster.Pose{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}
	mover.From = from
	mover.To = dest
	mover.DepartMs = t0 + 1000
	mover.ArriveMs = t0 + 6000
	mover.Pose = from
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	push.toSession, push.toDivision = nil, nil

	// Tick during wander: player at 1080 is spotted in flight
	player.World.Spawn.X = 1080
	ops.RunMonsterLeg(t0+2000, []SessionSnapshot{player}, push)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverChasing {
		t.Fatalf("phase 2: in-flight wander was not interrupted by sight aggro (mode=%v)", mover.Mode())
	}
	if mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("phase 2: target not acquired: %d", mover.TargetGID())
	}

	// --- PHASE 3: Death-triggered disengagement ---
	// Player dies while monster is chasing
	player.CombatEligible = false
	push.toSession, push.toDivision = nil, nil

	ops.RunMonsterLeg(t0+2100, []SessionSnapshot{player}, push)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverReturning {
		t.Fatalf("phase 3: target death did not trigger MoverReturning (mode=%v)", mover.Mode())
	}
	if mover.TargetGID() != 0 {
		t.Fatalf("phase 3: target GID not cleared on death: %d", mover.TargetGID())
	}

	// --- PHASE 4: Leash break return ---
	// Arrive at home, player resurrects, aggro re-acquired, then player flees past leash
	mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
	mover.Pose = anchorPose(instance)
	mover.From, mover.To = monster.Pose{}, monster.Pose{}
	mover.DepartMs, mover.ArriveMs = 0, 0
	player.CombatEligible = true
	mustMoverTransition(&mover, monster.MoverEventAggroAcquired, PlayerObjectID(1))
	// Move monster to 1350 (exceeds anchor 1000 + ChaseLeash 300)
	mover.Pose.X = 1350
	ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
	push.toSession, push.toDivision = nil, nil

	player.World.Spawn.X = 1360
	ops.RunMonsterLeg(t0+3000, []SessionSnapshot{player}, push)
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverReturning {
		t.Fatalf("phase 4: leash breach did not trigger MoverReturning (mode=%v)", mover.Mode())
	}
	if mover.TargetGID() != 0 {
		t.Fatalf("phase 4: target GID not cleared on leash break: %d", mover.TargetGID())
	}
}
