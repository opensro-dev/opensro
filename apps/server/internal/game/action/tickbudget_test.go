/*
===========================================================================

tickbudget_test.go - the synthetic-shard tick allocation guard (#1943)

One benchmarked tick of a synthetic shard: players with realistic
inventories and attack pets out, a monster population advancing, NPC
patrol frames building, and a ground plane snapshotting. The guard test
fails when a tick allocates more than the pinned budget, so a change
that reintroduces per-tick copying lands red instead of back on the
live shard. The budgets are today's main plus ten percent; every #1943
fix lowers them to its measured after.

===========================================================================
*/
package action

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// The synthetic shard's shape: busy-division scale, not stress scale.
const (
	tickBudgetPlayers      = 16
	tickBudgetInventoryRow = 48
	tickBudgetGroundDrops  = 300
	tickBudgetWarmupTicks  = 10
	tickBudgetMeasureTicks = 200
)

// Pinned budgets: the measured tick plus ten percent. One constant per
// metric; a #1943 fix lowers the constant it improves, never raises it.
// History: f3004434 ticked 1,977,338 B / 5,505 allocs; after #1944,
// #1945 and #1948 (dd2e3691c) 1,299,000 B / 3,458 allocs; after the
// crow-brood and pet-spirit owner-copy admissions 1,131,000 B /
// 2,962 allocs; after the pet-periodic admissions (the pet tick, the
// persistent-effect pulse, the crow recipient walk and the COS/monster
// source capture) 984,000 B / 2,380 allocs.
const (
	tickBudgetBytesPerTick  = 984000
	tickBudgetAllocsPerTick = 2380
)

/*
================
syntheticShard

A runtime holding tickBudgetPlayers characters in one division, each
with a full inventory and a summoned attack pet, a monster population
in the same division, and a ground plane of dropped items.
================
*/
type syntheticShard struct {
	rt     *Runtime
	clock  *fakeClock
	hook   func(int64) []simulation.DivisionFrames
	roster []simulation.NpcDef
	anchor simulation.Spawn
}

/*
================
newSyntheticShard
================
*/
func newSyntheticShard(tb testing.TB) *syntheticShard {
	tb.Helper()
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	chars := make([]*enterworld.Character, 0, tickBudgetPlayers)
	for i := 0; i < tickBudgetPlayers; i++ {
		chars = append(chars, syntheticPlayer(i))
	}
	ref := monster.MonsterRef{
		RefObjID: 1933, TidWord: 0x00c6, Codename: "MOB_CH_MANGNYANG", Level: 20,
		MaxHP: 100000, BodyRadius: 6, ExpToGive: 24, RewardActionPinned: true, CombatPinned: true,
		PhysicalDefense: 7, MagicalDefense: 10, ParryRate: 1, MagicalParry: 1, EvasionRate: 27, HitRate: 27, CriticalRate: 2,
	}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{ref.RefObjID: ref},
		[]monster.NestRow{{
			SpawnPoint:     monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: 0x62A8, X: 963, Y: 20, Z: 458},
			RetailEvidence: true, MaxCount: 120,
		}},
	))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: chars},
		Items:      testItems(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, monsters)
	rt.Now = clock.Now
	rt.BerserkRoll = func() (uint32, error) { return 9999, nil }
	rt.WearRoll = func() (uint32, error) { return 99, nil }
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	rt.AttackPetExpansion = true
	for _, c := range chars {
		rt.BindPetSession(testDivision, c, 1)
		rt.BindRecoverySession(testDivision, c, 1)
	}
	for i := 0; i < tickBudgetGroundDrops; i++ {
		rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 3117, StackCount: 1, MagicOptions: []uint64{uint64(i)}, DroppedBy: "hunter"})
	}
	return &syntheticShard{rt: rt, clock: clock, hook: rt.TickHook(), roster: simulation.DefaultNpcRoster(), anchor: simulation.NpcShopSpawn()}
}

