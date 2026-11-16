/*
===========================================================================

christmas_waves_test.go - a winter wave lands, guards and leaves; off is nothing

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
christmasWaveRuntime

A division with every wave field monster (a hostile level-40 nest each)
and the winter rows a level-40 field names: the Snow Slave, the
level-40 White Knight and Princess, plus an ordinary monster's and a
unique's references the extra nests may name, neither a wave field's. The Christmas window is
2026-12-24 to -26 UTC; the clock is the test's.
================
*/
func christmasWaveRuntime(t *testing.T, on bool, extra ...monster.NestRow) (*Runtime, *time.Time) {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: progression.ChristmasSnowSlaveCodename, Name: "Snow Slave", TidWord: 0xc6, Level: 1, MaxHP: 5},
		2: {RefObjID: 2, Codename: progression.ChristmasKnightCodename(waveTestLevel), Name: "White Knight", TidWord: 0xc6,
			Level: waveTestLevel, MaxHP: 624652, WalkSpeed: 8, RunSpeed: 20},
		3: {RefObjID: 3, Codename: progression.ChristmasPrincessCodename(waveTestLevel), Name: "Princess Miyene", TidWord: 0xc6,
			Level: waveTestLevel, MaxHP: 3747915, WalkSpeed: 8, RunSpeed: 20},
		christmasTestOrdinary: {RefObjID: christmasTestOrdinary, Codename: "MOB_CH_MANGNYANG", Name: "Mangnyang", TidWord: 0xc6,
			Level: waveTestLevel, MaxHP: 100},
		christmasTestUnique: {RefObjID: christmasTestUnique, Codename: "MOB_CH_TIGERWOMAN", Name: "Tiger Girl", TidWord: 0xc6,
			Level: waveTestLevel, MaxHP: 100, MonsterType: 3},
	}
	var nests []monster.NestRow
	for i, field := range progression.HalloweenWaveFields() {
		id := uint32(10 + i)
		refs[id] = monster.MonsterRef{RefObjID: id, Codename: field.Codename, Name: field.Codename, TidWord: 0xc6,
			Level: waveTestLevel, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20}
		nests = append(nests, monster.NestRow{
			SpawnPoint: monster.SpawnPoint{RefObjID: id, RegionID: waveTestRegion, X: 900, Y: 85, Z: 900},
			Aggressive: true, SightRange: 77,
		})
	}
	nests = append(nests, extra...)
	if on {
		t.Setenv(progression.EnvChristmasEvent, "on")
	} else {
		t.Setenv(progression.EnvChristmasEvent, "off")
	}
	t.Setenv(progression.EnvChristmasEventWindow, "2026-12-24T00:00:00Z/2026-12-26T00:00:00Z")
	clock := time.Date(2026, 12, 24, 12, 0, 0, 0, time.UTC)
	state := simulation.NewMonsterState(monster.TemplateFromParts(refs, nests))
	state.SetRandomSource(func() float64 { return 0.5 })
	state.StartDivision(testDivision)
	rt := &Runtime{Monsters: state, Christmas: progression.ChristmasEventFromEnv(), Now: func() time.Time { return clock }}
	return rt, &clock
}

// christmasTestOrdinary and christmasTestUnique are the references
// extra nests may name.
const (
	christmasTestOrdinary = 4
	christmasTestUnique   = 5
)

/*
================
countByCodename
================
*/
func countByCodename(actors []monster.Instance) map[string]int {
	counts := map[string]int{}
	for _, actor := range actors {
		counts[actor.Ref.Codename]++
	}
	return counts
}

/*
================
TestKnightsWarnThenLandOnce

The first ask only sets the clock; the 12:25 warning queues the field
lines and the explanation; at 12:30 each field gets one White Knight of
its level band, through the quest spawn path, once, and no Snow Slave:
those roam on their own (christmas_snow_slaves.go).
================
*/
func TestKnightsWarnThenLandOnce(t *testing.T) {
	rt, clock := christmasWaveRuntime(t, true)
	rt.advanceChristmasWaves()
	*clock = time.Date(2026, 12, 24, 12, 26, 0, 0, time.UTC)
	rt.advanceChristmasWaves()
	if len(detachedActors(rt)) != 0 || len(rt.announcer.queued) != waveFields+1 {
		t.Fatalf("after the warning: actors %d, notices %d", len(detachedActors(rt)), len(rt.announcer.queued))
	}
	*clock = time.Date(2026, 12, 24, 12, 30, 1, 0, time.UTC)
	rt.advanceChristmasWaves()
	counts := countByCodename(detachedActors(rt))
	if counts[progression.ChristmasKnightCodename(waveTestLevel)] != waveFields*progression.ChristmasKnightsPerField || len(counts) != 1 {
		t.Fatalf("landed %v", counts)
	}
	for _, actor := range detachedActors(rt) {
		if actor.GMSpawned {
			t.Fatal("a GM-spawned wave monster is conjured, not hunted")
		}
	}
	*clock = clock.Add(time.Minute)
	rt.advanceChristmasWaves()
	if got := len(detachedActors(rt)); got != waveFields*progression.ChristmasKnightsPerField {
		t.Fatalf("the knights landed twice: %d actors", got)
	}
	rt.Monsters.ExpireMonsterLifetimes(clock.Add(progression.ChristmasKnightLifetime + time.Hour).UnixMilli())
	if left := len(detachedActors(rt)); left != 0 {
		t.Fatalf("%d wave monsters outlived the knights' hour", left)
	}
}

