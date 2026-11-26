/*
===========================================================================

questmonster_test.go - scripted guardian population and lifecycle boundaries

Quest actors use the normal spawn-ground and population owners. Failed or
stale admission must not allocate identities or leak actors into another world.

===========================================================================
*/
package simulation

import (
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
TestQuestGuardianUsesExactPopulationAndDoesNotRespawn
================
*/
func TestQuestGuardianUsesExactPopulationAndDoesNotRespawn(t *testing.T) {
	const code = "MOB_QT_02_PUNISHER_CLON"
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: code, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
	}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("quest")
	lease, exists := state.PopulationLease("quest", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	request := QuestMonsterSpawn{Division: "quest", Population: lease, Codename: code,
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000, RadiusSpan: 20}
	stale := request
	stale.Population.Generation++
	if state.SpawnQuestMonster(stale) {
		t.Fatal("stale generation spawned a guardian")
	}
	if !state.SpawnQuestMonster(request) {
		t.Fatal("valid guardian admission failed")
	}
	actors := state.MaterializedInstances("quest")
	if len(actors) != 1 || !actors[0].NestDetached || actors[0].CurrentHP != 100 || actors[0].Spawn.X != 253 {
		t.Fatal("guardian lost its reference, native pose or detached lifetime", actors)
	}
	if !state.Defeat("quest", actors[0].Gid, time.UnixMilli(1001)) {
		t.Fatal("guardian could not retire through the ordinary population owner")
	}
	state.AdvancePopulation(1000000)
	if len(state.InstancesInRegions("quest", []uint16{0x655e})) != 0 {
		t.Fatal("script guardian acquired a respawning nest")
	}
	request.Position.RegionID |= 0x8000
	if state.SpawnQuestMonster(request) {
		t.Fatal("indoor guardian admitted")
	}
}

/*
================
TestQuestMonsterLandsOnANamedCavePoint

A cave floor refuses the radius sample, but a spawn at its named point
(fixed, no radius) lands there: the temple's Haroeris in his sanctum.
================
*/
func TestQuestMonsterLandsOnANamedCavePoint(t *testing.T) {
	const code = "MOB_SD_HAROERIS"
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: code, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
	}, nil))
	state.SetRandomSource(func() float64 { return 0.5 })
	state.StartDivision("temple")
	lease, exists := state.PopulationLease("temple", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	at := Spawn{RegionID: 0x800E, X: 9919, Y: 100, Z: 24}
	gid, ok := state.SpawnQuestMonsterGID(QuestMonsterSpawn{Division: "temple", Population: lease, Codename: code,
		Position: at, FixedPosition: true, NowMs: 1000})
	if !ok {
		t.Fatal("the named cave point was refused")
	}
	actors := state.MaterializedInstances("temple")
	if len(actors) != 1 || actors[0].Gid != gid || actors[0].Spawn.RegionID != at.RegionID ||
		actors[0].Spawn.X != at.X || actors[0].Spawn.Z != at.Z {
		t.Fatalf("landed at %+v, want %+v", actors, at)
	}
	sampled := QuestMonsterSpawn{Division: "temple", Population: lease, Codename: code,
		Position: at, FixedPosition: true, NowMs: 1000, RadiusSpan: 20}
	if _, ok := state.SpawnQuestMonsterGID(sampled); ok {
		t.Fatal("a radius sample in a cave was admitted")
	}
}

/*
================
TestQuestSpawnRedrawsAPointInsideAClosedObject

A spawn that asks to avoid closed objects redraws its angle and radius while the
point is inside one; without the ask the first draw stands, as the retail
scripts take it.
================
*/
func TestQuestSpawnRedrawsAPointInsideAClosedObject(t *testing.T) {
	const code = "MOB_QT_CH_CHARGHOST_01"
	land := func(avoid bool) (float64, int) {
		state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
			1: {RefObjID: 1, Codename: code, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
		}, nil))
		// Draws: angle 0, radius 0 (east of the centre), then angle 0.5, radius 0 (west of it).
		draws := []float64{0, 0, 0.5, 0, 0.5, 0}
		next := 0
		state.SetRandomSource(func() float64 {
			value := draws[next%len(draws)]
			next++
			return value
		})
		checks := 0
		state.SetSpawnClosedObjectCheck(func(at Spawn) bool {
			checks++
			return at.X > 1000
		})
		state.StartDivision("quest")
		lease, _ := state.PopulationLease("quest", instance.Pack(1, 1))
		if !state.SpawnQuestMonster(QuestMonsterSpawn{Division: "quest", Population: lease, Codename: code,
			Position: Spawn{RegionID: 0x655e, X: 1000, Y: 85, Z: 449}, NowMs: 1000, RadiusMin: 90, RadiusSpan: 10,
			AvoidClosedObjects: avoid}) {
			t.Fatal("the spawn failed")
		}
		return state.MaterializedInstances("quest")[0].Spawn.X, checks
	}
	if x, checks := land(true); x >= 1000 || checks != 2 {
		t.Fatalf("with the ask the monster landed at x %.1f after %d checks, want west of the centre after 2", x, checks)
	}
	if x, checks := land(false); x < 1000 || checks != 0 {
		t.Fatalf("without the ask the monster landed at x %.1f after %d checks, want the first draw with none", x, checks)
	}
}

