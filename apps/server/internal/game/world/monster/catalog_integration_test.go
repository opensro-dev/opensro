/*
===========================================================================

catalog_integration_test.go - the monster catalog and registry

Rarity HP, gid bands, per-region materialization, division separation,
nest promotion, spawn placement and respawn of the monster registry. Loading
the shipped tables is tested in catalog_load_test.go.

===========================================================================
*/
package monster_test

import (
	"math"
	"testing"
	"time"

	. "opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
testTemplate
================
*/
func testTemplate() Template {
	refs := map[uint32]MonsterRef{
		1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", NameStrID: "SN_MOB_CH_MANGNYANG", Name: "Mangyang", Level: 1, MaxHP: 54, WalkSpeed: 8, RunSpeed: 22, ScaleDenom: 100},
		2000: {RefObjID: 2000, TidWord: 0x00C6, Codename: "MOB_TEST_OTHER", NameStrID: "SN_MOB_TEST_OTHER", Name: "Other", Level: 2, MaxHP: 70, WalkSpeed: 10, RunSpeed: 30, ScaleDenom: 100},
	}
	nests := []NestRow{
		{SpawnPoint: SpawnPoint{RefObjID: 1933, RegionID: 25258, X: 812.68, Y: 75.08, Z: 392.90}},
		{SpawnPoint: SpawnPoint{RefObjID: 1933, RegionID: 25258, X: 1610.9, Y: 60.94, Z: 503.83}},
		{SpawnPoint: SpawnPoint{RefObjID: 2000, RegionID: 25259, X: 100, Y: 0, Z: 200}},
	}
	return TemplateFromParts(refs, nests)
}

/*
================
newMonsterState
================
*/
func newMonsterState(template Template) *simulation.MonsterState {
	return simulation.NewMonsterState(template)
}

/*
================
crtWords
================
*/
// crtWords scripts the population PRNG in the native rand() domain: each
// sample projects to exactly the listed word, and drawing past the script
// fails the test, so a script also pins the native draw count.
func crtWords(t *testing.T, words ...uint32) func() float64 {
	t.Helper()
	return func() float64 {
		if len(words) == 0 {
			t.Fatal("population drew past its scripted rand() words")
		}
		word := words[0]
		words = words[1:]
		return (float64(word) + 0.5) / 32768
	}
}

/*
================
TestEffectiveMaxHPMatchesTheClientRarityTable
================
*/
func TestEffectiveMaxHPMatchesTheClientRarityTable(t *testing.T) {
	tests := []struct {
		name   string
		rarity uint8
		want   uint32
	}{
		{name: "normal", rarity: 0x00, want: 54},
		{name: "champion", rarity: 0x01, want: 108},
		{name: "unique", rarity: 0x03, want: 54},
		{name: "giant", rarity: 0x04, want: 1080},
		{name: "titan", rarity: 0x05, want: 5400},
		{name: "elite", rarity: 0x06, want: 216},
		{name: "party champion", rarity: 0x11, want: 1080},
		{name: "party giant", rarity: 0x14, want: 10800},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			instance := Instance{
				Ref: MonsterRef{MaxHP: 54},
				Nest: NestRow{
					HasRarityOverride: true,
					RarityOverride:    tc.rarity,
				},
			}
			if got := instance.EffectiveMaxHP(); got != tc.want {
				t.Fatalf("EffectiveMaxHP() = %d, want %d", got, tc.want)
			}
		})
	}
}

/*
================
TestRegistryGidBand
================
*/
// Gids come from the 400000 band, above every existing band (players
// 100000+, NPCs 200000+, ground drops 300000+), and the registry is the
// sole allocator.
func TestRegistryGidBand(t *testing.T) {
	if GidBase != 400000 {
		t.Fatalf("GidBase = %d, want 400000", GidBase)
	}
	registry := newMonsterState(testTemplate())
	registry.StartDivision("global-official")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("global-official", []uint16{25258})
	if len(instances) != 2 {
		t.Fatalf("region 25258 instances = %d, want 2", len(instances))
	}
	for _, instance := range instances {
		if instance.Gid <= GidBase {
			t.Fatalf("gid %d not above GidBase %d", instance.Gid, GidBase)
		}
		if instance.CurrentHP != instance.EffectiveMaxHP() {
			t.Fatalf("gid %d spawned at HP %d, want full effective max %d",
				instance.Gid, instance.CurrentHP, instance.EffectiveMaxHP())
		}
	}
	if instances[0].Gid == instances[1].Gid {
		t.Fatalf("duplicate gid %d", instances[0].Gid)
	}
}

