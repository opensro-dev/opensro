package monster_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	. "opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

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

func newMonsterState(template Template) *simulation.MonsterState {
	return simulation.NewMonsterState(template)
}

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

// SpawnableRefs is the full-roster snapshot source: every distinct
// spawnable refObjID exactly once, ordered.
func TestSpawnableRefs(t *testing.T) {
	refs := testTemplate().SpawnableRefs()
	if len(refs) != 2 || refs[0].RefObjID != 1933 || refs[1].RefObjID != 2000 {
		t.Fatalf("spawnable refs = %+v, want [1933 2000]", refs)
	}
}

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

// The classification filter is the arbiter-confirmed binary admit set
// (RZ seq234, closing the A8 dispute): rows with (TID1=1, TID2=2, TID3=1)
// pack to 0x00C6 and are monsters; TID3=2 is the NPC subtype; col12/TID4
// is NOT consumed by the gates, so a quest-clone row differing only in
// col12 IS a monster.
func TestLoadMonsterRefsClassification(t *testing.T) {
	dir := t.TempDir()
	rows := "" +
		// 120-column rows are not required by the loader; pad to the
		// consumed range. Column layout: 0 service, 1 id, 2 codename,
		// 8 charBit, 9-11 TID1-3, 12 unused-by-gates, 46/47/48
		// walk/run/scale, and 50 BCRadius.
		row(1, 1933, "MOB_CH_MANGNYANG", 1, 2, 1, 1, "8", "22", "100") +
		row(1, 5555, "MOB_QT_CLONE", 1, 2, 1, 0, "8", "22", "100") + // col12=0: STILL a monster (seq234)
		row(1, 7495, "NPC_EU_SMITH", 1, 2, 2, 0, "0", "0", "100") + // TID3=2: NPC
		row(0, 1934, "MOB_DISABLED", 1, 2, 1, 1, "8", "22", "100") + // service=0
		row(1, 6001, "MOB_BAD_WALK", 1, 2, 1, 1, "NaN", "22", "100") +
		row(1, 6002, "MOB_BAD_RUN", 1, 2, 1, 1, "8", "-1", "100") +
		row(1, 6003, "MOB_BAD_SCALE", 1, 2, 1, 1, "8", "22", "0") +
		rowWithMetadata(6004, "MOB_BAD_LEVEL", "256", "54") +
		rowWithMetadata(6005, "MOB_BAD_HP", "1", "not-a-number")
	if err := os.WriteFile(filepath.Join(dir, "characterdata_test.txt"), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "textdataname.txt"),
		[]byte("1\tSN_MOB_CH_MANGNYANG\t0\t0\t0\t0\t0\t0\tMangyang\n"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	refs := LoadMonsterRefs(dir)
	if len(refs) != 2 {
		t.Fatalf("classified %d rows as monsters, want 2 (1933 + the col12=0 clone) - got %v", len(refs), refs)
	}
	if _, ok := refs[5555]; !ok {
		t.Fatal("col12=0 row excluded - the loader is still applying the retracted seq42 TID4 gate")
	}
	mangnyang, ok := refs[1933]
	if !ok {
		t.Fatal("1933 missing from the monster set")
	}
	if mangnyang.TidWord != 0x00C6 {
		t.Fatalf("TidWord = %#04x, want 0x00C6", mangnyang.TidWord)
	}
	if mangnyang.WalkSpeed != 8 || mangnyang.RunSpeed != 22 || mangnyang.ScaleDenom != 100 || mangnyang.BodyRadius != 2 {
		t.Fatalf("movement/contact data = %v/%v/%v radius %v, want 8/22/100 radius 2", mangnyang.WalkSpeed, mangnyang.RunSpeed, mangnyang.ScaleDenom, mangnyang.BodyRadius)
	}
	if mangnyang.MonsterType != 0 {
		t.Fatalf("Mangnyang monster type = %d, want ordinary type 0", mangnyang.MonsterType)
	}
	if mangnyang.Country != 0 {
		t.Fatalf("Mangnyang country = %d, want China bucket 0", mangnyang.Country)
	}
	if mangnyang.NameStrID != "SN_MOB_CH_MANGNYANG" || mangnyang.Name != "Mangyang" {
		t.Fatalf("name = %q -> %q, want SN_MOB_CH_MANGNYANG -> Mangyang", mangnyang.NameStrID, mangnyang.Name)
	}
	if mangnyang.Level != 1 || mangnyang.MaxHP != 54 ||
		mangnyang.ModelPath != `mob\china\mangnyang.bsr` {
		t.Fatalf("snapshot metadata = level %d hp %d model %q", mangnyang.Level, mangnyang.MaxHP, mangnyang.ModelPath)
	}
}

func TestLoadMonsterRefsJoinsCharacterInfoRideContract(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "characterdata_test.txt"),
		[]byte(row(1, 1954, "MOB_CH_TIGERWOMAN", 1, 2, 1, 1, "8", "22", "100")),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	// Retail skilleffect.txt is tabular even for its section directives. This
	// exact header shape previously made the server discard every ride row.
	const skillEffect = "#section\tcharacterInfo\n" +
		"MOB_CH_TIGERWOMAN\tMOB_TIGERWOMAN\t2.8\tnone\tres\\mob\\china\\bluetiger.bsr\n" +
		"MOB_FIXED\tMOB_FIXED\t1\tRT_FIXED\tres\\mob\\fixed.bsr\n" +
		"MOB_DUMMY\tMOB_DUMMY\t1\tRT_DUMMY\tres\\mob\\dummy.bsr\n" +
		"MOB_UNKNOWN\tMOB_UNKNOWN\t1\tRT_GUESSED\tres\\mob\\guessed.bsr\n" +
		"#section\tskillInfo\n"
	if err := os.WriteFile(filepath.Join(dir, "skilleffect.txt"), []byte(skillEffect), 0o644); err != nil {
		t.Fatal(err)
	}

	tigerGirl, ok := LoadMonsterRefs(dir)[1954]
	if !ok {
		t.Fatal("Tiger Girl ref missing")
	}
	if tigerGirl.RideModelPath != `res\mob\china\bluetiger.bsr` || tigerGirl.RiderTransformMode != 0 {
		t.Fatalf("joined Tiger Girl ride contract = %+v", tigerGirl)
	}
}

