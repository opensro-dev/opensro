package simulation

import (
	"encoding/binary"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

const monsterTestDivision = "DIV_A"

// monsterTestRegion is the field the monster fixtures stand in: 0x60A8, a
// battlefield just west of Jangan. It must not be a town: a monster whose
// region is not a battlefield vanishes (vanishInSafeZone, 4C1270).
const monsterTestRegion = 0x60a8

func monsterInstanceByRef(t *testing.T, registry *MonsterState, refObjID uint32) monster.Instance {
	t.Helper()
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	for _, instance := range registry.InstancesInRegions(monsterTestDivision, []uint16{monsterTestRegion}) {
		if instance.Ref.RefObjID == refObjID {
			return instance
		}
	}
	t.Fatalf("population instance for refObjID %d not materialized", refObjID)
	return monster.Instance{}
}

func monsterLegFixture(t *testing.T, tactics monster.Tactics) (*MonsterMoverOps, monster.Instance) {
	t.Helper()
	template := monster.Template{
		Refs: map[uint32]monster.MonsterRef{
			1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100, BodyRadius: 6, MaxHP: 54},
			2000: {RefObjID: 2000, TidWord: 0x00C6, Codename: "MOB_TEST_STATIONARY"},
		},
	}
	registry := NewMonsterState(monster.TemplateFromParts(template.Refs, []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}},
		{SpawnPoint: monster.SpawnPoint{RefObjID: 2000, RegionID: monsterTestRegion, X: 900, Y: 20, Z: 900}},
	}))
	registry.StartDivision(monsterTestDivision)
	registry.AdvancePopulation(time.Now().UnixMilli())
	instance := monsterInstanceByRef(t, registry, 1933)
	mover, _ := registry.Mover(monsterTestDivision, instance.Gid)
	if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
		t.Fatalf("seed idle mover: %v", err)
	}
	mover.Pose.Heading = 49151 // fixture faces -Z; raw wire zero faces +X
	mover.BehaviorDeadlineMs = 0
	if tactics.Aggressive {
		// Acquisition tests start in an unexpired IDLE state. An expired
		// native state runs its decision before scanning (55A8B0).
		mover.BehaviorDeadlineMs = math.MaxInt64
	}
	registry.CommitMover(monsterTestDivision, instance.Gid, mover)
	ops := &MonsterMoverOps{
		Monsters:   registry,
		TacticsFor: fixedTactics(tactics),
		Rand:       func() float64 { return 0.5 },
	}
	ops.AttackPlan = func(monster.Instance, uint32, float64) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(6), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	return ops, instance
}

func TestChaseEqualWalkAndRunSpeedsPreservesRunChannel(t *testing.T) {
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	instance.Ref.RunSpeed = instance.Ref.WalkSpeed
	target := playerPose{Gid: PlayerObjectID(1), Pose: Spawn{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000}, BodyRadius: 4}
	frames, _ := ops.advanceInstance(monsterTestDivision, instance, []playerPose{target}, 1000)
	if len(frames) < 2 || frames[0].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("chase must publish the run channel before its goal: %+v", frames)
	}
	refresh, err := wire.DecodeObjectStateRefresh(frames[0].Payload)
	if err != nil || refresh.Value != wire.MoveStateRun {
		t.Fatalf("equal speeds must not turn a run decision into walk: %+v (%v)", refresh, err)
	}
}

func fixedTactics(tactics monster.Tactics) monster.TacticsResolver {
	return func(monster.Instance) monster.Tactics {
		return tactics
	}
}

// passiveTactics is an explicit mover test contract. Production behavior is
// covered separately by monster.ResolveTactics evidence tests.
func passiveTactics() monster.Tactics {
	return monster.Tactics{
		SightRange:          115,
		ChaseLeash:          500,
		WanderProbeDistance: 30,
	}
}

// sequenceRand supplies repeatable idle decisions, headings, and delays.
func sequenceRand(samples ...float64) func() float64 {
	i := 0
	return func() float64 {
		if i >= len(samples) {
			return 0.5
		}
		v := samples[i]
		i++
		return v
	}
}

// aggressiveTactics is the production fixture with a TEST-LOCAL override
// marking the test mob aggressive - the shipped set is EMPTY (board
// seq490 BUG-6: the Mangnyang smoke marking was contrary to retail).
// These tests exercise the acquisition/chase/leash MECHANISM, never the
// shipped aggressive set.
func aggressiveTactics() monster.Tactics {
	tactics := passiveTactics()
	tactics.Aggressive = true
	return tactics
}

func playerSessionAt(characterID int64, x, z float64) SessionSnapshot {
	world := WorldState{Spawn: Spawn{RegionID: monsterTestRegion, X: x, Y: 20, Z: z}, SpawnSet: true}
	return SessionSnapshot{
		SessionID:      "viewer",
		DivisionID:     monsterTestDivision,
		CharacterID:    characterID,
		WorldInstance:  0x10001,
		Population:     instance.Lease{ID: instance.Pack(1, 1), Generation: 1},
		CombatEligible: true,
		World:          world,
		BodyRadius:     BodyRadius(4),
	}
}

// decodeGoalPayload unpacks the destination-mode 0xB738 body the monster
// leg emits (the BuildMovementAckPayload layout, RZ seq272 widths).
func decodeGoalPayload(t *testing.T, payload []byte) (gid uint32, region uint16, x, y, z uint16, sourcePresent bool) {
	t.Helper()
	if len(payload) < 14 {
		t.Fatalf("goal payload too short: % X", payload)
	}
	gid = binary.LittleEndian.Uint32(payload[0:4])
	if payload[4] != MovementAckDestinationMode {
		t.Fatalf("goal mode byte = %d, want destination mode", payload[4])
	}
	region = binary.LittleEndian.Uint16(payload[5:7])
	x = binary.LittleEndian.Uint16(payload[7:9])
	y = binary.LittleEndian.Uint16(payload[9:11])
	z = binary.LittleEndian.Uint16(payload[11:13])
	sourcePresent = payload[13] == 1
	return
}