/*
================
syntheticPlayer

One online player: a 48-row inventory of equipment and stacks, full
vitals, and a summoned raven riding as the active COS.
================
*/
func syntheticPlayer(i int) *enterworld.Character {
	c := testCharacter()
	c.ID = int64(100 + i)
	c.Name = fmt.Sprintf("shard%02d", i)
	rows := make([]enterworld.InventoryRow, 0, tickBudgetInventoryRow)
	for slot := 0; slot < tickBudgetInventoryRow; slot++ {
		row := enterworld.InventoryRow{Slot: int64(slot), StackCount: 1}
		switch slot % 3 {
		case 0:
			row.RefObjID, row.Codename = 11459, "ITEM_CH_SWORD_01_A_RARE"
			row.TypeFlags = wire.PackTypeFlags(3, 1, 6, 2)
			row.VarianceBits = "9223372036854775808"
			row.Durability = 96
			row.MagicOptions = []uint64{uint64(slot), uint64(slot + 1)}
		case 1:
			row.RefObjID, row.Codename = 3630, "ITEM_ETC_HP_POTION_01"
			row.TypeFlags = wire.PackTypeFlags(3, 3, 1, 1)
			row.StackCount = 50
		default:
			row.RefObjID, row.Codename = 62, "ITEM_ETC_GOLD_02"
			row.TypeFlags = wire.PackTypeFlags(3, 3, 5, 2)
			row.StackCount = 1000
		}
		rows = append(rows, row)
	}
	c.MissionInventory = rows
	spawnX, spawnY, spawnZ := float64(940+i*4), float64(20), float64(458)
	c.World = &domain.CharacterWorld{Spawn: &domain.WorldSpawn{RegionID: testInt64(0x62A8), X: &spawnX, Y: &spawnY, Z: &spawnZ}}
	c.ActiveCOS = &domain.CharacterCOS{GID: uint32(5000 + i), Codename: "COS_P_RAVEN_040", Level: 90, Summoned: true}
	if c.CurrentHP != nil {
		*c.CurrentHP /= 2
	}
	return c
}

/*
================
tick

One full tick: the action runtime's whole step list, the NPC patrol
frames, and one ground snapshot on the commit cadence.
================
*/
func (s *syntheticShard) tick(tick int64) {
	s.clock.Advance(100 * time.Millisecond)
	now := s.clock.NowMs()
	_ = s.hook(now)
	_ = simulation.NpcMoveFrames(s.roster, s.anchor, tick)
	_ = s.rt.Ground.Snapshot()
}

/*
================
BenchmarkTickSyntheticShard

Reports ns, bytes and allocations per tick of the synthetic shard.
================
*/
func BenchmarkTickSyntheticShard(b *testing.B) {
	s := newSyntheticShard(b)
	for i := int64(0); i < tickBudgetWarmupTicks; i++ {
		s.tick(i)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.tick(int64(b.N) + int64(i))
	}
}

/*
================
TestTickAllocationBudget

The guard: a warmed synthetic shard must tick inside the pinned byte
and allocation budgets. Failure here means a per-tick copy crept back;
lower the constants only with a measured after, never raise them.
================
*/
func TestTickAllocationBudget(t *testing.T) {
	if raceDetectorOn {
		t.Skip("the race detector's instrumentation allocates on its own; the budget pins the plain build")
	}
	s := newSyntheticShard(t)
	for i := int64(0); i < tickBudgetWarmupTicks; i++ {
		s.tick(i)
	}
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := int64(0); i < tickBudgetMeasureTicks; i++ {
		s.tick(tickBudgetWarmupTicks + i)
	}
	runtime.ReadMemStats(&after)
	bytesPerTick := float64(after.TotalAlloc-before.TotalAlloc) / tickBudgetMeasureTicks
	allocsPerTick := float64(after.Mallocs-before.Mallocs) / tickBudgetMeasureTicks
	t.Logf("synthetic tick: %.0f bytes/tick, %.0f allocs/tick over %d ticks", bytesPerTick, allocsPerTick, tickBudgetMeasureTicks)
	if bytesPerTick > tickBudgetBytesPerTick {
		t.Fatalf("tick allocated %.0f bytes, budget %d (lower the budget only with a measured fix)", bytesPerTick, tickBudgetBytesPerTick)
	}
	if allocsPerTick > tickBudgetAllocsPerTick {
		t.Fatalf("tick made %.0f allocations, budget %d (lower the budget only with a measured fix)", allocsPerTick, tickBudgetAllocsPerTick)
	}
}