/*
================
snowSlavesByRegion
================
*/
func snowSlavesByRegion(rt *Runtime) map[uint16]int {
	regions := map[uint16]int{}
	for _, actor := range detachedActors(rt) {
		if actor.Ref.Codename == progression.ChristmasSnowSlaveCodename {
			regions[actor.Spawn.RegionID]++
		}
	}
	return regions
}

/*
================
TestSnowSlavesRoamOrdinaryNestsOnly

The standing crowd lands beside ordinary open-world nests only: never
beside a unique's nest or in a cave.
================
*/
func TestSnowSlavesRoamOrdinaryNestsOnly(t *testing.T) {
	const far = 0x5b9d
	var extra []monster.NestRow
	for i := range progression.ChristmasSnowSlaveNestShare * 8 {
		x := float64(100 + i*10)
		extra = append(extra,
			monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: christmasTestOrdinary, RegionID: far, X: x, Y: 0, Z: 500}},
			monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: christmasTestUnique, RegionID: far + 1, X: x, Y: 0, Z: 500}},
			monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: christmasTestOrdinary, RegionID: far | 0x8000, X: x, Y: 0, Z: 500}})
	}
	rt, _ := christmasWaveRuntime(t, true, extra...)
	rt.advanceChristmasSnowSlaves()
	regions := snowSlavesByRegion(rt)
	if regions[far] == 0 || regions[far+1] != 0 || regions[far|0x8000] != 0 {
		t.Fatalf("Snow Slaves by region %v: want them at the ordinary nests only", regions)
	}
}

/*
================
TestSnowSlavesStandAndComeBack

The first count fills the crowd to its share of the ordinary nests; a
count before the interval does nothing; once the interval passes, the
killed slaves come back to the full share. The crowd lives until the
window closes, and the event off or outside its window lands none.
================
*/
func TestSnowSlavesStandAndComeBack(t *testing.T) {
	const far = 0x5b9d
	var extra []monster.NestRow
	for i := range 40 {
		extra = append(extra, monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: christmasTestOrdinary, RegionID: far, X: float64(100 + i*40), Y: 0, Z: 500}})
	}
	rt, clock := christmasWaveRuntime(t, true, extra...)
	ordinary := len(rt.Monsters.OpenWorldNests())
	share := ordinary / progression.ChristmasSnowSlaveNestShare
	slaves := func() int { return rt.Monsters.CountByCodename(testDivision, progression.ChristmasSnowSlaveCodename) }
	rt.advanceChristmasSnowSlaves()
	if got := slaves(); got != share || share == 0 {
		t.Fatalf("first count: %d Snow Slaves, want %d (one per %d of %d nests)", got, share, progression.ChristmasSnowSlaveNestShare, ordinary)
	}
	killed := detachedActors(rt)[:share/2]
	for _, actor := range killed {
		rt.Monsters.Defeat(testDivision, actor.Gid, *clock)
	}
	rt.advanceChristmasSnowSlaves()
	if got := slaves(); got != share-len(killed) {
		t.Fatalf("a count inside the interval: %d Snow Slaves, want %d", got, share-len(killed))
	}
	*clock = clock.Add(progression.ChristmasSnowSlaveTopUpEvery)
	rt.advanceChristmasSnowSlaves()
	if got := slaves(); got != share {
		t.Fatalf("after the interval: %d Snow Slaves, want the full %d back", got, share)
	}
	rt.Monsters.ExpireMonsterLifetimes(rt.Christmas.EndsAt().Add(time.Minute).UnixMilli())
	if got := slaves(); got != 0 {
		t.Fatalf("%d Snow Slaves outlived the window", got)
	}
	*clock = rt.Christmas.EndsAt().Add(time.Hour)
	rt.advanceChristmasSnowSlaves()
	if got := slaves(); got != 0 {
		t.Fatalf("after the window: %d Snow Slaves landed", got)
	}
	off, _ := christmasWaveRuntime(t, false, extra...)
	off.advanceChristmasSnowSlaves()
	if got := off.Monsters.CountByCodename(testDivision, progression.ChristmasSnowSlaveCodename); got != 0 {
		t.Fatalf("event off: %d Snow Slaves", got)
	}
}

