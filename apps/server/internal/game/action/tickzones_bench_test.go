/*
===========================================================================

tickzones_bench_test.go - the four skill tick zones at 100 fighting players (#3101)

Each benchmark builds 100 players in one division, every one fighting
its own monster, and advances one zone in the steady waiting state: the
attack intent stands in reach with its next swing far away, the
prepared cast waits for its release time, the combat trap waits for its
next scan, the ground viewer already shows every drop in range. That is
the state a tick must find free; the due-time work is pinned by the
behaviour tests, not here.

===========================================================================
*/

package action

import (
	"fmt"
	"math"
	"sort"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/game/world/skillobject"
	"opensro.online/server/internal/transport"
)

// tickZonePlayers is the synthetic fighting population every zone is
// pinned at.
const tickZonePlayers = 100

// tickZoneWaitMs is how far away the next due time sits: at a 10 ms
// tick the waiting work must not notice the wait at all.
const tickZoneWaitMs = 600_000

/*
================
tickZoneFight

One built world of tickZonePlayers fighters and the handles a zone
benchmark drives.
================
*/
type tickZoneFight struct {
	rt         *Runtime
	clock      *fakeClock
	characters []*enterworld.Character
	targets    []monster.Instance
	now        int64
}

/*
================
newTickZoneFight

100 characters in one division, each three units from its own monster,
admitted to the world and seeded at its spawn.
================
*/
func newTickZoneFight(b testing.TB) *tickZoneFight {
	b.Helper()
	const region = uint16(0x62A8)
	clock := &fakeClock{now: time.UnixMilli(1_000_000)}
	now := clock.NowMs()

	ref := monster.MonsterRef{
		RefObjID: 1933, TidWord: 0x00c6, Codename: "MOB_CH_MANGNYANG", Level: 1,
		MaxHP: 100000, BodyRadius: 6, ExpToGive: 24,
		RewardActionPinned: true, CombatPinned: true,
		PhysicalDefense: 7, MagicalDefense: 10, ParryRate: 1, MagicalParry: 1,
		EvasionRate: 27, HitRate: 27, CriticalRate: 2,
	}
	sword := &enterworld.ItemRef{
		RefObjID: 71, Codename: "ITEM_CH_SWORD_01_A", TypeIDs: [4]int64{3, 1, 6, 2},
		Combat: &enterworld.ItemCombatRef{
			ActionRange: 6,
			PhysicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 15, Max: 16, PerPlus: 2.4},
				Maximum: enterworld.ItemStatRange{Min: 16, Max: 18, PerPlus: 2.4},
			},
			MagicalAttack: enterworld.ItemAttackRange{
				Minimum: enterworld.ItemStatRange{Min: 25, Max: 26, PerPlus: 4.1},
				Maximum: enterworld.ItemStatRange{Min: 28, Max: 31, PerPlus: 4.1},
			},
			HitRate:      enterworld.ItemStatRange{Min: 24, Max: 30},
			CriticalRate: enterworld.ItemStatRange{Min: 3, Max: 15},
		},
	}

	var nests []monster.NestRow
	var characters []*enterworld.Character
	for i := 0; i < tickZonePlayers; i++ {
		x := 960 + float64(i)*64
		nests = append(nests, monster.NestRow{
			SpawnPoint:     monster.SpawnPoint{RefObjID: ref.RefObjID, RegionID: region, X: x + 3, Y: 20, Z: 458},
			RetailEvidence: true, MaxCount: 1,
		})
		character := &enterworld.Character{
			ID:            int64(1000 + i),
			Name:          fmt.Sprintf("P%03d", i),
			ModelCodename: "CHAR_CH_MAN_ADVENTURER",
			RaceIndex:     testInt64(enterworld.RaceChina),
			Level:         testInt64(1),
			MaxLevel:      testInt64(1),
			CurrentHP:     testInt64(100),
			CurrentMP:     testInt64(100),
			Strength:      testInt64(20),
			Intellect:     testInt64(20),
			Masteries:     []enterworld.CharacterMastery{{ID: 257, Level: 1}},
			Skills:        []uint32{2},
			BattleUntilMs: math.MaxInt64,
			MissionInventory: []enterworld.InventoryRow{{
				Slot: 6, RefObjID: sword.RefObjID, Codename: sword.Codename,
				TypeFlags: sword.TypeFlags(), VarianceBits: "0", Durability: 76, StackCount: 1,
			}},
			World: &enterworld.CharacterWorld{
				Spawn: &enterworld.WorldSpawn{
					RegionID: testInt64(int64(region)),
					X:        func() *float64 { value := x; return &value }(),
					Y:        func() *float64 { value := 20.0; return &value }(),
					Z:        func() *float64 { value := 458.0; return &value }(),
					Angle:    testInt64(0),
				},
				SpawnSet: true,
			},
		}
		characters = append(characters, character)
	}

	monsters := simulation.NewMonsterState(monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nests))
	monsters.SetTimeSource(clock.Now)
	monsters.StartDivision(testDivision)
	monsters.AdvancePopulation(monsters.CurrentTimeMillis())
	targets := monsters.InstancesInRegions(testDivision, []uint16{region})
	if len(targets) != tickZonePlayers {
		b.Fatalf("fight fixture materialized %d monsters, want %d", len(targets), tickZonePlayers)
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Spawn.X < targets[j].Spawn.X })

	deps := &enterworld.Deps{
		Characters: enterworld.StaticCharacterSource{testDivision: characters},
		Roster: &enterworld.Roster{
			Format: "sro-server-character-authority", Version: 3,
			Models: []enterworld.RosterModel{{
				Codename: "CHAR_CH_MAN_ADVENTURER", RefObjID: 1907, BodyRadius: 4, Knockdown: 3, KORecoverMs: 3000,
			}},
		},
		Items:        staticItemSource{sword.Codename: sword},
		Levels:       testCombatRewardLevels(),
		MagicOptions: &emptyMagicOptionSource{},
		Skills: staticSkillSource{2: {
			ID: 2, Codename: "SKILL_CH_SWORD_BASE_01", TargetRequired: true,
			ActionCastingTimeMs: 0, ActionCastingTimePinned: true,
			ActionDurationMs: uint32(testBasicAttackActionDuration.Milliseconds()), ActionDurationPinned: true,
			CoolTimeMs: 1000, TimingPinned: true, AIWeight: 100,
			ActionRange: 6, ActionRangePinned: true,
			RequiredWeaponKinds: [2]uint8{2, 3},
			Attack:              enterworld.SkillAttack{Present: true, Flags: 5, Percent: 60, Value5: 60, ImpactCount: 2},
			Consumption:         enterworld.SkillConsumption{MP: 5, Pinned: true},
			Group:               1,
			CombatPinned:        true,
		}},
	}
	rt := NewRuntime(deps, monsters)
	rt.BerserkRoll = func() (uint32, error) { return 9999, nil }
	rt.WearRoll = func() (uint32, error) { return 99, nil }
	rt.CombatRoll = func() (uint32, error) { return 100, nil }
	rt.Now = clock.Now

	for _, character := range characters {
		if err := rt.AdmitCharacterSession(testDivision, character.Name, uint64(character.ID)); err != nil {
			b.Fatalf("admit %s: %v", character.Name, err)
		}
		rt.Worlds.Update(simulation.WorldKey(testDivision, character.Name),
			func() simulation.WorldState { return simulation.SeedWorldState(character) },
			func(w *simulation.WorldState) { w.SpawnSet = true })
	}
	return &tickZoneFight{rt: rt, clock: clock, characters: characters, targets: targets, now: now}
}