/*
================
TestRegistryMaterializesPerRegionOnce
================
*/
// Materialization is per-region on demand and memoized: re-requesting a
// region returns the SAME gids (a viewer re-entering must not respawn a
// parallel population), and an untouched region allocates nothing until
// requested.
func TestRegistryMaterializesPerRegionOnce(t *testing.T) {
	registry := newMonsterState(testTemplate())

	registry.StartDivision("global-official")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	first := registry.InstancesInRegions("global-official", []uint16{25258})
	registry.StartDivision("global-official")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	second := registry.InstancesInRegions("global-official", []uint16{25258})
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("instance counts = %d then %d, want 2 and 2", len(first), len(second))
	}
	for i := range first {
		if first[i].Gid != second[i].Gid {
			t.Fatalf("re-request changed gid %d -> %d", first[i].Gid, second[i].Gid)
		}
	}

	registry.StartDivision("global-official")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	other := registry.InstancesInRegions("global-official", []uint16{25259})
	if len(other) != 1 {
		t.Fatalf("region 25259 instances = %d, want 1", len(other))
	}
	for _, instance := range first {
		if other[0].Gid == instance.Gid {
			t.Fatalf("region 25259 reused gid %d from region 25258", instance.Gid)
		}
	}
}

/*
================
TestRegistryDivisionsAreSeparate
================
*/
// Divisions are separate populations: the same template row gets a
// DIFFERENT gid per division (division-keyed ownership), never a shared
// one.
func TestRegistryDivisionsAreSeparate(t *testing.T) {
	registry := newMonsterState(testTemplate())
	registry.StartDivision("division-a")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	a := registry.InstancesInRegions("division-a", []uint16{25259})
	registry.StartDivision("division-b")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	b := registry.InstancesInRegions("division-b", []uint16{25259})
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("instance counts = %d / %d, want 1 / 1", len(a), len(b))
	}
	if a[0].Gid == b[0].Gid {
		t.Fatalf("divisions share gid %d", a[0].Gid)
	}
}

/*
================
TestSpawnableRefs
================
*/
// SpawnableRefs is the full-roster snapshot source: every distinct
// spawnable refObjID exactly once, ordered.
func TestSpawnableRefs(t *testing.T) {
	refs := testTemplate().SpawnableRefs()
	if len(refs) != 2 || refs[0].RefObjID != 1933 || refs[1].RefObjID != 2000 {
		t.Fatalf("spawnable refs = %+v, want [1933 2000]", refs)
	}
}

/*
================
TestNestPromotionProducesChampionAndGiantRarity
================
*/
func TestNestPromotionProducesChampionAndGiantRarity(t *testing.T) {
	const region = uint16(25258)
	refs := map[uint32]MonsterRef{
		1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", MonsterType: 0},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:            SpawnPoint{RefObjID: 1933, RegionID: region, X: 100, Y: 10, Z: 100},
		RetailEvidence:        true,
		ChampionGenPercentage: 100,
		MaxCount:              2,
		SightRange:            100,
		TargetPolicy:          2,
		HasChampionTactics:    true,
		ChampionTactics:       ChampionTactics{Aggressive: true, SightRange: 150, NativeTacticsFlags: 0x200},
	}}))
	// Per 5607B0 attempt: promotion roll, split roll, heading, replacement.
	// Both pass the promotion roll; the first split is in the native <=14
	// giant band, the second in the >14 champion band.
	registry.SetRandomSource(crtWords(t, 0, 14, 0, 0, 0, 0, 15, 0, 0, 0))
	now := time.Unix(100, 0)
	registry.SetTimeSource(func() time.Time { return now })
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	if first := registry.InstancesInRegions("division", []uint16{region}); len(first) != 1 {
		t.Fatalf("first callback = %d, want one", len(first))
	}
	now = now.Add(time.Second)

	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 2 {
		t.Fatalf("instances = %d, want 2", len(instances))
	}
	if instances[0].Rarity() != 4 || instances[1].Rarity() != 1 {
		t.Fatalf("rarities = %d/%d, want giant(4)/champion(1)", instances[0].Rarity(), instances[1].Rarity())
	}
	// 560968 -> 5F6EB0: both grades run on the champion tactics row, while
	// population fields stay with the nest.
	for _, instance := range instances {
		tactics := ResolveTactics(instance)
		if !tactics.Aggressive || tactics.SightRange != 150 || instance.Nest.NativeTacticsFlags != 0x200 ||
			instance.Nest.TargetPolicy != 0 || instance.Nest.MaxCount != 2 || instance.Nest.ChampionGenPercentage != 100 {
			t.Fatalf("promoted instance kept the ordinary tactics: %+v / %+v", instance.Nest, tactics)
		}
	}
}