/*
================
TestPrincessLandsOnePerField
================
*/
func TestPrincessLandsOnePerField(t *testing.T) {
	rt, clock := christmasWaveRuntime(t, true)
	*clock = time.Date(2026, 12, 24, 12, 45, 0, 0, time.UTC)
	rt.advanceChristmasWaves()
	*clock = time.Date(2026, 12, 24, 13, 0, 1, 0, time.UTC)
	rt.advanceChristmasWaves()
	counts := countByCodename(detachedActors(rt))
	if counts[progression.ChristmasPrincessCodename(waveTestLevel)] != waveFields || len(counts) != 1 {
		t.Fatalf("landed %v, want one princess per field", counts)
	}
}

/*
================
TestChristmasWavesOffLandNothing
================
*/
func TestChristmasWavesOffLandNothing(t *testing.T) {
	rt, clock := christmasWaveRuntime(t, false)
	rt.advanceChristmasWaves()
	for _, at := range []time.Time{
		time.Date(2026, 12, 24, 12, 30, 1, 0, time.UTC),
		time.Date(2026, 12, 24, 13, 0, 1, 0, time.UTC),
	} {
		*clock = at
		rt.advanceChristmasWaves()
		rt.advanceChristmasSnowSlaves()
	}
	if len(detachedActors(rt)) != 0 || len(rt.announcer.queued) != 0 {
		t.Fatalf("event off: actors %d, notices %d", len(detachedActors(rt)), len(rt.announcer.queued))
	}
}

/*
================
TestChristmasEntryNamesTheNextWaves
================
*/
func TestChristmasEntryNamesTheNextWaves(t *testing.T) {
	rt, _ := christmasWaveRuntime(t, true)
	if frames := rt.ChristmasEntryFrames(testDivision, nil); len(frames) != 0 {
		t.Fatal("no character, no frames")
	}
	character := &enterworld.Character{Name: "entrant"}
	if frames := rt.ChristmasEntryFrames(testDivision, character); len(frames) != 4 {
		t.Fatalf("entry frames %d, want the window, the snowfall, the knights and the princess", len(frames))
	}
	off, _ := christmasWaveRuntime(t, false)
	if frames := off.ChristmasEntryFrames(testDivision, character); len(frames) != 0 {
		t.Fatalf("event off: %d frames", len(frames))
	}
}

/*
================
TestGameCoordinatesMatchTheMinimap

Jangan's square, sector (168, 97) at local (880, 650), reads X 6424
Y 1025 on the minimap; a sector south and west of the origin reads
negative.
================
*/
func TestGameCoordinatesMatchTheMinimap(t *testing.T) {
	if x, y := gameCoordinates(0x61a8, 880, 650); x != 6424 || y != 1025 {
		t.Fatalf("Jangan: X %d Y %d, want X 6424 Y 1025", x, y)
	}
	if x, y := gameCoordinates(0x5b9d, 1697, 1663); x != 4393 || y != -26 {
		t.Fatalf("South-Tiger Mt.: X %d Y %d, want X 4393 Y -26", x, y)
	}
}

/*
================
TestSnowSlavesComeWithTheSnowAndMelt

Between snowfalls no slave lands; during one the crowd fills, and once
it stops (and the melt spread has passed) every slave is gone.
================
*/
func TestSnowSlavesComeWithTheSnowAndMelt(t *testing.T) {
	const far = 0x5b9d
	var extra []monster.NestRow
	for i := range 40 {
		extra = append(extra, monster.NestRow{SpawnPoint: monster.SpawnPoint{RefObjID: christmasTestOrdinary, RegionID: far, X: float64(100 + i*40), Y: 0, Z: 500}})
	}
	rt, clock := christmasWaveRuntime(t, true, extra...)
	slaves := func() int { return rt.Monsters.CountByCodename(testDivision, progression.ChristmasSnowSlaveCodename) }
	*clock = time.Date(2026, 12, 24, 13, 30, 0, 0, time.UTC)
	rt.advanceChristmasSnowSlaves()
	if got := slaves(); got != 0 {
		t.Fatalf("between snowfalls: %d Snow Slaves", got)
	}
	*clock = time.Date(2026, 12, 24, 14, 0, 0, 0, time.UTC)
	rt.advanceChristmasSnowSlaves()
	if got := slaves(); got == 0 {
		t.Fatal("the 14:00 snowfall landed no Snow Slave")
	}
	rt.Monsters.ExpireMonsterLifetimes(time.Date(2026, 12, 24, 14, 59, 59, 0, time.UTC).UnixMilli())
	if got := slaves(); got == 0 {
		t.Fatal("the Snow Slaves melted before the snow stopped")
	}
	rt.Monsters.ExpireMonsterLifetimes(time.Date(2026, 12, 24, 15, 0, 0, 0, time.UTC).Add(progression.ChristmasSnowMeltSpread).UnixMilli())
	if got := slaves(); got != 0 {
		t.Fatalf("%d Snow Slaves outlived the snowfall", got)
	}
}
