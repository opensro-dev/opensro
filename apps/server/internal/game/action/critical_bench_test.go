/*
===========================================================================

critical_bench_test.go - per-tick cost of the monster critical-history sweep

retireMonsterCriticals runs once per GameWorld tick (#1820). The bench
holds the steady state a live shard reaches: thousands of living monsters
that have rolled at least once, a few hundred players, and nothing due.

===========================================================================
*/

package action

import (
	"fmt"
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	benchCriticalRegion   = uint16(0x62A8)
	benchCriticalMonsters = 3000
	benchCriticalPlayers  = 300
)

/*
================
newCriticalSweepFixture

A population of benchCriticalMonsters living monsters, each with an
initialized history, plus benchCriticalPlayers player histories.
================
*/
func newCriticalSweepFixture(tb testing.TB) *Runtime {
	tb.Helper()
	ref := monster.MonsterRef{RefObjID: 1933, TidWord: 0x00c6, Codename: "MOB_CH_MANGNYANG", Level: 1, MaxHP: 1000, BodyRadius: 6}
	nests := make([]monster.NestRow, benchCriticalMonsters)
	for i := range nests {
		nests[i] = monster.NestRow{
			SpawnPoint:     monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: benchCriticalRegion, X: float64(i % 1920), Y: 20, Z: float64(i / 1920 * 10)},
			RetailEvidence: true,
			MaxCount:       1,
		}
	}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nests))
	monsters.StartDivision(testDivision)
	monsters.AdvancePopulation(monsters.CurrentTimeMillis())
	instances := monsters.InstancesInRegions(testDivision, []uint16{benchCriticalRegion})
	if len(instances) != benchCriticalMonsters {
		tb.Fatalf("fixture materialized %d monsters, want %d", len(instances), benchCriticalMonsters)
	}
	rt := &Runtime{Monsters: monsters}
	history := combat.Probability{Initialized: true, Threshold: 3}
	rt.criticals.mu.Lock()
	for _, instance := range instances {
		rt.rememberProbability(criticalActor{division: testDivision, monster: instance.Gid}, 2, history)
		rt.rememberProbability(criticalActor{division: testDivision, monster: instance.Gid}, 0x42000002, history)
	}
	for i := 0; i < benchCriticalPlayers; i++ {
		rt.rememberProbability(criticalActor{division: testDivision, character: fmt.Sprintf("player%d", i)}, 2, history)
	}
	rt.criticals.mu.Unlock()
	return rt
}

/*
================
BenchmarkRetireMonsterCriticals

Steady state: every monster lives, so the sweep retires nothing.
================
*/
func BenchmarkRetireMonsterCriticals(b *testing.B) {
	rt := newCriticalSweepFixture(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		rt.retireMonsterCriticals()
	}
	b.StopTimer()
	if got := len(rt.criticals.actors); got != benchCriticalMonsters+benchCriticalPlayers {
		b.Fatalf("sweep retired living histories: %d left", got)
	}
}