/*
================
TestNestWithoutChampionTacticsIsNeverPromoted
================
*/
func TestNestWithoutChampionTacticsIsNeverPromoted(t *testing.T) {
	const region = uint16(25258)
	refs := map[uint32]MonsterRef{1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", MonsterType: 0}}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:            SpawnPoint{RefObjID: 1933, RegionID: region, X: 100, Y: 10, Z: 100},
		RetailEvidence:        true,
		ChampionGenPercentage: 100,
		MaxCount:              1,
	}}))
	// 560903: a zero champion tactics id skips the promotion before any roll;
	// only the heading and the replacement roll draw.
	registry.SetRandomSource(crtWords(t, 0, 0, 0))
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 1 || instances[0].Rarity() != 0 {
		t.Fatalf("instance = %+v, want ordinary unpromoted spawn", instances)
	}
}

/*
================
TestOrdinarySpawnKeepsNestTacticsBesideChampionRow
================
*/
func TestOrdinarySpawnKeepsNestTacticsBesideChampionRow(t *testing.T) {
	const region = uint16(25258)
	refs := map[uint32]MonsterRef{1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG", MonsterType: 0}}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:            SpawnPoint{RefObjID: 1933, RegionID: region, X: 100, Y: 10, Z: 100},
		RetailEvidence:        true,
		ChampionGenPercentage: 10,
		MaxCount:              1,
		SightRange:            100,
		HasChampionTactics:    true,
		ChampionTactics:       ChampionTactics{Aggressive: true, SightRange: 150},
	}}))
	registry.SetRandomSource(crtWords(t, 10, 0, 0, 0)) // rand()%101 == 10 is not < 10
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 1 || instances[0].Rarity() != 0 {
		t.Fatalf("instance = %+v, want ordinary spawn", instances)
	}
	if tactics := ResolveTactics(instances[0]); tactics.Aggressive || tactics.SightRange != 100 {
		t.Fatalf("ordinary spawn used champion tactics: %+v", tactics)
	}
}

/*
================
TestStaticSpecialMonsterTypeIsNeverPromoted
================
*/
func TestStaticSpecialMonsterTypeIsNeverPromoted(t *testing.T) {
	const region = uint16(25258)
	refs := map[uint32]MonsterRef{
		9000: {RefObjID: 9000, TidWord: 0x00C6, Codename: "MOB_TEST_UNIQUE", MonsterType: 3},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:            SpawnPoint{RefObjID: 9000, RegionID: region, X: 100, Y: 10, Z: 100},
		RetailEvidence:        true,
		ChampionGenPercentage: 100,
		MaxCount:              1,
		HasChampionTactics:    true,
		InitialDir:            16384,
	}}))
	// A static grade skips the party and promotion rolls, and a unique takes
	// the authored wInitialDir heading: only the replacement roll draws.
	registry.SetRandomSource(crtWords(t, 0, 0))

	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 1 || instances[0].Rarity() != 3 {
		t.Fatalf("special instance = %+v, want preserved rarity 3", instances)
	}
	if want := simulation.HeadingWordFromRadians(float64(InitialDirRadians(16384))); instances[0].SpawnHeading != want {
		t.Fatalf("unique heading = %d, want wInitialDir heading %d", instances[0].SpawnHeading, want)
	}
}

