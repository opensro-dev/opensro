package simulation

import (
	"context"
	"opensro.online/server/internal/game/world/monster"
	"testing"
)

func TestTickerSettlesActionsBeforeDivisionAIInBothExecutionModes(t *testing.T) {
	for _, scheduled := range []bool{false, true} {
		ops, actor := monsterLegFixture(t, aggressiveTactics())
		lease, _ := ops.Monsters.ObjectPopulation(monsterTestDivision, actor.Gid)
		source := &fakeSource{sessions: []SessionSnapshot{{SessionID: "timer-order", DivisionID: monsterTestDivision,
			CharacterID: 1, Population: lease, CombatEligible: true, BodyRadius: 4,
			World: WorldState{SpawnSet: true, Spawn: Spawn{RegionID: actor.Spawn.RegionID, X: actor.Spawn.X, Z: actor.Spawn.Z}}}}}
		ticker := newTestTicker(source, &fakePusher{})
		var order []string
		ops.RunAction = func(_ string, run func(MonsterAttackOperation)) {
			order = append(order, "AI")
			run(func(string, monster.Instance, uint32, uint32, int64) MonsterAttackResult {
				return MonsterAttackResult{}
			})
		}
		ticker.Monsters = ops
		ticker.BeforeHooks = []NamedTickHook{{Hook: func(now int64) []DivisionFrames {
			if now != 100000 {
				t.Error("wrong pre-action clock", now)
			}
			order = append(order, "settle")
			return nil
		}}}
		ticker.Hooks = []NamedTickHook{{Hook: func(int64) []DivisionFrames { order = append(order, "post"); return nil }}}
		if scheduled {
			ctx, cancel := context.WithCancel(context.Background())
			inbox := make(chan shardTickBatch)
			done := make(chan struct{})
			go func() { ticker.runShard(ctx, inbox); close(done) }()
			ticker.runScheduledTick(ctx, 100000, []chan shardTickBatch{inbox})
			cancel()
			<-done
		} else {
			ticker.RunTick(100000)
		}
		if len(order) < 3 || order[0] != "settle" || order[len(order)-1] != "post" {
			t.Fatalf("scheduled=%v order=%v", scheduled, order)
		}
		for _, phase := range order[1 : len(order)-1] {
			if phase != "AI" {
				t.Fatalf("phase interleaved with division work: %v", order)
			}
		}
	}
}