func decodeGoalSource(t *testing.T, payload []byte) MovementSource {
	t.Helper()
	if len(payload) < 24 || payload[13] != 1 {
		t.Fatalf("goal has no complete source block: % X", payload)
	}
	return MovementSource{
		RegionID: binary.LittleEndian.Uint16(payload[14:16]),
		X:        float64(binary.LittleEndian.Uint16(payload[16:18])) / 10,
		Y:        float64(math.Float32frombits(binary.LittleEndian.Uint32(payload[18:22]))),
		Z:        float64(binary.LittleEndian.Uint16(payload[22:24])) / 10,
	}
}

// monsterFrames collects every frame the leg pushed (mover frames now
// deliver per-session against shown sets; nothing monster-related rides
// division broadcasts anymore - the ResolveGidObjectOrAssert guard).
func monsterFrames(push *fakePusher) []Frame {
	var out []Frame
	for _, pushed := range push.toSession {
		out = append(out, pushed.frames...)
	}
	for _, pushed := range push.toDivision {
		out = append(out, pushed.frames...)
	}
	return out
}

// scopeDeltaFrames filters a session's frames down to the scope-stream
// opcodes (spawn single / despawn single / despawn bracket begin) -
// movement traffic for already-shown monsters is legitimate while
// stationary and must not trip the churn canary.
func scopeDeltaFrames(push *fakePusher, sessionID string) []Frame {
	var out []Frame
	for _, frame := range sessionFrames(push, sessionID) {
		switch frame.Opcode {
		case wire.OpSingleObjectSpawn, wire.OpObjectDespawn, opObjectListStart:
			out = append(out, frame)
		}
	}
	return out
}

// Shared nest metadata must not erase individual actors' live positions.
func TestMonsterWanderUsesLivePositionNotSharedNestOrSpawn(t *testing.T) {
	for _, hasControls := range []bool{false, true} {
		for _, x := range []float64{750, 1250} {
			ops, instance := monsterLegFixture(t, passiveTactics())
			instance.Nest.HasControls = hasControls
			instance.Nest.Radius = 400
			// Both actors share nest/spawn (1000,1000), but their live
			// positions differ. No wander may pull them back to that center.
			mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
			mover.Pose.X = x
			ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
			for leg := 0; leg < 3; leg++ {
				from := mover.Pose
				frames := ops.startWanderLeg(monsterTestDivision, instance, passiveTactics(), mover, int64(leg)*10000)
				if len(frames) == 0 {
					t.Fatalf("controls=%v x=%v leg=%d: wander emitted no goal", hasControls, x, leg)
				}
				mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
				// Constant sample .5 produces CRT word 16384: 85-(16384%%40)=61.
				if distance := planarDistance(from, mover.To); math.Abs(distance-61) > 1 {
					t.Fatalf("controls=%v x=%v leg=%d: displacement=%v, want 61 from live pose %+v; goal=%+v", hasControls, x, leg, distance, from, mover.To)
				}
				// Settle exactly as the movement lifecycle does, then take
				// another step with the same random heading. It must advance.
				mover.Pose = mover.To
				mover.From, mover.To = monster.Pose{}, monster.Pose{}
				mover.DepartMs, mover.ArriveMs = 0, 0
				mustMoverTransition(&mover, monster.MoverEventSegmentArrived, 0)
				ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
			}
		}
	}
}

// A passive monster wanders: goal (0xB738 with a source because the first
// heading turns >45 degrees) -> quiet integration -> settle (0xB2F5) ->
// quiet until the cadence matures -> next goal. The second chord turns
// sharply from the first, so retail's autonomous-entity gate sources it too.
func TestMonsterWanderCycle(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, passiveTactics())
	step := float64(46)
	// Both legs turn 67.5 degrees and request 85-39 units. Their 5999ms
	// WANDER deadline follows arrival; timeout-first is tested separately.
	ops.Rand = sequenceRand(0.5, 0, 39.0/32768, 90.0/32768, 1.0/32768, 999.0/32768,
		.5, .5, .5, 0, 39.0/32768, 90.0/32768, 1.0/32768, 999.0/32768)
	// 283u away: inside the anchor's 320-unit interest block, beyond sight.
	sessions := []SessionSnapshot{playerSessionAt(1, 1200, 1200)}
	push := &fakePusher{}

	// Tick 1: wander goal.
	recordTestMonsterBootstrap(ops, sessions, t0)
	ops.RunMonsterLeg(t0, sessions, push)
	frames := monsterFrames(push)
	if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
		t.Fatalf("tick1 frames = %+v, want one 0xB738 goal", frames)
	}
	gid, region, x, _, z, sourcePresent := decodeGoalPayload(t, frames[0].Payload)
	if gid != instance.Gid || region != monsterTestRegion {
		t.Fatalf("goal gid/region = %d/%d, want %d/monsterTestRegion", gid, region, instance.Gid)
	}
	if !sourcePresent {
		t.Fatal("first goal turns >45 degrees from spawn facing and must carry a source block")
	}
	dx, dz := float64(x)-1000, float64(z)-1000
	if dist := math.Sqrt(dx*dx + dz*dz); math.Abs(dist-step) > 1 {
		t.Fatalf("wander destination %.1f units from anchor, want one %.1fu step", dist, step)
	}

	// Tick 2 (in flight; radius/2 units at walk 8): the server emits
	// NOTHING - the client path-follows the goal with its own integrator
	// (the BUG-7 fix: per-tick glides were the second position source
	// that caused the rubber-band).
	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(t0+250, sessions, push)
	if frames = monsterFrames(push); len(frames) != 0 {
		t.Fatalf("tick2 frames = %+v, want NONE while the goal matures (no glide re-seeds)", frames)
	}

	// Tick 3 (matured; one step at walk 8 has landed): exactly one 0xB2F5
	// settle at the goal.
	settleAt := t0 + int64(step/8*1000) + 100
	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(settleAt, sessions, push)
	frames = monsterFrames(push)
	if len(frames) != 1 || frames[0].Opcode != wire.OpObjectSourceCorrection {
		t.Fatalf("tick3 frames = %+v, want one 0xB2F5 settle", frames)
	}
	settledMover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	idleUntil := settledMover.BehaviorDeadlineMs

	// Tick 4 (idle, cadence not matured): quiet.
	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(idleUntil-250, sessions, push)
	if frames = monsterFrames(push); len(frames) != 0 {
		t.Fatalf("tick4 frames = %+v, want quiet until the wander cadence matures", frames)
	}

	// Tick 5 (cadence matured): the next goal makes a hard turn from the
	// previous heading and therefore carries a fresh live source.
	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(idleUntil+100, sessions, push)
	frames = monsterFrames(push)
	if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
		t.Fatalf("tick5 frames = %+v, want the next 0xB738 goal", frames)
	}
	if _, _, _, _, _, sourceAgain := decodeGoalPayload(t, frames[0].Payload); !sourceAgain {
		t.Fatal("second sharp-turn goal must carry the retail autonomous-entity source block")
	}
}