/*
================
TestResolveTacticsKeepsUnmatchedMobileMonstersPassiveButWandering
================
*/
func TestResolveTacticsKeepsUnmatchedMobileMonstersPassiveButWandering(t *testing.T) {
	unmatched := ResolveTactics(Instance{
		Ref:  MonsterRef{WalkSpeed: 8},
		Nest: NestRow{Radius: 900, Aggressive: true, SightRange: 200},
	})
	if unmatched.Aggressive || unmatched.SightRange != 0 ||
		unmatched.ChaseLeash != 0 || unmatched.WanderProbeDistance != 30 {
		t.Fatalf("unmatched mobile tactics = %+v, want passive retail idle wander", unmatched)
	}

	immobile := ResolveTactics(Instance{Ref: MonsterRef{WalkSpeed: 0}})
	if immobile != (Tactics{}) {
		t.Fatalf("unmatched immobile tactics = %+v, want stationary zero contract", immobile)
	}

	matched := ResolveTactics(Instance{Nest: NestRow{
		RetailEvidence: true,
		Radius:         700,
		Aggressive:     true,
		SightRange:     115,
	}})
	if !matched.Aggressive || matched.SightRange != 115 ||
		matched.ChaseLeash != 700 || matched.WanderProbeDistance != 30 {
		t.Fatalf("matched tactics = %+v", matched)
	}
}

/*
================
TestEvidenceBackedDensityAndZeroCap
================
*/
func TestEvidenceBackedDensityAndZeroCap(t *testing.T) {
	const region = uint16(25258)
	refs := map[uint32]MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG"},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{
		{
			SpawnPoint:     SpawnPoint{RefObjID: 1933, RegionID: region, X: 100, Y: 10, Z: 100},
			RetailEvidence: true,
			MaxCount:       3,
		},
		{
			SpawnPoint:     SpawnPoint{RefObjID: 1933, RegionID: region, X: 200, Y: 10, Z: 200},
			RetailEvidence: true,
			MaxCount:       0,
		},
	}))

	now := time.Unix(100, 0)
	registry.SetTimeSource(func() time.Time { return now })
	for want := 1; want < 3; want++ {
		registry.StartDivision("division")
		registry.AdvancePopulation(registry.CurrentTimeMillis())
		if live := registry.InstancesInRegions("division", []uint16{region}); len(live) != want {
			t.Fatalf("callback live=%d want=%d", len(live), want)
		}
		now = now.Add(time.Second)
	}
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 3 {
		t.Fatalf("live instances = %d, want evidence cap 3 (zero-cap row must stay empty)", len(instances))
	}
	for _, instance := range instances {
		if instance.Nest.X != 100 {
			t.Fatalf("zero-cap nest materialized instance %+v", instance)
		}
	}
}

/*
================
TestGeneratedSpawnNormalizesRegionAndUsesGroundAuthority
================
*/
func TestGeneratedSpawnNormalizesRegionAndUsesGroundAuthority(t *testing.T) {
	const west = uint16(0x6B4F)
	east := uint16(0x6B50)
	refs := map[uint32]MonsterRef{
		1933: {RefObjID: 1933, TidWord: 0x00C6, Codename: "MOB_CH_MANGNYANG"},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:     SpawnPoint{RefObjID: 1933, RegionID: west, X: 1910, Y: -500, Z: 100},
		RetailEvidence: true,
		GenerateRadius: 40,
		MaxCount:       1,
	}}))
	// Heading, replacement, then 531240: outer band (<70), full fraction of
	// the band (2/3r + 1/3r = r) and angle 0, so the candidate is 40 units
	// east of an anchor 10 units from the sector edge.
	registry.SetRandomSource(crtWords(t, 0, 0, 0, 32767, 0, 0))
	registry.SetSpawnGroundResolver(func(regionID uint16, x, _ float64, z float64) (float64, bool) {
		if regionID != east || math.Abs(x-30) > 1e-9 || math.Abs(z-100) > 1e-9 {
			t.Fatalf("ground query = region %#04x (%.3f, %.3f), want east region at (30, 100)", regionID, x, z)
		}
		return 77, true
	})

	// Materializing the west anchor may produce a live point in the east
	// sector. Query both so current-position indexing is exercised.
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{west, east})
	if len(instances) != 1 {
		t.Fatalf("instances = %d, want 1", len(instances))
	}
	spawn := instances[0].Spawn
	if spawn.RegionID != east || math.Abs(spawn.X-30) > 1e-9 ||
		math.Abs(spawn.Y-77) > 1e-9 || math.Abs(spawn.Z-100) > 1e-9 {
		t.Fatalf("generated spawn = %+v", spawn)
	}
}

