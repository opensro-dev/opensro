/*
===========================================================================

tickbudget_test.go - the synthetic-shard tick allocation guard (#1943)

One benchmarked tick of a synthetic shard: players with realistic
inventories and attack pets out, two crow broods laid (one hatching
mid-window) and a periodic pulse riding on one raven, a monster
population advancing, NPC patrol frames building, and a ground plane
snapshotting. The guard test
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
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
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
// 2,962 allocs; with crow broods and a periodic COS pulse seeded into
// the shard (a heavier workload, not a regression) 1,642,000 B /
// 3,588 allocs; with the pet-periodic admissions (the pet tick, the
// persistent-effect pulse, the crow recipient walk and the COS/monster
// source capture) on that same seeded shard, measured 1,019,000 B /
// 2,719 allocs; with the test-only refusal audit out of the measured
// window (it never runs in production, and its two encodes per door were
// 58% of the counted bytes), measured 422,144 B / 2,541 allocs; with a
// brood-free crow taking no owner copy on its tick, measured 275,466 B /
// 2,099 allocs; with crows lacking Tailwind or Guiding Feathers refused
// before the owner copy on those ticks, measured 208,183 B / 1,862 allocs;
// with the commit's ground read taking only the changed rows instead of
// a whole-plane snapshot (#2040), measured 142,251 B / 1,457 allocs;
// with the pet tick (#2046) and then the brood tick taking the companion
// copy instead of the whole owner record (#2030), measured 98,010 B /
// 1,380 allocs.
const (
	tickBudgetBytesPerTick  = 107800
	tickBudgetAllocsPerTick = 1518
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
		Items:      syntheticShardItems(),
		Skills:     syntheticShardSkills(),
		NpcSpawns:  enterworld.NpcSpawnConfig{Roster: simulation.DefaultNpcRoster()},
	}
	rt := NewRuntime(deps, monsters)
	rt.Now = clock.Now
	rt.BerserkRoll = func() (uint32, error) { return 9999, nil }
	rt.WearRoll = func() (uint32, error) { return 99, nil }
	rt.CombatRoll = func() (uint32, error) { return 0, nil }
	rt.AttackPetExpansion = true
	placeSeededPlayers(chars)
	for _, c := range chars {
		rt.BindPetSession(testDivision, c, 1)
		rt.BindRecoverySession(testDivision, c, 1)
	}
	for i := 0; i < tickBudgetGroundDrops; i++ {
		rt.Ground.Add(testDivision, grounditem.Item{RefObjID: 3117, StackCount: 1, MagicOptions: []uint64{uint64(i)}, DroppedBy: "hunter"})
	}
	shard := &syntheticShard{rt: rt, clock: clock, hook: rt.TickHook(), roster: simulation.DefaultNpcRoster(), anchor: simulation.NpcShopSpawn()}
	seedSyntheticPets(tb, shard, chars)
	return shard
}

// The seeded COS reference every involved raven resolves through, and
// the vitals that keep it alive under the seeded pulse.
const (
	syntheticRavenRefObjID = 9
	syntheticRavenMaxHP    = 100000
)

// syntheticFieldRegion is the battlefield tile beside the synthetic
// town tile (regioncombat_generated.go): the seeded casts and hits are
// town-gated, so their owners stand here.
const syntheticFieldRegion = 0x62AA

/*
================
placeSeededPlayers

Stand the pulse source, its victim and the two brood owners on the
battlefield tile before their sessions bind: followers settle beside
owners who are already where the tick keeps them.
================
*/
func placeSeededPlayers(chars []*enterworld.Character) {
	for _, i := range []int{0, 1, 2, 3} {
		*chars[i].World.Spawn.RegionID = syntheticFieldRegion
	}
}

/*
================
syntheticShardItems

The harness item source plus the one COS reference the seeded pets
resolve through. Only the seeded ravens carry its RefObjID; the other
thirteen still resolve nothing, exactly as before.
================
*/
func syntheticShardItems() cosTestItemSource {
	return cosTestItemSource{
		staticItemSource: testItems(),
		characters: map[string]*enterworld.CharacterRef{
			"COS_P_RAVEN_040": {
				RefObjID: syntheticRavenRefObjID, Codename: "COS_P_RAVEN_040",
				TidWord: attackPetBand<<11 | 0x1c6, Level: 90,
				MaxHP: syntheticRavenMaxHP, MaxMP: 600, WalkSpeed: 20, RunSpeed: 80,
				Parameters: monster.MonsterRef{CombatPinned: true, BodyRadius: 10, HitRate: 10},
			},
		},
	}
}

/*
================
syntheticShardSkills

The one skill row the seeded periodic pulse casts: a linked attack on
a two-second period that outlives the measured window.
================
*/
func syntheticShardSkills() staticSkillSource {
	return staticSkillSource{testPeriodicSkillID: {
		ID: testPeriodicSkillID, Group: 900, Codename: "TEST_LINKED_ATTACK",
		ActionDurationMs: 500, EffectRider: true,
		Consumption: enterworld.SkillConsumption{Pinned: true, MP: 5},
		TimedEffect: enterworld.SkillTimedEffect{Periodic: enterworld.SkillPeriodicEffect{
			Pinned: true, DurationMs: 120000, PeriodMs: 2000,
			Link:   enterworld.SkillEffectLink{Present: true, Group: 9, MaxOutgoing: 2},
			Attack: enterworld.SkillAttack{Present: true, Flags: 8, Percent: 37, Min: 4, Max: 6, ImpactCount: 1}}},
	}}
}