// An aggressive monster acquires a player inside SightRange immediately
// (retail shape: the per-monster flag is the only gate) and the chase
// goal aims at the player's combat ring; a non-aggressive monster with
// the same player wanders instead.
func TestMonsterAggroAcquisition(t *testing.T) {
	const t0 = int64(1_784_000_000_000)

	// Aggressive: chase goal toward the player at run speed. Player
	// geometry derives from the PRODUCTION SightRange (half of it away).
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	sight := ops.resolveTactics(instance).SightRange
	playerX := 1000 + math.Floor(sight/2)
	sessions := []SessionSnapshot{playerSessionAt(1, playerX, 1000)}
	push := &fakePusher{}
	recordTestMonsterBootstrap(ops, sessions, t0)
	ops.RunMonsterLeg(t0, sessions, push)
	frames := monsterFrames(push)
	// The chase bundle: a 0x3122 MOVE push flipping the client to the
	// RUN channel (the spawn row ships walk), then the 0xB738 goal.
	if len(frames) != 2 || frames[0].Opcode != wire.OpObjectStateRefresh || frames[1].Opcode != OpMovementAck {
		t.Fatalf("aggro frames = %+v, want [0x3122 run-channel, 0xB738 goal]", frames)
	}
	refresh, err := wire.DecodeObjectStateRefresh(frames[0].Payload)
	if err != nil || refresh.StateType != wire.StateChannelMove || refresh.Value != wire.MoveStateRun {
		t.Fatalf("channel push = %+v (%v), want MOVE channel run", refresh, err)
	}
	_, _, x, _, z, _ := decodeGoalPayload(t, frames[1].Payload)
	if float64(x) != playerX-14 || z != 1000 {
		t.Fatalf("chase goal = (%d, %d), want the 14-unit approach ring before (%v, 1000)", x, z, playerX)
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverChasing || mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("mover = %+v, want chasing player gid %d", mover, PlayerObjectID(1))
	}
	// The chase segment must be faster than the same distance at walk
	// speed (the POLICY run channel through the same emitter).
	if travel := mover.ArriveMs - mover.DepartMs; travel > int64((playerX-1000)/8*1000) {
		t.Fatalf("chase travel %dms is walk-paced; chase must use the run channel", travel)
	}

	// Passive control: the same geometry wanders (dest = the Rand-driven
	// wander point, NOT the player; walk channel unchanged so no 0x3122).
	passiveOps, _ := monsterLegFixture(t, passiveTactics())
	passivePush := &fakePusher{}
	recordTestMonsterBootstrap(passiveOps, []SessionSnapshot{playerSessionAt(1, playerX, 1000)}, t0)
	passiveOps.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(1, playerX, 1000)}, passivePush)
	passiveFrames := monsterFrames(passivePush)
	if len(passiveFrames) != 1 || passiveFrames[0].Opcode != OpMovementAck {
		t.Fatalf("passive frames = %+v, want one wander goal (no channel push on walk)", passiveFrames)
	}
	if _, _, px, _, pz, _ := decodeGoalPayload(t, passiveFrames[0].Payload); float64(px) == playerX && pz == 1000 {
		t.Fatal("a non-aggressive monster aimed at the player")
	}
}