/*
================
TestQuestMonstersLeaveOnTheirTimers

The spawn base arms the Ivy guardian's 300 s, and a quest's own timer
(Hidden Treasure 5's guardian) removes a monster the base never times.
================
*/
func TestQuestMonstersLeaveOnTheirTimers(t *testing.T) {
	const guardian, ong = "MOB_QT_02_PUNISHER_CLON", "MOB_QT_01_ONG"
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: guardian, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
		2: {RefObjID: 2, Codename: ong, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20},
	}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("quest")
	lease, exists := state.PopulationLease("quest", instance.Pack(1, 1))
	if !exists {
		t.Fatal("ordinary population absent")
	}
	at := Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}
	if !state.SpawnQuestMonster(QuestMonsterSpawn{Division: "quest", Population: lease, Codename: guardian,
		Position: at, NowMs: 1000, RadiusSpan: 20}) {
		t.Fatal("guardian admission failed")
	}
	if !state.SpawnQuestMonster(QuestMonsterSpawn{Division: "quest", Population: lease, Codename: ong,
		Position: at, NowMs: 2000, RadiusMin: 20, RadiusSpan: 80, LifetimeMs: 300000}) {
		t.Fatal("treasure guardian admission failed")
	}
	if left := state.ExpireMonsterLifetimes(300999); left != 0 {
		t.Fatalf("%d quest monsters left before their timers", left)
	}
	if left := state.ExpireMonsterLifetimes(301000); left != 1 {
		t.Fatalf("the Ivy guardian's spawn-base timer removed %d monsters", left)
	}
	if left := state.ExpireMonsterLifetimes(302000); left != 1 {
		t.Fatalf("the quest timer removed %d monsters", left)
	}
	if len(state.MaterializedInstances("quest")) != 0 {
		t.Fatal("a timed quest monster stayed in the world")
	}
}

/*
================
TestQuestMonsterRarityOverride

A spawn naming a grade lands at that grade (the party summon scroll's
grade 7), at the full health that grade gives; without one the
reference's own grade stands.
================
*/
func TestQuestMonsterRarityOverride(t *testing.T) {
	const code = "MOB_KK_YETI"
	state := NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: code, TidWord: 0xc6, MaxHP: 7059, WalkSpeed: 8, RunSpeed: 20},
	}, nil))
	state.SetRandomSource(func() float64 { return 0 })
	state.StartDivision("quest")
	lease, _ := state.PopulationLease("quest", instance.Pack(1, 1))
	request := QuestMonsterSpawn{Division: "quest", Population: lease, Codename: code,
		Position: Spawn{RegionID: 0x655e, X: 253, Y: 85, Z: 449}, NowMs: 1000, RadiusMin: 20, RadiusSpan: 80, LifetimeMs: 300000}
	if !state.SpawnQuestMonster(request) {
		t.Fatal("plain spawn refused")
	}
	request.Rarity, request.HasRarity = 7, true
	if !state.SpawnQuestMonster(request) {
		t.Fatal("graded spawn refused")
	}
	grades := map[uint8]int{}
	for _, actor := range state.MaterializedInstances("quest") {
		grades[actor.Rarity()]++
	}
	if grades[0] != 1 || grades[7] != 1 {
		t.Fatalf("grades %v, want one plain and one grade 7", grades)
	}
	for _, actor := range state.MaterializedInstances("quest") {
		if actor.CurrentHP != actor.EffectiveMaxHP() || (actor.Rarity() == 7 && actor.CurrentHP != 7059*30) {
			t.Fatalf("grade %d spawned at %d of %d HP, want full", actor.Rarity(), actor.CurrentHP, actor.EffectiveMaxHP())
		}
	}
}

/*
================
TestDaemonRingPicksItsThirdByTheRoll

The v1.188 event spawner's ring: rand() % 101 below 50 lands
in the outer third, 50 to 79 in the middle third, 80 to 100 in the inner
third; the fraction places the point inside its third.
================
*/
func TestDaemonRingPicksItsThirdByTheRoll(t *testing.T) {
	const radius = 51
	cases := []struct {
		band, fraction uint32
		want           float64
	}{
		{band: 0, fraction: 0, want: 34},
		{band: 49, fraction: nativeRandomMaximum, want: 51},
		{band: 50, fraction: 0, want: 17},
		{band: 79, fraction: 0, want: 17},
		{band: 80, fraction: 0, want: 0},
		{band: 100, fraction: nativeRandomMaximum, want: 17},
		{band: 101, fraction: 0, want: 34},
	}
	for _, c := range cases {
		if got := daemonRingRadius(radius, c.band, c.fraction); math.Abs(got-c.want) > 1e-4 {
			t.Errorf("band %d fraction %d: %f, want %f", c.band, c.fraction, got, c.want)
		}
	}
}