/*
================
seedSyntheticPets

Two behaviours the plain roster never reaches. Two crows lay real
brood eggs through the real cast - one rides the window out as an
egg, the other was laid 22 s before it and hatches inside - and
shard00's periodic effect rides on shard01's raven, pulsing every two
seconds. Everything is installed the way production installs it, and
a cast or install that refuses fails the fixture loudly here instead
of silently measuring nothing.
================
*/
func seedSyntheticPets(tb testing.TB, s *syntheticShard, chars []*enterworld.Character) {
	tb.Helper()
	// The pulse caster's stats must project. The synthetic band wears
	// a sword codename at five sockets, which is not one weapon, and
	// the harness wires no magic-option source: the caster keeps only
	// its slot-6 sword, options stripped. Every other player keeps
	// the rows (and the refusals) it has today.
	kept := make([]enterworld.InventoryRow, 0, len(chars[0].MissionInventory))
	for _, row := range chars[0].MissionInventory {
		if row.Slot >= 0 && row.Slot < int64(inventory.EquipmentSlotEnd) && row.Codename == "ITEM_CH_SWORD_01_A_RARE" {
			if row.Slot != 6 {
				continue
			}
			row.MagicOptions = nil
		}
		kept = append(kept, row)
	}
	chars[0].MissionInventory = kept
	// The involved ravens are alive and resolvable before any tick
	// runs: a dead pet loses its session to the first advance.
	for _, i := range []int{1, 2, 3} {
		chars[i].ActiveCOS.RefObjID = syntheticRavenRefObjID
		chars[i].ActiveCOS.CurrentHP = syntheticRavenMaxHP
		chars[i].ActiveCOS.CurrentMP = 600
	}
	// Settle the followers where the tick keeps them before the cast's
	// adjacency gate looks.
	for range 5 {
		s.clock.Advance(100 * time.Millisecond)
		_ = s.hook(s.clock.NowMs())
	}
	now := s.clock.NowMs()
	for _, i := range []int{2, 3} {
		c := chars[i]
		skill := learnPetBook(tb, c, 49)
		s.rt.castCrowBrood(testDivision, c, c.ActiveCOS.GID, skill, now)
		if len(c.Brood) != 1 {
			tb.Fatalf("synthetic crow %d laid no egg", i)
		}
	}
	// The second egg was laid 22 s before the window: it hatches
	// inside the measured ticks and the clone rides them out.
	chars[3].Brood[0].BroodUntil = now + 8000
	effect := linkedpulse.Effect{
		Division: testDivision, SourceName: chars[0].Name,
		SourceGID: enterworld.ObjectIDForCharacter(chars[0]), TargetGID: chars[1].ActiveCOS.GID,
		SourceToken: 0xB00001, TargetToken: 0xB00002,
		SkillID: testPeriodicSkillID, LinkGroup: 9, MaxPerTarget: 2,
		StartedMs: now, DurationMs: 120000, PeriodMs: 2000,
	}
	if code := s.rt.periodicEffects.Install(effect); code != 0 {
		tb.Fatalf("synthetic pulse refused: %d", code)
	}
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
frames, and the commit door's ground read on the commit cadence: the
changed rows and, once durable, their acknowledgement (store
ground_changes.go, #2040).
================
*/
func (s *syntheticShard) tick(tick int64) {
	s.clock.Advance(100 * time.Millisecond)
	now := s.clock.NowMs()
	_ = s.hook(now)
	_ = simulation.NpcMoveFrames(s.roster, s.anchor, tick)
	changes := s.rt.Ground.Changes()
	s.rt.Ground.Acknowledge(changes.Revision)
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
	// The refusal audit runs through warm-up and is off while counted: it
	// never runs in production and would dominate the measurement.
	restore := enterworld.SuspendRefusalAudit(b)
	defer restore()
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
	// The refusal audit runs through warm-up and after the window; it is
	// off only while counted, as it never runs in production.
	restore := enterworld.SuspendRefusalAudit(t)
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := int64(0); i < tickBudgetMeasureTicks; i++ {
		s.tick(tickBudgetWarmupTicks + i)
	}
	runtime.ReadMemStats(&after)
	restore()
	// One audited tick after the window: the doors still pass the audit.
	s.tick(tickBudgetWarmupTicks + tickBudgetMeasureTicks)
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

/*
================
TestSyntheticShardSeedsReachTheTick

The seeded behaviours must actually fire inside the measured window:
the early egg hatches, and the periodic pulse takes HP off the victim
raven. A seed that silently refused would leave the guard measuring
the plain roster while claiming otherwise.
================
*/
func TestSyntheticShardSeedsReachTheTick(t *testing.T) {
	s := newSyntheticShard(t)
	for i := int64(0); i < 100; i++ {
		s.tick(i)
	}
	victim := s.rt.findCharacter(testDivision, "shard01")
	egglayer := s.rt.findCharacter(testDivision, "shard02")
	hatcher := s.rt.findCharacter(testDivision, "shard03")
	if victim == nil || egglayer == nil || hatcher == nil {
		t.Fatal("seeded characters vanished")
	}
	if len(egglayer.Brood) != 1 || egglayer.Brood[0].BroodStage != 1 {
		t.Fatalf("riding egg = %+v", egglayer.Brood)
	}
	if len(hatcher.Brood) != 1 || hatcher.Brood[0].BroodStage != 2 {
		t.Fatalf("hatched brood = %+v", hatcher.Brood)
	}
	if victim.ActiveCOS.CurrentHP >= syntheticRavenMaxHP {
		t.Fatalf("pulse never landed: victim raven HP %d", victim.ActiveCOS.CurrentHP)
	}
}