func TestMonsterAggroTransitionsToRepeatedBasicAttackAndBackToChase(t *testing.T) {
	const (
		t0       = int64(1_784_000_000_000)
		rangeU   = 6.0
		cooldown = int64(1000)
		skillID  = uint32(0x1234)
	)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	attackCalls := 0
	ops.AttackPlan = func(got monster.Instance, requested uint32, _ float64) (MonsterAttackPlan, bool) {
		if got.Gid != instance.Gid {
			t.Fatalf("attack plan instance gid = %d, want %d", got.Gid, instance.Gid)
		}
		if requested != 0 && requested != skillID {
			t.Fatalf("requested skill = %#x, want zero or retained %#x", requested, skillID)
		}
		return MonsterAttackPlan{SkillID: skillID, Reach: ActionReach(rangeU), CooldownMs: cooldown, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(divisionID string, got monster.Instance, targetGid, gotSkillID uint32, _ int64) MonsterAttackResult {
		attackCalls++
		if divisionID != monsterTestDivision || got.Gid != instance.Gid ||
			targetGid != PlayerObjectID(1) || gotSkillID != skillID {
			t.Fatalf("attack args = %q/%d/%d/%#x", divisionID, got.Gid, targetGid, gotSkillID)
		}
		return MonsterAttackResult{
			Frames:      []Frame{{Opcode: wire.OpSkillCastResult, Payload: []byte{byte(attackCalls)}}},
			Accepted:    true,
			TargetAlive: true,
		}
	}
	session := playerSessionAt(1, 1000+rangeU-1, 1000)
	push := &fakePusher{}

	recordTestMonsterBootstrap(ops, []SessionSnapshot{session}, t0)

	ops.RunMonsterLeg(t0, []SessionSnapshot{session}, push)
	frames := monsterFrames(push)
	if attackCalls != 1 || len(frames) != 2 ||
		frames[0].Opcode != wire.OpObjectSourceCorrection ||
		frames[1].Opcode != wire.OpSkillCastResult {
		t.Fatalf("initial in-range aggro = calls %d frames %+v, want facing correction then B245 attack", attackCalls, frames)
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverAttacking || mover.TargetGID() != PlayerObjectID(1) ||
		mover.NextAttackMs != t0+int64(monster.NextAttackInterval(0, uint32(cooldown), 16384)) {
		t.Fatalf("post-attack mover = %+v, want attacking through cooldown", mover)
	}

	firstDeadline := mover.NextAttackMs
	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(firstDeadline-1, []SessionSnapshot{session}, push)
	if attackCalls != 1 || len(monsterFrames(push)) != 0 {
		t.Fatalf("cooldown tick attacked early: calls=%d frames=%+v", attackCalls, monsterFrames(push))
	}

	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(firstDeadline, []SessionSnapshot{session}, push)
	if attackCalls != 2 || len(monsterFrames(push)) != 1 {
		t.Fatalf("due attack = calls %d frames %+v, want the second bracket", attackCalls, monsterFrames(push))
	}

	// Leaving range does not discard aggro or interrupt the current action.
	// At its interval deadline, the attack state yields
	// to the one shared chase mover, which can re-enter attack when range is
	// regained without a second acquisition path.
	session = playerSessionAt(1, 1050, 1000)
	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(firstDeadline+250, []SessionSnapshot{session}, push)
	if frames = monsterFrames(push); len(frames) != 0 {
		t.Fatalf("target movement interrupted an admitted action: %+v", frames)
	}
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverAttacking {
		t.Fatalf("admitted action lost interval ownership: %s", mover.Mode())
	}
	ops.RunMonsterLeg(mover.NextAttackMs, []SessionSnapshot{session}, push)
	frames = monsterFrames(push)
	if len(frames) != 2 || frames[0].Opcode != wire.OpObjectStateRefresh || frames[1].Opcode != OpMovementAck {
		t.Fatalf("out-of-range attack transition = %+v, want run-channel + chase goal", frames)
	}
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverChasing || mover.TargetGID() != PlayerObjectID(1) {
		t.Fatalf("out-of-range mover = %+v, want retained-target chase", mover)
	}
}

func TestMonsterFatalConsequencesStaySameTurnAndTargetOnly(t *testing.T) {
	const (
		t0      = int64(1_784_000_000_000)
		rangeU  = 12.0
		skillID = uint32(0x1234)
	)
	ops, _ := monsterLegFixture(t, aggressiveTactics())
	ops.AttackPlan = func(monster.Instance, uint32, float64) (MonsterAttackPlan, bool) {
		return MonsterAttackPlan{SkillID: skillID, Reach: ActionReach(rangeU), CooldownMs: 1000, ActionLifecycleMs: 600}, true
	}
	ops.BasicAttack = func(_ string, _ monster.Instance, targetGid, _ uint32, _ int64) MonsterAttackResult {
		if targetGid != PlayerObjectID(1) {
			t.Fatalf("fatal target = %d, want actor gid %d", targetGid, PlayerObjectID(1))
		}
		return MonsterAttackResult{
			Frames:      []Frame{{Opcode: wire.OpSkillCastResult}},
			Private:     []MonsterPrivateFrames{{CharacterID: 1, Frames: []Frame{{Opcode: wire.OpExpUpdate}}}},
			Accepted:    true,
			TargetAlive: false,
		}
	}
	actor := playerSessionAt(1, 1005, 1000)
	actor.SessionID = "actor"
	peer := playerSessionAt(2, 1010, 1000)
	peer.SessionID = "peer"
	push := &fakePusher{}
	ops.RunMonsterLeg(t0, []SessionSnapshot{actor, peer}, push)

	actorFrames := sessionFrames(push, "actor")
	peerFrames := sessionFrames(push, "peer")
	actorB245, actorExp, b245Index, expIndex := 0, 0, -1, -1
	for index, frame := range actorFrames {
		switch frame.Opcode {
		case wire.OpSkillCastResult:
			actorB245++
			b245Index = index
		case wire.OpExpUpdate:
			actorExp++
			expIndex = index
		}
	}
	peerExp := 0
	for _, frame := range peerFrames {
		if frame.Opcode == wire.OpExpUpdate {
			peerExp++
		}
	}
	if actorB245 != 1 || actorExp != 1 || b245Index >= expIndex {
		t.Fatalf("actor fatal order = %+v, want public B245 before one private 30D2", actorFrames)
	}
	if peerExp != 0 {
		t.Fatalf("private death progression leaked to peer: %+v", peerFrames)
	}
	if len(push.toSession) < 2 || push.toSession[len(push.toSession)-1].sessionID != "actor" ||
		len(push.toSession[len(push.toSession)-1].frames) != 1 ||
		push.toSession[len(push.toSession)-1].frames[0].Opcode != wire.OpExpUpdate {
		t.Fatalf("targeted tail was not the final same-turn push: %+v", push.toSession)
	}
}

func TestPassiveMonsterRetaliatesAfterPlayerDamage(t *testing.T) {
	const (
		t0       = int64(1_784_000_000_000)
		rangeU   = 6.0
		cooldown = int64(1000)
		skillID  = uint32(0x1234)
	)

	configureAttack := func(t *testing.T, ops *MonsterMoverOps, instance monster.Instance) *int {
		t.Helper()
		attackCalls := new(int)
		ops.AttackPlan = func(got monster.Instance, requested uint32, _ float64) (MonsterAttackPlan, bool) {
			if got.Gid != instance.Gid || (requested != 0 && requested != skillID) {
				t.Fatalf("attack plan args = gid %d skill %#x", got.Gid, requested)
			}
			return MonsterAttackPlan{SkillID: skillID, Reach: ActionReach(rangeU), CooldownMs: cooldown, ActionLifecycleMs: 600}, true
		}
		ops.BasicAttack = func(divisionID string, got monster.Instance, targetGid, gotSkillID uint32, _ int64) MonsterAttackResult {
			*attackCalls++
			if divisionID != monsterTestDivision || got.Gid != instance.Gid ||
				targetGid != PlayerObjectID(1) || gotSkillID != skillID {
				t.Fatalf("attack args = %q/%d/%d/%#x", divisionID, got.Gid, targetGid, gotSkillID)
			}
			return MonsterAttackResult{
				Frames:      []Frame{{Opcode: wire.OpSkillCastResult}},
				Accepted:    true,
				TargetAlive: true,
			}
		}
		return attackCalls
	}

	t.Run("in range attacks without aggressive sight acquisition", func(t *testing.T) {
		ops, instance := monsterLegFixture(t, passiveTactics())
		attackCalls := configureAttack(t, ops, instance)
		if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1)) {
			t.Fatal("live passive monster rejected retaliation target")
		}

		push := &fakePusher{}
		recordTestMonsterBootstrap(ops, []SessionSnapshot{playerSessionAt(1, 1000+rangeU-1, 1000)}, t0)
		recordTestMonsterBootstrap(ops, []SessionSnapshot{playerSessionAt(1, 1000+rangeU-1, 1000)}, t0)
		ops.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(1, 1000+rangeU-1, 1000)}, push)
		frames := monsterFrames(push)
		if *attackCalls != 1 || len(frames) != 2 ||
			frames[0].Opcode != wire.OpObjectSourceCorrection ||
			frames[1].Opcode != wire.OpSkillCastResult {
			t.Fatalf("retaliation tick = calls %d frames %+v, want facing correction then B245 attack", *attackCalls, frames)
		}
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.Mode() != monster.MoverAttacking || mover.TargetGID() != PlayerObjectID(1) || mover.RetaliationPending() {
			t.Fatalf("retaliating mover = %+v, want active attack target with consumed edge", mover)
		}
	})

	t.Run("out of range chases the attacker instead of wandering", func(t *testing.T) {
		ops, instance := monsterLegFixture(t, passiveTactics())
		configureAttack(t, ops, instance)
		if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1)) {
			t.Fatal("live passive monster rejected retaliation target")
		}

		const playerX = 1050.0
		push := &fakePusher{}
		recordTestMonsterBootstrap(ops, []SessionSnapshot{playerSessionAt(1, playerX, 1000)}, t0)
		ops.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(1, playerX, 1000)}, push)
		frames := monsterFrames(push)
		if len(frames) != 2 || frames[0].Opcode != wire.OpObjectStateRefresh || frames[1].Opcode != OpMovementAck {
			t.Fatalf("retaliation chase = %+v, want run-channel + player goal", frames)
		}
		_, _, x, _, z, _ := decodeGoalPayload(t, frames[1].Payload)
		if float64(x) != playerX-14 || z != 1000 {
			t.Fatalf("retaliation goal = (%d,%d), want 14-unit stand-off from attacker (%v,1000)", x, z, playerX)
		}
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.Mode() != monster.MoverChasing || mover.TargetGID() != PlayerObjectID(1) || mover.RetaliationPending() {
			t.Fatalf("retaliation chase mover = %+v", mover)
		}
	})

	t.Run("missing target releases retaliation and resumes wander", func(t *testing.T) {
		ops, instance := monsterLegFixture(t, passiveTactics())
		configureAttack(t, ops, instance)
		if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1)) {
			t.Fatal("live passive monster rejected retaliation target")
		}

		push := &fakePusher{}
		recordTestMonsterBootstrap(ops, []SessionSnapshot{playerSessionAt(2, 1005, 1000)}, t0)
		ops.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(2, 1005, 1000)}, push)
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.Mode() != monster.MoverIdle || mover.TargetGID() != 0 || mover.RetaliationPending() {
			t.Fatalf("missing-target retaliation did not settle back to idle: %+v", mover)
		}

		push.toSession, push.toDivision = nil, nil
		ops.RunMonsterLeg(t0+monster.RetailIdleDelayMaxMs+1,
			[]SessionSnapshot{playerSessionAt(2, 1005, 1000)}, push)
		frames := monsterFrames(push)
		if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
			t.Fatalf("released retaliation wander = %+v, want a normal wander goal", frames)
		}
	})

	t.Run("missing attack plan fails closed and resumes wander", func(t *testing.T) {
		ops, instance := monsterLegFixture(t, passiveTactics())
		ops.AttackPlan = nil
		if !ops.Monsters.ArmRetaliation(monsterTestDivision, instance.Gid, PlayerObjectID(1)) {
			t.Fatal("live passive monster rejected retaliation target")
		}

		push := &fakePusher{}
		recordTestMonsterBootstrap(ops, []SessionSnapshot{playerSessionAt(1, 1005, 1000)}, t0)
		ops.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(1, 1005, 1000)}, push)
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.Mode() != monster.MoverIdle || mover.TargetGID() != 0 || mover.RetaliationPending() {
			t.Fatalf("no-plan retaliation stranded the mover: %+v", mover)
		}

		push.toSession, push.toDivision = nil, nil
		ops.RunMonsterLeg(t0+monster.RetailIdleDelayMaxMs+1,
			[]SessionSnapshot{playerSessionAt(1, 1005, 1000)}, push)
		frames := monsterFrames(push)
		if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
			t.Fatalf("no-plan fallback wander = %+v, want a normal wander goal", frames)
		}
	})
}

