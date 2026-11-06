/*
===========================================================================

projectilecast_bench_test.go - what a waiting projectile cast costs per tick

#2139 item 2: a prepared projectile cast that is not due should cost
nothing while it waits. The bench stands prepared casts in flight on
one caster against a live monster, none of them due; every measured
tick re-judges every wait and must emit nothing and retire nothing.
The caster carries a loaded bag (spare ammunition, stored companions),
so the record copies a wait takes price like the live ones. Run:

	go test -run xxx -bench WaitingProjectile -benchmem ./internal/game/action/

===========================================================================
*/
package action

import (
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// benchWaitingSkillID is the synthetic offense the waiting casts carry:
// pinned consumption, so the wait verdict runs its resource check.
const benchWaitingSkillID = 424242

/*
================
waitingProjectileBenchRuntime

One caster beside one live monster, with count prepared projectile
casts in flight and none of them due. The caster's bag is loaded the
way loadCombatBag loads it, plus two more stored companions: a settled
caster's copies carry its stored pets.
================
*/
func waitingProjectileBenchRuntime(b *testing.B, count int) (*Runtime, int64) {
	b.Helper()
	const region = uint16(0x62A8)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}

	ref := monster.MonsterRef{
		RefObjID:   1933,
		TidWord:    0x00c6, // Authored monster class; actor-class gates must run in fixtures too.
		Codename:   "MOB_CH_MANGNYANG",
		Level:      1,
		MaxHP:      100000,
		BodyRadius: 6,
		ExpToGive:  24,

		RewardActionPinned: true,

		CombatPinned:    true,
		PhysicalDefense: 7,
		MagicalDefense:  10,
		ParryRate:       1,
		MagicalParry:    1,
		EvasionRate:     27,
		HitRate:         27,
		CriticalRate:    2,
	}
	monsters := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{ref.RefObjID: ref},
		[]monster.NestRow{{
			SpawnPoint: monster.SpawnPoint{
				RefObjID: ref.RefObjID,
				RegionID: region,
				X:        963,
				Y:        20,
				Z:        458,
			},
			RetailEvidence: true,
			MaxCount:       1,
		}},
	))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	monsters.AdvancePopulation(monsters.CurrentTimeMillis())
	instances := monsters.InstancesInRegions(testDivision, []uint16{region})
	if len(instances) != 1 {
		b.Fatalf("bench materialized %d monsters, want one", len(instances))
	}

	sword := &enterworld.ItemRef{
		RefObjID: 71,
		Codename: "ITEM_CH_SWORD_01_A",
		TypeIDs:  [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{
			ActionRange: 6,
			PhysicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4000001},
				Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4000001},
			},
			MagicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.0999999},
				Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.0999999},
			},
			HitRate:      enterworld.ItemStatRange{Min: 24, Max: 30},
			CriticalRate: enterworld.ItemStatRange{Min: 3, Max: 15},
		},
	}
	character := &enterworld.Character{
		ID:            3,
		Name:          "asd2",
		ModelCodename: "CHAR_CH_MAN_ADVENTURER",
		RaceIndex:     testInt64(enterworld.RaceChina),
		Level:         testInt64(1),
		MaxLevel:      testInt64(1),
		CurrentHP:     testInt64(100),
		CurrentMP:     testInt64(100000),
		Strength:      testInt64(20),
		Intellect:     testInt64(20),
		Masteries:     []enterworld.CharacterMastery{{ID: 257, Level: 1}},
		Skills:        []uint32{benchWaitingSkillID},
		MissionInventory: []enterworld.InventoryRow{{
			Slot:         6,
			RefObjID:     sword.RefObjID,
			Codename:     sword.Codename,
			TypeFlags:    sword.TypeFlags(),
			VarianceBits: "0",
			Durability:   76,
			StackCount:   1,
		}},
		World: &enterworld.CharacterWorld{
			Spawn: &enterworld.WorldSpawn{
				RegionID: testInt64(int64(region)),
				X:        testFloat64(960),
				Y:        testFloat64(20),
				Z:        testFloat64(458),
				Angle:    testInt64(0),
			},
			SpawnSet: true,
		},
	}
	// Fixtures fight already in battle, as in the combat fixture.
	character.BattleUntilMs = math.MaxInt64
	loadCombatBag(character)
	stored := &domain.CharacterCOS{GID: 0x7F00_0002, RefObjID: 3914, Codename: "COS_P_RAVEN", CurrentHP: 100}
	character.MissionInventory = append(character.MissionInventory,
		enterworld.InventoryRow{Slot: 17, RefObjID: 3913, Codename: "ITEM_COS_P_RAVEN", StackCount: 1, Summon: stored})
	stored = &domain.CharacterCOS{GID: 0x7F00_0003, RefObjID: 3914, Codename: "COS_P_RAVEN", CurrentHP: 100}
	character.MissionInventory = append(character.MissionInventory,
		enterworld.InventoryRow{Slot: 18, RefObjID: 3913, Codename: "ITEM_COS_P_RAVEN", StackCount: 1, Summon: stored})

	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: {character}},
		Roster: &enterworld.Roster{
			Format: "sro-server-character-authority", Version: 3,
			Models: []enterworld.RosterModel{{
				Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000,
			}},
		},
		Items:  staticItemSource{sword.Codename: sword},
		Levels: testCombatRewardLevels(),
		// An identity-carrying magic-option source, as in production:
		// without one the stats memo never engages and every wait
		// would be judged the cold way.
		MagicOptions: &emptyMagicOptionSource{},
		Skills: staticSkillSource{benchWaitingSkillID: {
			ID:             benchWaitingSkillID,
			Codename:       "SKILL_BENCH_WAITING",
			TargetRequired: true,
			Group:          4242,
			Consumption:    enterworld.SkillConsumption{Pinned: true, MP: 21},
		}},
	}
	rt := NewRuntime(deps, monsters)
	rt.Now = clock.Now
	// Keep the bench on the noncritical branch, as in the combat fixture.
	rt.CombatRoll = func() (uint32, error) { return 100, nil }

	now := clock.NowMs()
	for i := 0; i < count; i++ {
		p := newPendingProjectileCast(testDivision, character.Name)
		p.token = uint32(10_000 + i)
		p.characterID = character.ID
		p.cast = wire.SkillAction{ActionId: benchWaitingSkillID, HasTarget: true, TargetGid: instances[0].Gid}
		// The production shape: prepare prices the cast (projectilecast.go).
		p.verdictCost = enterworld.SkillConsumption{Pinned: true, MP: 21}
		p.verdictGroup = 4242
		p.releaseAtMs = now + 3_600_000
		rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, p)
	}
	return rt, now
}

