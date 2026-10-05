package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

func TestFreshMonsterRunsRetailSpawnIdleWanderLifecycle(t *testing.T) {
	const t0 = int64(1_784_000_000_000)
	template := monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG", WalkSpeed: 8, RunSpeed: 22},
	}, []monster.NestRow{
		{SpawnPoint: monster.SpawnPoint{RefObjID: 1933, RegionID: monsterTestRegion, X: 1000, Y: 20, Z: 1000}},
	})
	registry := NewMonsterState(template)
	registry.SetTimeSource(func() time.Time { return time.UnixMilli(t0) })
	instance := monsterInstanceByRef(t, registry, 1933)
	ops := &MonsterMoverOps{
		Monsters:   registry,
		TacticsFor: fixedTactics(passiveTactics()),
		// spawn->idle delay min; repeat roll 10; repeat delay min;
		// next roll 50 enters wander; angle quarter-turn; wander timer min.
		Rand: sequenceRand(0, 1999.0/32768, 0.1, 0, 1999.0/32768, 0.5, 0, 0, 0, 0, 0),
	}

	if frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, t0+monster.RetailSpawnHoldMs-1); len(frames) != 0 {
		t.Fatalf("spawn hold emitted frames: %+v", frames)
	}
	mover, _ := registry.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverSpawning {
		t.Fatalf("pre-deadline state = %s, want spawning", mover.Mode())
	}

	spawnSettledAt := t0 + monster.RetailSpawnHoldMs
	if frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, spawnSettledAt); len(frames) != 0 {
		t.Fatalf("spawn->idle emitted movement: %+v", frames)
	}
	mover, _ = registry.Mover(monsterTestDivision, instance.Gid)
	firstIdleDeadline := spawnSettledAt + monster.RetailIdleDelayMinMs
	if mover.Mode() != monster.MoverIdle || mover.BehaviorDeadlineMs != firstIdleDeadline {
		t.Fatalf("spawn completion = %+v, want idle deadline %d", mover, firstIdleDeadline)
	}

	if frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, firstIdleDeadline+1); len(frames) != 0 {
		t.Fatalf("idle-repeat emitted movement: %+v", frames)
	}
	mover, _ = registry.Mover(monsterTestDivision, instance.Gid)
	secondIdleDeadline := firstIdleDeadline + 1 + monster.RetailIdleDelayMinMs
	if mover.Mode() != monster.MoverIdle || mover.LastEvent() != monster.MoverEventIdleRepeated ||
		mover.BehaviorDeadlineMs != secondIdleDeadline {
		t.Fatalf("idle-repeat = %+v, want a second idle deadline %d", mover, secondIdleDeadline)
	}

	frames, _ := ops.advanceInstance(monsterTestDivision, instance, nil, secondIdleDeadline+1)
	if len(frames) != 1 || frames[0].Opcode != OpMovementAck {
		t.Fatalf("idle->wander frames = %+v, want one goal", frames)
	}
	mover, _ = registry.Mover(monsterTestDivision, instance.Gid)
	if mover.Mode() != monster.MoverWandering || !mover.InFlight(secondIdleDeadline) {
		t.Fatalf("idle->wander state = %+v", mover)
	}
}