// npcpos rows parse both mainland (unsigned) and dungeon (signed
// 0x8000-bit) region ids; the signed form silently dropped every dungeon
// spawn before the seq186 canary red.
func TestLoadSpawnPointsParsesDungeonRegions(t *testing.T) {
	dir := t.TempDir()
	rows := "1933\t25258\t812.68\t75.08\t392.90\n" +
		"1933\t-32767\t100.5\t-2.0\t200.5\n" + // uint16 0x8001, dungeon-sector bit set
		"1933\tnotanumber\t1\t2\t3\n" // unparseable region: skipped, not fatal
	if err := os.WriteFile(filepath.Join(dir, "npcpos.txt"), []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}

	points := LoadSpawnPoints(dir)
	if len(points) != 2 {
		t.Fatalf("parsed %d rows, want 2", len(points))
	}
	if points[0].RegionID != 25258 {
		t.Fatalf("mainland region = %d, want 25258", points[0].RegionID)
	}
	if points[1].RegionID != 0x8001 {
		t.Fatalf("dungeon region = %#04x, want 0x8001 (sign-bit form of -32767)", points[1].RegionID)
	}
}

// row builds one synthetic characterdata line with the consumed columns
// placed at their real indices.
func row(service int, id int, codename string, tid1, tid2, tid3, col12 int, walk, run, scale string) string {
	cols := make([]string, 60)
	for i := range cols {
		cols[i] = "0"
	}
	cols[0] = itoa(service)
	cols[1] = itoa(id)
	cols[2] = codename
	cols[5] = "SN_" + codename
	cols[8] = "1" // char/bionic bit (every characterdata row ships 1)
	cols[9], cols[10], cols[11], cols[12] = itoa(tid1), itoa(tid2), itoa(tid3), itoa(col12)
	cols[46], cols[47], cols[48] = walk, run, scale
	cols[50] = "2"
	cols[52] = `mob\china\mangnyang.bsr`
	cols[57] = "1"
	cols[59] = "54"
	line := cols[0]
	for _, c := range cols[1:] {
		line += "\t" + c
	}
	return line + "\n"
}

func rowWithMetadata(id int, codename, level, maxHP string) string {
	line := row(1, id, codename, 1, 2, 1, 1, "8", "22", "100")
	cols := strings.Split(strings.TrimSuffix(line, "\n"), "\t")
	cols[57] = level
	cols[59] = maxHP
	return strings.Join(cols, "\t") + "\n"
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	digits := ""
	for v > 0 {
		digits = string(rune('0'+v%10)) + digits
		v /= 10
	}
	return digits
}