/*
================
BenchmarkAdvanceBasicAttackIntents100

100 intents in reach, each waiting out a far-away next swing.
================
*/
func BenchmarkAdvanceBasicAttackIntents100(b *testing.B) {
	fight := newTickZoneFight(b)
	rt := fight.rt
	for i, character := range fight.characters {
		rt.setCombatIntent(basicAttackIntent{
			DivisionID: testDivision, CharacterName: character.Name,
			TargetGid: fight.targets[i].Gid, SkillID: 2,
			CooldownMs: 1000, NextActionMs: fight.now + tickZoneWaitMs,
		})
	}
	openActionOwners := map[string]bool{}
	rt.advanceBasicAttackIntents(fight.now, openActionOwners)
	if got := len(rt.basicAttackSnapshot); got != tickZonePlayers {
		b.Fatalf("warmup left %d intents, want %d", got, tickZonePlayers)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		rt.advanceBasicAttackIntents(fight.now, openActionOwners)
	}
}

/*
================
BenchmarkAdvanceProjectileCasts100

100 prepared casts waiting for a far-away release time.
================
*/
func BenchmarkAdvanceProjectileCasts100(b *testing.B) {
	fight := newTickZoneFight(b)
	rt := fight.rt
	for i, character := range fight.characters {
		cast := newPendingProjectileCast(testDivision, character.Name)
		cast.characterID = character.ID
		cast.cast = wire.SkillAction{ActionId: 2, HasTarget: true, TargetGid: fight.targets[i].Gid}
		cast.token = uint32(5000 + i)
		cast.releaseAtMs = fight.now + tickZoneWaitMs
		cast.verdictCost = enterworld.SkillConsumption{MP: 5, Pinned: true}
		cast.verdictGroup = 1
		rt.pendingProjectileCasts = append(rt.pendingProjectileCasts, cast)
	}
	rt.advanceProjectileCasts(fight.now)
	if got := len(rt.pendingProjectileCasts); got != tickZonePlayers {
		b.Fatalf("warmup left %d casts, want %d", got, tickZonePlayers)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		rt.advanceProjectileCasts(fight.now)
	}
}