// Chase re-aim source policy is direction-based, not cadence-based and not a
// one-shot lifecycle latch. A shallow continuation keeps the client's current
// integration uninterrupted; a hard cut ships the server's live departure so
// the native logical/visual correction planes can converge.
func TestMonsterChaseReaimSourcesOnlySharpTurns(t *testing.T) {
	const t0 = int64(1_784_000_000_000)

	t.Run("shallow continuation", func(t *testing.T) {
		ops, instance := monsterLegFixture(t, aggressiveTactics())
		push := &fakePusher{}
		ops.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(1, 1050, 1000)}, push)

		push.toSession, push.toDivision = nil, nil
		ops.RunMonsterLeg(t0+500, []SessionSnapshot{playerSessionAt(1, 1080, 1000)}, push)
		frames := monsterFrames(push)
		if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
			t.Fatalf("shallow re-aim frames = %+v, want one 0xB738 goal", frames)
		}
		if _, _, _, _, _, sourcePresent := decodeGoalPayload(t, frames[0].Payload); sourcePresent {
			t.Fatal("a shallow same-direction chase re-aim must not restart the source cursor")
		}
		mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if mover.Mode() != monster.MoverChasing {
			t.Fatalf("mover mode = %v, want chasing", mover.Mode())
		}
	})

	t.Run("hard cut", func(t *testing.T) {
		ops, instance := monsterLegFixture(t, aggressiveTactics())
		push := &fakePusher{}
		ops.RunMonsterLeg(t0, []SessionSnapshot{playerSessionAt(1, 1050, 1000)}, push)
		before, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		wantSource := before.LivePoseAt(t0+500, nil)

		push.toSession, push.toDivision = nil, nil
		ops.RunMonsterLeg(t0+500, []SessionSnapshot{playerSessionAt(1, 1000, 1050)}, push)
		frames := monsterFrames(push)
		if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
			t.Fatalf("hard-cut frames = %+v, want one 0xB738 goal", frames)
		}
		if _, _, _, _, _, sourcePresent := decodeGoalPayload(t, frames[0].Payload); !sourcePresent {
			t.Fatal("a >45-degree chase cut must carry the live authoritative source")
		}
		source := decodeGoalSource(t, frames[0].Payload)
		if source.RegionID != wantSource.RegionID ||
			math.Abs(source.X-wantSource.X) > 0.11 ||
			math.Abs(source.Y-wantSource.Y) > 0.001 ||
			math.Abs(source.Z-wantSource.Z) > 0.11 {
			t.Fatalf("hard-cut source = %+v, want live departure %+v", source, wantSource)
		}
	})
}

