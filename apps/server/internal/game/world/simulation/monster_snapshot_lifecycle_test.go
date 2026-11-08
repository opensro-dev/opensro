package simulation

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

func TestMonsterFirstSightReplaysInFlightMovementIntentOnce(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	ops, instance := monsterLegFixture(t, passiveTactics())
	mover, ok := ops.Monsters.Mover(monsterTestDivision, instance.Gid)
	if !ok {
		t.Fatal("materialized monster has no mover")
	}
	if err := mover.Transition(monster.MoverEventStartWander, 0); err != nil {
		t.Fatalf("idle -> wander: %v", err)
	}
	frames := ops.commitSegment(
		monsterTestDivision,
		&instance,
		mover,
		monster.Pose{RegionID: instance.Spawn.RegionID, X: 1100, Y: 20, Z: 1000},
		instance.Ref.RunSpeed,
		wire.MoveStateRun,
		t0,
	)
	if len(frames) != 2 {
		t.Fatalf("initial segment frames = %d, want run-channel + goal", len(frames))
	}

	push := &fakePusher{}
	session := playerSessionAt(7, 1050, 1050)
	recordTestMonsterBootstrap(ops, []SessionSnapshot{session}, t0+1_000)
	ops.RunMonsterLeg(t0+1_000, []SessionSnapshot{session}, push)
	got := sessionFrames(push, session.SessionID)
	var refreshes, goals int
	for _, frame := range got {
		switch frame.Opcode {
		case wire.OpObjectStateRefresh:
			refreshes++
		case OpMovementAck:
			goals++
			gid, _, _, _, _, sourcePresent := decodeGoalPayload(t, frame.Payload)
			if gid != instance.Gid || !sourcePresent {
				t.Fatalf("replayed goal gid/source = %d/%v, want %d/true", gid, sourcePresent, instance.Gid)
			}
			source := decodeGoalSource(t, frame.Payload)
			if math.Abs(source.X-1022) > 0.11 || math.Abs(source.Z-1000) > 0.11 {
				t.Fatalf("replayed source = (%.2f,%.2f), want live pose near (1022,1000)", source.X, source.Z)
			}
		}
	}
	if refreshes != 1 || goals != 1 {
		t.Fatalf("first-sight continuation = %d channel / %d goal, want 1/1", refreshes, goals)
	}

	push.toSession = nil
	ops.RunMonsterLeg(t0+1_250, []SessionSnapshot{session}, push)
	for _, frame := range sessionFrames(push, session.SessionID) {
		if frame.Opcode == OpMovementAck {
			t.Fatal("in-flight goal replayed after first-sight reconciliation")
		}
	}
}