/*
================
TestStaticSpawnUsesWorldSurfaceAuthority
================
*/
func TestStaticSpawnUsesWorldSurfaceAuthority(t *testing.T) {
	const region = uint16(0x5CA0)
	refs := map[uint32]MonsterRef{
		1949: {RefObjID: 1949, Codename: "MOB_CH_BANDIT"},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint: SpawnPoint{
			RefObjID: 1949,
			RegionID: region,
			X:        778.04999,
			Y:        1414.3,
			Z:        711.40002,
		},
	}}))
	groundQueries := 0
	registry.SetSpawnGroundResolver(func(regionID uint16, x, authoredY, z float64) (float64, bool) {
		groundQueries++
		if regionID != region || x != 778.04999 || authoredY != 1414.3 || z != 711.40002 {
			t.Fatalf(
				"ground query = region %#04x (%.5f, %.5f, %.5f), want the exact npcpos seed",
				regionID,
				x,
				authoredY,
				z,
			)
		}
		return 1390.33467, true
	})

	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 1 {
		t.Fatalf("static instances = %d, want 1", len(instances))
	}
	if groundQueries != 1 {
		t.Fatalf("static npcpos anchor made %d world-surface queries, want 1", groundQueries)
	}
	if instances[0].Spawn != (SpawnPoint{
		RefObjID: 1949,
		RegionID: region,
		X:        778.04999,
		Y:        1390.33467,
		Z:        711.40002,
	}) {
		t.Fatalf("static spawn = %+v, want the world-surface-resolved coordinates", instances[0].Spawn)
	}
}

/*
================
TestStaticSpawnWaitsForWorldSurfaceCoverage
================
*/
func TestStaticSpawnWaitsForWorldSurfaceCoverage(t *testing.T) {
	const region = uint16(0x5CA0)
	now := time.UnixMilli(1_784_000_000_000)
	groundOpen := false
	refs := map[uint32]MonsterRef{
		1949: {RefObjID: 1949, Codename: "MOB_CH_BANDIT"},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint: SpawnPoint{
			RefObjID: 1949,
			RegionID: region,
			X:        778.04999,
			Y:        1414.3,
			Z:        711.40002,
		},
	}}))
	registry.SetTimeSource(func() time.Time { return now })
	registry.SetSpawnGroundResolver(func(uint16, float64, float64, float64) (float64, bool) {
		return 1390.33467, groundOpen
	})

	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	if instances := registry.InstancesInRegions("division", []uint16{region}); len(instances) != 0 {
		t.Fatalf("uncovered static spawn materialized: %+v", instances)
	}
	groundOpen = true
	now = now.Add(time.Second)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 1 || instances[0].Spawn.Y != 1390.33467 {
		t.Fatalf("covered static spawn = %+v, want one grounded instance", instances)
	}
}

/*
================
TestPopulationNeverFallsBackToBlockedAnchor
================
*/
func TestPopulationNeverFallsBackToBlockedAnchor(t *testing.T) {
	const region = uint16(25258)
	base := time.UnixMilli(1_784_000_000_000)
	now := base
	groundOpen := false
	refs := map[uint32]MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG"},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:     SpawnPoint{RefObjID: 1933, RegionID: region, X: 100, Y: -500, Z: 100},
		RetailEvidence: true,
		GenerateRadius: 40,
		MaxCount:       1,
	}}))
	registry.SetTimeSource(func() time.Time { return now })
	registry.SetRandomSource(func() float64 { return 0.5 })
	registry.SetSpawnGroundResolver(func(uint16, float64, float64, float64) (float64, bool) {
		return 77, groundOpen
	})

	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	if instances := registry.InstancesInRegions("division", []uint16{region}); len(instances) != 0 {
		t.Fatalf("blocked population anchor materialized: %+v", instances)
	}
	groundOpen = true
	now = base.Add(999 * time.Millisecond)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	if instances := registry.InstancesInRegions("division", []uint16{region}); len(instances) != 0 {
		t.Fatalf("blocked slot retried before the one-second refill cadence: %+v", instances)
	}
	now = base.Add(time.Second)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	instances := registry.InstancesInRegions("division", []uint16{region})
	if len(instances) != 1 || instances[0].Spawn.Y != 77 {
		t.Fatalf("recovered population slot = %+v, want one grounded instance", instances)
	}
}