/*
================
BenchmarkWaitingProjectileCasts

Forty prepared casts wait on a live target: the per-tick advance must
emit nothing and retire nothing. ns/op covers all forty waits.
================
*/
func BenchmarkWaitingProjectileCasts(b *testing.B) {
	rt, now := waitingProjectileBenchRuntime(b, 40)
	if out := rt.advanceProjectileCasts(now); len(out) != 0 {
		b.Fatalf("warmup tick produced %d division frames", len(out))
	}
	if len(rt.pendingProjectileCasts) != 40 {
		b.Fatalf("warmup retired casts: %d queued", len(rt.pendingProjectileCasts))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := rt.advanceProjectileCasts(now); len(out) != 0 {
			b.Fatalf("waiting casts produced frames: %+v", out)
		}
	}
}

/*
================
BenchmarkWaitingProjectileCast

The single-cast control: one prepared cast waiting on a live target.
================
*/
func BenchmarkWaitingProjectileCast(b *testing.B) {
	rt, now := waitingProjectileBenchRuntime(b, 1)
	if out := rt.advanceProjectileCasts(now); len(out) != 0 {
		b.Fatalf("warmup tick produced %d division frames", len(out))
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if out := rt.advanceProjectileCasts(now); len(out) != 0 {
			b.Fatalf("waiting cast produced frames: %+v", out)
		}
	}
}

// dueProjectileSink keeps the compiler from discarding a measured release.
var dueProjectileSink []simulation.DivisionFrames

/*
================
BenchmarkDueProjectileCast

One prepared bow cast is due against a live monster: the measured call
is the production release lane (advanceProjectileCasts), from the queue
verdict through acceptSkillStagePhaseAt and the damage commit. The
target carries enough HP that no measured release can kill it, and each
iteration re-arms exactly one cast and restores the MP and arrow stack
the previous release spent.
================
*/
func BenchmarkDueProjectileCast(b *testing.B) {
	rt, c, target, skill, now := arrowFixtureHP(b, 1_000_000_000)
	cast := wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target}
	cost, err := rt.preparedExecutionMPCost(testDivision, c, skill)
	if err != nil {
		b.Fatalf("price due cast: %v", err)
	}
	prepared := newPendingProjectileCast(testDivision, c.Name)
	prepared.executionCost = skillCharge{mp: cost, hp: rt.preparedExecutionHPCost(testDivision, c, skill)}
	prepared.verdictCost, prepared.verdictGroup = skill.Consumption, skill.Group
	prepared.characterID = c.ID
	prepared.cast = cast
	prepared.releaseAtMs = now
	queue := make([]pendingProjectileCast, 0, 1)
	seed := func(token uint32) {
		*c.CurrentMP = 100
		c.MissionInventory[1].StackCount = 250
		pending := prepared
		pending.token = token
		rt.pendingProjectileCasts = append(queue[:0], pending)
	}
	before, found := rt.Monsters.Get(testDivision, target)
	if !found {
		b.Fatal("due-cast target vanished before warmup")
	}
	seed(10_000)
	if out := rt.advanceProjectileCasts(now); len(out) != 2 {
		b.Fatalf("warmup release produced %d division frames, want 2", len(out))
	}
	after, found := rt.Monsters.Get(testDivision, target)
	if !found || after.CurrentHP >= before.CurrentHP {
		b.Fatalf("warmup release dealt no damage: before %+v after %+v", before, after)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		seed(uint32(10_001 + i))
		dueProjectileSink = rt.advanceProjectileCasts(now)
	}
}