// Leash: a chaser whose live position exceeds ChaseLeash from its nest
// anchor gives up, drops the target and walks home (the runaway-chase
// guard).
func TestMonsterChaseLeashReturnsHome(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	fixture := aggressiveTactics()
	// Test-local OVERRIDE (not a private copy of the P3 value): a tiny
	// leash so the first chase step breaches it - this test exercises the
	// give-up MECHANISM, not the signed leash number.
	fixture.ChaseLeash = 5
	ops, instance := monsterLegFixture(t, fixture)
	playerX := 1000 + math.Floor(fixture.SightRange*0.8) // inside sight
	sessions := []SessionSnapshot{playerSessionAt(1, playerX, 1000)}
	push := &fakePusher{}

	ops.RunMonsterLeg(t0, sessions, push) // acquire + chase bundle (0x3122 run + goal)
	push.toSession, push.toDivision = nil, nil
	// Mid-flight the live pose passes the 5u leash: the next tick must
	// emit the return bundle - 0x3122 back to walk, then the goal toward
	// the anchor, target dropped.
	ops.RunMonsterLeg(t0+1000, sessions, push)
	frames := monsterFrames(push)
	if len(frames) != 2 || frames[0].Opcode != wire.OpObjectStateRefresh || frames[1].Opcode != OpMovementAck {
		t.Fatalf("leash frames = %+v, want [0x3122 walk-channel, 0xB738 return goal]", frames)
	}
	refresh, err := wire.DecodeObjectStateRefresh(frames[0].Payload)
	if err != nil || refresh.Value != wire.MoveStateWalk {
		t.Fatalf("leash channel push = %+v (%v), want walk", refresh, err)
	}
	_, _, x, _, z, sourcePresent := decodeGoalPayload(t, frames[1].Payload)
	if x != 1000 || z != 1000 {
		t.Fatalf("return goal = (%d, %d), want the nest anchor (1000, 1000)", x, z)
	}
	if !sourcePresent {
		t.Fatal("the leash reversal must source its live departure (>45-degree turn)")
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverReturning || mover.TargetGID() != 0 {
		t.Fatalf("mover after leash = %+v, want returning with no target", mover)
	}
}

// A 0/0-speed type never moves and never emits (no invented speeds).
func TestMonsterZeroSpeedNeverMoves(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, _ := monsterLegFixture(t, passiveTactics())
	stationary := monsterInstanceByRef(t, ops.Monsters, 2000)
	push := &fakePusher{}
	for i := int64(0); i < 40; i++ {
		ops.RunMonsterLeg(t0+i*250, []SessionSnapshot{playerSessionAt(1, 3000, 3000)}, push)
	}
	for _, frame := range monsterFrames(push) {
		if frame.Opcode == OpMovementAck {
			gid := binary.LittleEndian.Uint32(frame.Payload[0:4])
			if gid == stationary.Gid {
				t.Fatal("a 0/0-speed monster emitted a move goal")
			}
		}
	}
}

// Divisions without sessions do not tick their monsters at all.
func TestMonsterLegSkipsUnwatchedDivisions(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, _ := monsterLegFixture(t, passiveTactics())
	push := &fakePusher{}
	ops.RunMonsterLeg(t0, nil, push) // no sessions anywhere
	if len(push.toDivision) != 0 {
		t.Fatalf("unwatched division pushed %d frames", len(push.toDivision))
	}
}

// MID-MOTION scope-exit (coordinator seq463 / G-SRV seq465 co-sign gate):
// an entity leaving the viewer's interest WHILE its move segment is in flight
// must be evicted with an explicit despawn, and the viewer must receive
// ZERO further mover frames for that gid - no stranded moving ghost, no
// goal/glide after despawn. Ordering holds by construction (scope
// visibility runs before mover delivery each tick); this canary witnesses
// it for both the wander and the chase arm.
func TestMonsterMidMotionScopeExit(t *testing.T) {
	const t0 = int64(1_784_000_000_000)

	arms := []struct {
		name    string
		tactics monster.Tactics
		// player position inside the nest's interest block: near enough to
		// be acquired in the chase arm, beyond sight in the wander arm.
		playerX float64
	}{
		{name: "mid-wander", tactics: passiveTactics(), playerX: 300},
		{name: "mid-chase", tactics: aggressiveTactics(), playerX: 130}, // ~30u from the nest at (100,100)
	}

	for _, arm := range arms {
		t.Run(arm.name, func(t *testing.T) {
			regionA := RegionIDForSectors(16, 16)
			refs := map[uint32]monster.MonsterRef{
				1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100, BodyRadius: 6},
			}
			ops := &MonsterMoverOps{
				Monsters: NewMonsterState(monster.TemplateFromParts(refs, []monster.NestRow{
					{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: regionA, X: 100, Y: 10, Z: 100}},
				})),
				TacticsFor: fixedTactics(arm.tactics),
				Rand:       func() float64 { return 0.25 }, // nonzero wander leg
			}
			ops.AttackPlan = func(monster.Instance, uint32, float64) (MonsterAttackPlan, bool) {
				return MonsterAttackPlan{SkillID: 0x1234, Reach: ActionReach(6), CooldownMs: 1000, ActionLifecycleMs: 600}, true
			}
			ops.Monsters.StartDivision(monsterTestDivision)
			ops.Monsters.AdvancePopulation(ops.Monsters.CurrentTimeMillis())
			instances := ops.Monsters.InstancesInRegions(monsterTestDivision, []uint16{regionA})
			for _, instance := range instances {
				mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
				if err := mover.Transition(monster.MoverEventSpawnHoldElapsed, 0); err != nil {
					t.Fatalf("seed idle mover: %v", err)
				}
				mover.BehaviorDeadlineMs = 0
				ops.Monsters.CommitMover(monsterTestDivision, instance.Gid, mover)
			}
			push := &fakePusher{}

			// Tick 1: seed + the mover starts a leg (wander goal or
			// immediate chase acquisition) - segment now IN FLIGHT.
			session := SessionSnapshot{
				SessionID: "viewer", DivisionID: monsterTestDivision, CharacterID: 7,
				WorldInstance:  0x10001,
				Population:     instance.Lease{ID: instance.Pack(1, 1), Generation: 1},
				CombatEligible: true,
				World:          WorldState{Spawn: Spawn{RegionID: regionA, X: arm.playerX, Y: 10, Z: 100}, SpawnSet: true},
				BodyRadius:     BodyRadius(4),
			}
			ops.RunMonsterLeg(t0, []SessionSnapshot{session}, push)
			goals := 0
			for _, frame := range sessionFrames(push, "viewer") {
				if frame.Opcode == OpMovementAck {
					goals++
				}
			}
			if goals != 1 {
				t.Fatalf("tick1 delivered %d goals, want 1 (the in-flight leg)", goals)
			}
			var gid uint32
			for shownGid := range ops.shownMonsters["viewer"] {
				gid = shownGid
			}
			mover, _ := ops.Monsters.Mover(monsterTestDivision, gid)
			if !mover.InFlight(t0 + 250) {
				t.Fatal("precondition failed: the segment must still be in flight at the exit tick")
			}

			// Tick 2 (+250ms, segment still maturing): the viewer jumps
			// three sectors east - the monster leaves scope MID-MOTION.
			push.toSession, push.toDivision = nil, nil
			session.World = WorldState{Spawn: Spawn{RegionID: RegionIDForSectors(19, 16), X: 900, Y: 10, Z: 900}, SpawnSet: true}
			ops.RunMonsterLeg(t0+250, []SessionSnapshot{session}, push)
			frames := sessionFrames(push, "viewer")
			despawns, moverFrames := 0, 0
			for _, frame := range frames {
				switch frame.Opcode {
				case wire.OpObjectDespawn:
					despawns++
				case OpMovementAck, wire.OpObjectSourceMove, wire.OpObjectSourceCorrection, wire.OpObjectStateRefresh:
					moverFrames++
				}
			}
			if despawns != 1 {
				t.Fatalf("%s exit tick despawns = %d, want exactly 1 (0x36AB for the in-flight monster)", arm.name, despawns)
			}
			if moverFrames != 0 {
				t.Fatalf("%s exit tick delivered %d mover frames to the viewer, want 0 (never a goal/glide after despawn)", arm.name, moverFrames)
			}

			// Ticks 3-4: still out of scope - no mover frames, no more
			// despawns (single-shot eviction), no stranded ghost traffic.
			push.toSession, push.toDivision = nil, nil
			ops.RunMonsterLeg(t0+500, []SessionSnapshot{session}, push)
			ops.RunMonsterLeg(t0+750, []SessionSnapshot{session}, push)
			if frames := sessionFrames(push, "viewer"); len(frames) != 0 {
				t.Fatalf("%s post-exit ticks delivered %d frames, want 0", arm.name, len(frames))
			}
		})
	}
}

