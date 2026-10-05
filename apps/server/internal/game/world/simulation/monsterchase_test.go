package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
)

func monsterMovementAckFrames(push *fakePusher) []Frame {
	var movement []Frame
	for _, frame := range monsterFrames(push) {
		if frame.Opcode == OpMovementAck {
			movement = append(movement, frame)
		}
	}
	return movement
}

func TestMonsterChaseOwnsGuidanceSeparatelyFromStandOffGoal(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}
	target := playerSessionAt(1, 1050, 1000)

	ops.RunMonsterLeg(t0, []SessionSnapshot{target}, push)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	guidance, ok := mover.ChaseGuidance()
	if !ok {
		t.Fatal("chase did not retain the target guidance that authored its goal")
	}
	destination := guidance.Destination()
	if guidance.TargetMoving() {
		t.Fatal("stationary target guidance was recorded as moving")
	}
	if destination.RegionID != target.World.Spawn.RegionID ||
		destination.X != target.World.Spawn.X || destination.Z != target.World.Spawn.Z {
		t.Fatalf("chase guidance = %+v, want player movement destination %+v", guidance, target.World.Spawn)
	}
	if mover.To.X == destination.X && mover.To.Z == destination.Z {
		t.Fatalf("chase goal %+v aliases guidance %+v; want the derived stand-off point", mover.To, guidance)
	}
	if distance := planarDistance(mover.To, destination); math.Abs(distance-14) > 0.01 {
		t.Fatalf("chase goal stand-off = %.3f, want body+body+inner reach = 14", distance)
	}

	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(t0+500, []SessionSnapshot{target}, push)
	if frames := monsterFrames(push); len(frames) != 0 {
		t.Fatalf("stationary target caused a derived-goal re-aim: %+v", frames)
	}
}

func TestMonsterChaseApproachesLiveTargetNotOppositeCommandDestination(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, _ := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}
	target := playerSessionAt(1, 1200, 1000)
	target.World.MoveSegment = &MoveSegment{
		From:        Spawn{RegionID: monsterTestRegion, X: 900, Y: 20, Z: 1000},
		StartedAtMs: t0,
		ArrivesAtMs: t0 + 5000,
	}

	ops.RunMonsterLeg(t0, []SessionSnapshot{target}, push)
	frames := monsterMovementAckFrames(push)
	if len(frames) != 1 {
		t.Fatalf("initial chase frames = %+v, want one B738 leg", frames)
	}
	_, _, x, _, z, _ := decodeGoalPayload(t, frames[0].Payload)
	if x != 914 || z != 1000 {
		t.Fatalf("initial chase goal = (%d, %d), want live-target stand-off (914, 1000)", x, z)
	}
	if x >= 1000 {
		t.Fatalf("chase goal x = %d, command destination at 1200 pulled the monster away from live target x=900", x)
	}
}

func TestMonsterChaseRefreshesMovingTargetBeforeTheCurrentGoalExpires(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}
	target := playerSessionAt(1, 1150, 1000)
	target.World.MoveSegment = &MoveSegment{
		From:        Spawn{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000},
		StartedAtMs: t0,
		ArrivesAtMs: t0 + 5000,
	}

	ops.RunMonsterLeg(t0, []SessionSnapshot{target}, push)
	frames := monsterMovementAckFrames(push)
	if len(frames) != 1 {
		t.Fatalf("initial chase frames = %+v, want one B738 guidance leg", frames)
	}
	_, _, x, _, z, _ := decodeGoalPayload(t, frames[0].Payload)
	if x != 1036 || z != 1000 {
		t.Fatalf("initial chase goal = (%d, %d), want stand-off from live target (1036, 1000)", x, z)
	}

	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(t0+referenceChaseRefreshMinMs-1, []SessionSnapshot{target}, push)
	if frames := monsterFrames(push); len(frames) != 0 {
		t.Fatalf("moving target refreshed before the reference pacing floor: %+v", frames)
	}

	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(t0+referenceChaseRefreshMinMs, []SessionSnapshot{target}, push)
	frames = monsterMovementAckFrames(push)
	if len(frames) != 1 {
		t.Fatalf("moving-target refresh frames = %+v, want one B738 before the old goal expires", frames)
	}
	_, _, x, _, z, sourcePresent := decodeGoalPayload(t, frames[0].Payload)
	if x != 1038 || z != 1000 {
		t.Fatalf("refreshed chase goal = (%d, %d), want stand-off from the new live target (1038, 1000)", x, z)
	}
	if sourcePresent {
		t.Fatal("straight moving-target refresh unexpectedly re-anchored the client source cursor")
	}
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	guidance, ok := mover.ChaseGuidance()
	if !ok || !guidance.TargetMoving() {
		t.Fatalf("moving-target guidance = %+v (ok=%v), want in-flight ownership", guidance, ok)
	}

	push.toSession, push.toDivision = nil, nil
	commandChangedAt := t0 + referenceChaseRefreshMinMs + 25
	target.World.MoveSegment = &MoveSegment{
		From:        target.World.LiveSpawnAt(commandChangedAt),
		StartedAtMs: commandChangedAt,
		ArrivesAtMs: commandChangedAt + 5000,
	}
	target.World.Spawn.X = 1250
	ops.RunMonsterLeg(commandChangedAt, []SessionSnapshot{target}, push)
	frames = monsterMovementAckFrames(push)
	if len(frames) != 1 {
		t.Fatalf("replacement target command frames = %+v, want an immediate B738 re-plan", frames)
	}
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	guidance, ok = mover.ChaseGuidance()
	destination := guidance.Destination()
	if !ok || !guidance.TargetMoving() || destination.X != 1250 || destination.Z != 1000 {
		t.Fatalf("replacement guidance = %+v (ok=%v), want target goal (1250, 1000)", guidance, ok)
	}
}