// Canary against the REAL shipped v1.150 textdata (the leveldata canary
// posture): the counts under the ARBITER-CONFIRMED classification (RZ
// seq234) are pinned exactly, so a media re-extraction or a packing
// regression fails HERE, loudly, instead of silently reshaping the
// population.
func TestShippedTemplateCanary(t *testing.T) {
	dir := ""
	for _, candidate := range []string{
		filepath.Join("..", "..", "..", "..", "..", "..", "..", "extracted", "Media_extracted", "server_dep", "silkroad", "textdata"),
	} {
		if _, err := os.Stat(filepath.Join(candidate, "npcpos.txt")); err == nil {
			dir = candidate
			break
		}
	}
	if dir == "" {
		t.Skip("shipped textdata not present in this checkout")
	}

	template := LoadTemplate(dir)
	if got := len(template.Refs); got != 5986 {
		t.Fatalf("monster refs = %d, want 5986 (RZ seq234 admit set over shipped characterdata)", got)
	}
	if got := len(template.Nests); got != 8753 {
		t.Fatalf("monster nest rows = %d, want 8753 (shipped npcpos)", got)
	}
	if got := template.EvidenceMatches; got != 8458 {
		t.Fatalf("combined population matches = %d, want 8458 native-float natural-key joins after the monster classifier", got)
	}
	for _, nest := range template.Nests {
		if nest.RetailEvidence && (!nest.HasControls || (nest.HasChampionTactics && !nest.ChampionTactics.HasControls)) {
			t.Fatalf("source-backed nest lost complete tactics: %+v", nest.SpawnPoint)
		}
	}
	// Every shipped unique anchor must carry the shared hive, including the
	// three half-tenth decimal spellings that formerly produced extra uniques.
	families := map[uint32][]NestRow{}
	for _, nest := range template.Nests {
		if template.Refs[nest.RefObjID].MonsterType&15 == 3 {
			families[nest.RefObjID] = append(families[nest.RefObjID], nest)
		}
	}
	for ref, nests := range families {
		if len(nests) == 0 {
			continue
		}
		for _, nest := range nests {
			if !nest.RetailEvidence || nest.HiveMaxCount != 1 || nest.HiveKey != nests[0].HiveKey {
				t.Fatalf("unique %d escaped shared policy: %+v", ref, nest)
			}
		}
		state := simulation.NewMonsterState(TemplateFromParts(template.Refs, nests))
		now := time.Unix(100, 0)
		state.SetTimeSource(func() time.Time { return now })
		state.StartDivision("audit")
		// Authored unique delays can be hours. Cross-region observation must
		// not bypass them; advance the world clock beyond every initial delay.
		now = now.Add(7 * 24 * time.Hour)
		for _, nest := range nests {
			state.StartDivision("audit")
			state.AdvancePopulation(state.CurrentTimeMillis())
			state.InstancesInRegions("audit", []uint16{nest.RegionID})
			now = now.Add(5 * time.Minute)
		}
		if got := len(state.MaterializedInstances("audit")); got != 1 {
			t.Fatalf("unique %d crossing created %d occupants", ref, got)
		}
		if got := len(state.DrainUniqueNotices("audit")); got != 1 {
			t.Fatalf("unique %d crossing emitted %d appearances", ref, got)
		}
	}
	// 178 includes MOB_DH_SOLDIEREARTHGHOST and its clone: the v1.150 client
	// places them and QNO_WC_SOLDIER_EA2_1 needs 1,600 kills, while the v1.188
	// shard backup caps every one of their nests at zero (laterDisabledCodenames).
	if got := len(template.SpawnableRefs()); got != 178 {
		t.Fatalf("spawnable refs = %d, want 178", got)
	}
	for _, codename := range []string{"MOB_DH_SOLDIEREARTHGHOST", "MOB_DH_SOLDIEREARTHGHOST_CLON"} {
		found := false
		for _, ref := range template.SpawnableRefs() {
			found = found || ref.Codename == codename
		}
		if !found {
			t.Fatalf("%s is a v1.150 quest target and must be spawnable", codename)
		}
	}
	monsterTypes := map[uint8]int{}
	for _, ref := range template.Refs {
		monsterTypes[ref.MonsterType]++
	}
	if monsterTypes[0] != 5967 || monsterTypes[3] != 19 || len(monsterTypes) != 2 {
		t.Fatalf("monster type distribution = %v, want 5967 ordinary / 19 unique", monsterTypes)
	}
	mangnyang, ok := template.Refs[1933]
	if !ok || mangnyang.Codename != "MOB_CH_MANGNYANG" || mangnyang.TidWord != 0x00C6 {
		t.Fatalf("1933 = %+v, want MOB_CH_MANGNYANG with TidWord 0x00C6", mangnyang)
	}
	if mangnyang.WalkSpeed != 8 || mangnyang.RunSpeed != 22 {
		t.Fatalf("1933 speeds = %v/%v, want 8/22", mangnyang.WalkSpeed, mangnyang.RunSpeed)
	}
	if mangnyang.NameStrID != "SN_MOB_CH_MANGNYANG" || mangnyang.Name != "Mangyang" ||
		mangnyang.Level != 1 || mangnyang.MaxHP != 54 || mangnyang.Country != 0 ||
		mangnyang.ModelPath != `mob\china\mangnyang.bsr` {
		t.Fatalf("1933 retail metadata = %+v", mangnyang)
	}
	scaleCanary, ok := template.Refs[7550]
	if !ok || scaleCanary.ScaleDenom != 135 {
		t.Fatalf("7550 = %+v, want shipped non-default scale denominator 135", scaleCanary)
	}
	// Dungeon spawns must be present (the seq186 red): the shipped file
	// carries 172 monster rows in sign-bit dungeon regions.
	dungeonRows := 0
	for _, nest := range template.Nests {
		if nest.RegionID&0x8000 != 0 {
			dungeonRows++
		}
	}
	if dungeonRows != 172 {
		t.Fatalf("dungeon-region nest rows = %d, want 172", dungeonRows)
	}
}