/*
================
TestDefeatSchedulesSlotAndRespawnsWithNewIdentity
================
*/
func TestDefeatSchedulesSlotAndRespawnsWithNewIdentity(t *testing.T) {
	const region = uint16(25258)
	base := time.UnixMilli(1_784_000_000_000)
	now := base
	refs := map[uint32]MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG"},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:         SpawnPoint{RefObjID: 1933, RegionID: region, X: 100, Y: 10, Z: 100},
		RetailEvidence:     true,
		MaxCount:           1,
		Respawn:            true,
		RespawnDelayMinSec: 1,
		RespawnDelayMaxSec: 3,
	}}))
	registry.SetTimeSource(func() time.Time { return now })
	registry.SetRandomSource(func() float64 { return 0.5 })

	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	first := registry.InstancesInRegions("division", []uint16{region})
	if len(first) != 1 {
		t.Fatalf("initial instances = %d, want 1", len(first))
	}
	oldGid := first[0].Gid
	if !registry.Defeat("division", oldGid, base) {
		t.Fatal("defeat rejected a live population gid")
	}
	now = base.Add(time.Second)
	registry.AdvancePopulation(now.UnixMilli())
	now = base.Add(1999 * time.Millisecond)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	if live := registry.InstancesInRegions("division", []uint16{region}); len(live) != 0 {
		t.Fatalf("respawned before due time: %+v", live)
	}
	now = base.Add(2 * time.Second)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	live := registry.InstancesInRegions("division", []uint16{region})
	if len(live) != 1 || live[0].Gid == oldGid {
		t.Fatalf("respawn = %+v, want one instance with a new gid", live)
	}
	if _, ok := registry.Get("division", oldGid); ok {
		t.Fatalf("defeated gid %d still resolves", oldGid)
	}
}

/*
================
TestRejectedRespawnPlacementRetriesWithoutLosingTheSlot
================
*/
func TestRejectedRespawnPlacementRetriesWithoutLosingTheSlot(t *testing.T) {
	const region = uint16(25258)
	base := time.UnixMilli(1_784_000_000_000)
	now := base
	groundOpen := true
	refs := map[uint32]MonsterRef{
		1933: {RefObjID: 1933, Codename: "MOB_CH_MANGNYANG"},
	}
	registry := newMonsterState(TemplateFromParts(refs, []NestRow{{
		SpawnPoint:         SpawnPoint{RefObjID: 1933, RegionID: region, X: 100, Y: 10, Z: 100},
		RetailEvidence:     true,
		GenerateRadius:     1,
		MaxCount:           1,
		Respawn:            true,
		RespawnDelayMinSec: 1,
		RespawnDelayMaxSec: 1,
	}}))
	registry.SetTimeSource(func() time.Time { return now })
	registry.SetRandomSource(func() float64 { return 0.5 })
	registry.SetSpawnGroundResolver(func(uint16, float64, float64, float64) (float64, bool) {
		return 77, groundOpen
	})

	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	first := registry.InstancesInRegions("division", []uint16{region})
	if len(first) != 1 {
		t.Fatalf("initial instances = %d, want 1", len(first))
	}
	oldGid := first[0].Gid
	if !registry.Defeat("division", oldGid, base) {
		t.Fatal("defeat rejected a live population gid")
	}

	groundOpen = false
	now = base.Add(time.Second)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	if live := registry.InstancesInRegions("division", []uint16{region}); len(live) != 0 {
		t.Fatalf("blocked respawn placement materialized: %+v", live)
	}

	groundOpen = true
	now = base.Add(1999 * time.Millisecond)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	if live := registry.InstancesInRegions("division", []uint16{region}); len(live) != 0 {
		t.Fatalf("blocked respawn slot retried before the refill cadence: %+v", live)
	}
	now = base.Add(2 * time.Second)
	registry.StartDivision("division")
	registry.AdvancePopulation(registry.CurrentTimeMillis())
	live := registry.InstancesInRegions("division", []uint16{region})
	if len(live) != 1 || live[0].Gid == oldGid || live[0].Spawn.Y != 77 {
		t.Fatalf("recovered respawn slot = %+v, want one new grounded instance", live)
	}
}