// TestSettleCorrectionMatchesTheGoalTheClientWasGiven pins BUG-12: "when
// monster moves, then stops, they always slide a 1cm after stop".
//
// The 0xB738 goal carries x/y/z as ROUNDED u16, so an integer position is the
// only thing the client can ever walk to. The 0xB2F5 settle encodes f32. If
// the server keeps a FRACTIONAL destination as its authoritative arrival
// pose, the settle hands the client a position up to 0.5u per axis away from
// where it just stopped, and it slides that residual off after halting.
//
// The invariant is therefore a RELATIONSHIP between two packets, not a value
// in either one: the position the settle corrects to must be the position
// the goal named. Several wander angles are driven because a single one can
// land on an integral destination by luck and pass without exercising the
// quantisation at all.
func TestSettleCorrectionMatchesTheGoalTheClientWasGiven(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	for _, r := range []float64{0.1, 0.2, 0.3, 0.37, 0.5, 0.77, 0.9} {
		ops, instance := monsterLegFixture(t, passiveTactics())
		ops.Rand = sequenceRand(0.5, r, 0.5)
		sessions := []SessionSnapshot{playerSessionAt(1, 1200, 1200)} // visible block, out of sight
		push := &fakePusher{}

		recordTestMonsterBootstrap(ops, sessions, t0)

		ops.RunMonsterLeg(t0, sessions, push)
		frames := monsterFrames(push)
		if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
			t.Fatalf("rand %v: frames = %+v, want one 0xB738 goal", r, frames)
		}
		_, goalRegion, goalX, _, goalZ, _ := decodeGoalPayload(t, frames[0].Payload)

		mover, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
		if !ok || mover.ArriveMs <= mover.DepartMs {
			t.Fatalf("rand %v: no in-flight segment committed", r)
		}
		// The stored truth must itself be wire-expressible; otherwise the
		// agreement below could only ever hold by rounding luck.
		if mover.To.X != math.Trunc(mover.To.X) || mover.To.Z != math.Trunc(mover.To.Z) {
			t.Errorf("rand %v: committed destination (%v, %v) is fractional - the client can only walk to integers, so the settle will slide the remainder (BUG-12)",
				r, mover.To.X, mover.To.Z)
		}

		push.toSession, push.toDivision = nil, nil
		if mover.BehaviorDeadlineMs < mover.ArriveMs {
			ops.RunMonsterLeg(mover.BehaviorDeadlineMs+1, sessions, push)
			push.toSession, push.toDivision = nil, nil
		}
		ops.RunMonsterLeg(mover.ArriveMs+1, sessions, push)
		settle := monsterFrames(push)
		if len(settle) != 1 || settle[0].Opcode != wire.OpObjectSourceCorrection {
			t.Fatalf("rand %v: settle frames = %+v, want one 0xB2F5 correction", r, settle)
		}
		correction, err := wire.DecodeObjectSourceCorrection(settle[0].Payload)
		if err != nil {
			t.Fatalf("rand %v: settle decode: %v", r, err)
		}
		if correction.RegionID != goalRegion {
			t.Errorf("rand %v: settle region %d != goal region %d", r, correction.RegionID, goalRegion)
		}
		// Exact equality on purpose: any non-zero delta here IS the slide,
		// and a tolerance would silently admit the defect back.
		if correction.X != float32(goalX) || correction.Z != float32(goalZ) {
			t.Errorf("rand %v: settle corrects to (%v, %v) but the client was told to walk to (%d, %d) - it stops at the goal and then slides %.3fu/%.3fu (BUG-12)",
				r, correction.X, correction.Z, goalX, goalZ,
				math.Abs(float64(correction.X)-float64(goalX)), math.Abs(float64(correction.Z)-float64(goalZ)))
		}
	}
}

// TestHeadingWordTowardUsesNativeYawConvention pins BUG-11's root cause.
//
// Native yaw 0 faces -z: Math_YawToDirVec (sub_8788c0) is
// {sin(yaw), 0, -cos(yaw)}, and Math_DirVecToYaw (sub_8791a0) inverts it as
// acos(-z/len) mirrored for x<0 - algebraically Atan2(dx, -dz).
//

// recordTestMonsterBootstrap models the already-delivered object list. Tests
// of initial admission deliberately omit it and must observe create frames.
func recordTestMonsterBootstrap(ops *MonsterMoverOps, sessions []SessionSnapshot, now int64) {
	for _, session := range sessions {
		visible := ops.Monsters.PopulationInterestInstances(session.DivisionID, session.Population,
			worldgeom.RegionXZ{RegionID: session.World.Spawn.RegionID, X: session.World.Spawn.X, Z: session.World.Spawn.Z}, now)
		var gids []uint32
		for _, actor := range visible {
			gids = append(gids, actor.Gid)
		}
		ops.Monsters.RecordObjectList(session.DivisionID, PlayerObjectID(session.CharacterID), gids)
	}
}