/*
================
BenchmarkAdvanceSkillObjects100

100 combat traps waiting for a far-away next scan, 100 viewers already
showing them.
================
*/
func BenchmarkAdvanceSkillObjects100(b *testing.B) {
	fight := newTickZoneFight(b)
	rt := fight.rt
	var sessions []simulation.SessionView
	for i, character := range fight.characters {
		lease, admitted := rt.EntryPopulationLease(testDivision, character.Name)
		if !admitted {
			b.Fatalf("%s holds no entry lease", character.Name)
		}
		object, err := rt.SkillObjects.Create(skillobject.Object{
			Division: testDivision, Population: lease,
			OwnerGID: enterworld.ObjectIDForCharacter(character), OwnerName: character.Name,
			Program: skillobject.Program{
				SkillID: 2, DurationMs: tickZoneWaitMs, ScanMs: 60_000, Radius: 10, Combat: true,
			},
			Spawn: wire.SkillObjectSpawn{
				Region: uint16(*character.World.Spawn.RegionID),
				X:      float32(*character.World.Spawn.X), Y: 20, Z: 458,
			},
			CreatedMs: fight.now,
		})
		if err != nil {
			b.Fatalf("plant trap for %s: %v", character.Name, err)
		}
		sessions = append(sessions, simulation.SessionView{
			SessionID: fmt.Sprintf("s%03d", i), DivisionID: testDivision,
			CharacterID: character.ID, WorldInstance: domain.DefaultWorldInstance,
			Population: lease, PublishedObjects: []uint32{object.Spawn.GID},
			World: simulation.SeedWorldState(character),
		})
	}
	// A settled viewer already shows every trap in its range, as
	// transport admission leaves it: the published set is the whole
	// visible set, not only the viewer's own trap.
	objects := rt.SkillObjects.Snapshot()
	for i := range sessions {
		pose := sessions[i].World.LiveSpawnAt(fight.now)
		viewer := skillobject.Viewer{Division: testDivision, Population: sessions[i].Population,
			Position: worldgeom.RegionXZ{RegionID: pose.RegionID, X: pose.X, Z: pose.Z}}
		published := make([]uint32, 0, len(objects))
		for _, object := range objects {
			if skillobject.Visible(object, viewer) {
				published = append(published, object.Spawn.GID)
			}
		}
		sessions[i].PublishedObjects = published
	}
	rt.AdvanceSkillObjects(fight.now, sessions)
	if got := len(rt.SkillObjects.Snapshot()); got != tickZonePlayers {
		b.Fatalf("warmup left %d objects, want %d", got, tickZonePlayers)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		rt.AdvanceSkillObjects(fight.now, sessions)
	}
}

/*
================
BenchmarkAdvanceGroundScope100

100 viewers, each standing in 20 fresh drops it already shows.
================
*/
func BenchmarkAdvanceGroundScope100(b *testing.B) {
	fight := newTickZoneFight(b)
	rt := fight.rt
	gold := GoldHeapRef{RefObjID: 62, Codename: "ITEM_ETC_GOLD_02", Tid1: 3, Tid2: 3, Tid3: 5, Tid4: 2}
	for _, character := range fight.characters {
		at := simulation.SeedWorldState(character).Spawn
		for j := 0; j < 20; j++ {
			drop := at
			drop.X += float64(j%5)*10 - 20
			drop.Z += float64(j/5)*10 - 15
			rt.Ground.Add(testDivision, PlanGoldDrop(gold, 100, drop, "someone", fight.clock.Now()))
		}
	}
	buckets := rt.groundDropBuckets(testDivision, grounditem.Population{})
	var sessions []simulation.SessionView
	for i, character := range fight.characters {
		world := simulation.SeedWorldState(character)
		spawn, _ := groundScope(buckets, viewerBlock(world.LiveSpawnAt(fight.now)), nil, make(map[uint32]struct{}))
		published := make([]uint32, 0, len(spawn))
		for _, item := range spawn {
			published = append(published, item.Gid)
		}
		if len(published) == 0 {
			b.Fatalf("viewer %s starts with no drops in range", character.Name)
		}
		sessions = append(sessions, simulation.SessionView{
			SessionID: fmt.Sprintf("s%03d", i), DivisionID: testDivision,
			CharacterID: character.ID, WorldInstance: domain.DefaultWorldInstance,
			PublishedObjects: published, World: world,
		})
	}
	scene := func(string) (*transport.Session, uint64, bool) { return nil, 0, false }
	rt.AdvanceGroundScope(fight.now, sessions, scene)
	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		rt.AdvanceGroundScope(fight.now, sessions, scene)
	}
}