func TestMonsterChasePublishesContinuousStraightGuidanceAtProductionCadence(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, _ := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}
	target := playerSessionAt(1, 1550, 1000)
	target.World.MoveSegment = &MoveSegment{
		From:        Spawn{RegionID: monsterTestRegion, X: 1050, Y: 20, Z: 1000},
		StartedAtMs: t0,
		ArrivesAtMs: t0 + 5000,
	}

	previousGoalX := uint16(0)
	for step := int64(0); step <= 10; step++ {
		push.toSession, push.toDivision = nil, nil
		nowMs := t0 + step*DefaultTickInterval.Milliseconds()
		ops.RunMonsterLeg(nowMs, []SessionSnapshot{target}, push)

		frames := monsterMovementAckFrames(push)
		if len(frames) != 1 {
			t.Fatalf("step %d movement frames = %+v, want one continuous guidance leg", step, frames)
		}
		_, _, goalX, _, _, sourcePresent := decodeGoalPayload(t, frames[0].Payload)
		if step > 0 && sourcePresent {
			t.Fatalf("step %d straight refresh re-anchored the client source cursor", step)
		}
		if step > 0 && goalX <= previousGoalX {
			t.Fatalf("step %d goal x = %d, want monotonic guidance after %d", step, goalX, previousGoalX)
		}
		previousGoalX = goalX
	}
}

func TestMonsterChaseRefreshesWhenTargetMovementSettles(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, aggressiveTactics())
	push := &fakePusher{}
	target := playerSessionAt(1, 1100, 1000)
	target.World.MoveSegment = &MoveSegment{
		From:        Spawn{RegionID: monsterTestRegion, X: 1090, Y: 20, Z: 1000},
		StartedAtMs: t0,
		ArrivesAtMs: t0 + 100,
	}

	ops.RunMonsterLeg(t0, []SessionSnapshot{target}, push)
	mover, _ := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if guidance, ok := mover.ChaseGuidance(); !ok || !guidance.TargetMoving() {
		t.Fatalf("initial guidance = %+v (ok=%v), want moving", guidance, ok)
	}

	push.toSession, push.toDivision = nil, nil
	ops.RunMonsterLeg(t0+100, []SessionSnapshot{target}, push)
	frames := monsterMovementAckFrames(push)
	if len(frames) != 1 {
		t.Fatalf("movement-completion refresh frames = %+v, want one final B738", frames)
	}
	mover, _ = ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if guidance, ok := mover.ChaseGuidance(); !ok || guidance.TargetMoving() {
		t.Fatalf("settled guidance = %+v (ok=%v), want stationary", guidance, ok)
	}
}

func TestMonsterMovementSourceTurnThresholdIsStrictlyGreaterThan45Degrees(t *testing.T) {
	from := monster.Pose{RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}
	mover := monster.MoverState{Pose: from} // wire heading 0 faces +X
	destinationAt := func(angle float64) monster.Pose {
		return monster.Pose{
			RegionID: from.RegionID,
			X:        from.X + math.Cos(angle)*100,
			Y:        from.Y,
			Z:        from.Z + math.Sin(angle)*100,
		}
	}

	if monsterMovementSourceRequired(mover, from, destinationAt(math.Pi/4-1e-6), false) {
		t.Fatal("a turn below 45 degrees must not carry a source")
	}
	if monsterMovementSourceRequired(mover, from, destinationAt(math.Pi/4), false) {
		t.Fatal("the reference comparison is strict: an exact 45-degree turn must not carry a source")
	}
	if !monsterMovementSourceRequired(mover, from, destinationAt(math.Pi/4+1e-6), false) {
		t.Fatal("a turn above 45 degrees must carry a source")
	}
}
