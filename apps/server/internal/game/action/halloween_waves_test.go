/*
===========================================================================

halloween_waves_test.go - a wave lands, pays and leaves; off is nothing

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

const waveTestRegion = 0x655e

/*
================
waveTestRuntime

A division with a Gourd and all six field monsters, each with a hostile
nest. The clock is the test's.
================
*/
func waveTestRuntime(t *testing.T, on bool) (*Runtime, *time.Time) {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: halloweenGourdCodename, Name: "Gourd", TidWord: 0xc6, MaxHP: 11},
	}
	var nests []monster.NestRow
	codes := []string{"MOB_CH_WHITETIGER", "MOB_EU_KYKLOPES", "MOB_AM_CRAB",
		"MOB_KK_YETI_CLON", "MOB_TK_DEATHKARA_CLON", "MOB_TK_DEVILEYE"}
	for i, code := range codes {
		id := uint32(10 + i)
		refs[id] = monster.MonsterRef{RefObjID: id, Codename: code, Name: code, TidWord: 0xc6, MaxHP: 100, WalkSpeed: 8, RunSpeed: 20}
		nests = append(nests, monster.NestRow{
			SpawnPoint: monster.SpawnPoint{RefObjID: id, RegionID: waveTestRegion, X: 900, Y: 85, Z: 900},
			Aggressive: true, SightRange: 77,
		})
	}
	start := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	if on {
		t.Setenv(progression.EnvHalloweenEvent, "on")
	} else {
		t.Setenv(progression.EnvHalloweenEvent, "off")
	}
	t.Setenv(progression.EnvHalloweenEventWindow, start.Format(time.RFC3339)+"/"+start.Add(48*time.Hour).Format(time.RFC3339))
	clock := start.Add(time.Hour)
	state := simulation.NewMonsterState(monster.TemplateFromParts(refs, nests))
	state.SetRandomSource(func() float64 { return 0.5 })
	state.StartDivision(testDivision)
	rt := &Runtime{Monsters: state, Halloween: progression.HalloweenEventFromEnv(), Now: func() time.Time { return clock }}
	return rt, &clock
}

/*
================
detachedActors

The wave's monsters: nest-bred ones are the field's ordinary population.
================
*/
func detachedActors(rt *Runtime) []monster.Instance {
	var out []monster.Instance
	for _, actor := range rt.Monsters.MaterializedInstances(testDivision) {
		if actor.NestDetached {
			out = append(out, actor)
		}
	}
	return out
}

/*
================
TestWaveWarnsThenLandsHuntableMonstersOnce
================
*/
func TestWaveWarnsThenLandsHuntableMonstersOnce(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	rt.advanceHalloweenWaves()
	*clock = time.Date(2026, 11, 3, 6, 55, 0, 0, time.UTC)
	rt.advanceHalloweenWaves()
	if len(detachedActors(rt)) != 0 || len(rt.announcer.queued) != 1 {
		t.Fatalf("before the rotating wave: actors %d, notices %d", len(detachedActors(rt)), len(rt.announcer.queued))
	}
	*clock = clock.Add(5*time.Minute + time.Second)
	rt.advanceHalloweenWaves()
	actors := detachedActors(rt)
	want := 2 * (progression.HalloweenWaveGourdsPerField + progression.HalloweenWaveRingPerField)
	if len(actors) != want {
		t.Fatalf("actors = %d, want %d", len(actors), want)
	}
	gourds, hostile := 0, 0
	for _, actor := range actors {
		if actor.GMSpawned {
			t.Fatal("a GM-spawned wave monster would pay no box")
		}
		if actor.Ref.Codename == halloweenGourdCodename {
			gourds++
		} else if actor.Nest.Aggressive && actor.Nest.SightRange == 77 {
			hostile++
		}
	}
	if gourds != 2*progression.HalloweenWaveGourdsPerField || hostile != 2*progression.HalloweenWaveRingPerField {
		t.Fatalf("gourds %d, hostile ring %d", gourds, hostile)
	}
	*clock = clock.Add(time.Minute)
	rt.advanceHalloweenWaves()
	if len(detachedActors(rt)) != want {
		t.Fatal("the wave landed twice")
	}
	if len(rt.announcer.queued) != 2 {
		t.Fatalf("notices = %d, want the warning and the landing", len(rt.announcer.queued))
	}
	rt.Monsters.ExpireMonsterLifetimes(clock.Add(progression.HalloweenWaveSpan()).UnixMilli())
	if left := len(detachedActors(rt)); left != 0 {
		t.Fatalf("%d wave monsters outlived the wave", left)
	}
}

/*
================
TestWaveLeavesInStepsNotAllAtOnce

The #2124 fix's contract: at the bare lifetime exactly one monster per
field is due, then one more per despawn step, and the field is empty
when its last step lands. One synchronized expiry of all 140 wave
monsters cost the crowded-field run an 11-tick burst (worst tick 6.2 s).
================
*/
func TestWaveLeavesInStepsNotAllAtOnce(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	rt.advanceHalloweenWaves()
	*clock = time.Date(2026, 11, 3, 6, 55, 0, 0, time.UTC)
	rt.advanceHalloweenWaves()
	*clock = clock.Add(5*time.Minute + time.Second)
	rt.advanceHalloweenWaves()
	landed := *clock
	perField := progression.HalloweenWaveGourdsPerField + progression.HalloweenWaveRingPerField
	want := 2 * perField
	if got := len(detachedActors(rt)); got != want {
		t.Fatalf("actors = %d, want %d", got, want)
	}
	expire := func(at time.Time) int {
		rt.Monsters.ExpireMonsterLifetimes(at.UnixMilli())
		return len(detachedActors(rt))
	}
	step := progression.HalloweenWaveDespawnStep
	base := landed.Add(progression.HalloweenWaveLifetime)
	if got := expire(base.Add(-time.Second)); got != want {
		t.Fatalf("before the lifetime: %d left, want %d", got, want)
	}
	if got := expire(base); got != want-2 {
		t.Fatalf("at the lifetime: %d left, want %d (one per field)", got, want-2)
	}
	if got := expire(base.Add(step)); got != want-4 {
		t.Fatalf("one step later: %d left, want %d", got, want-4)
	}
	if got := expire(base.Add(time.Duration(perField-1) * step)); got != 0 {
		t.Fatalf("after the drain: %d left, want 0", got)
	}
}

/*
================
TestEveryWaveFieldHasOpenWorldGroundInShippedData

The timetable names fields by codename; a typo would silently skip a field.
================
*/
func TestEveryWaveFieldHasOpenWorldGroundInShippedData(t *testing.T) {
	licensed.RequireGameData(t)
	state := simulation.NewMonsterState(monster.LoadTemplate(licensed.RetailTextdataDir(t)))
	if _, found := state.ReferenceByCodename(halloweenGourdCodename); !found {
		t.Fatal("the Gourd is not in shipped data")
	}
	for _, field := range progression.HalloweenWaveFields() {
		ref, found := state.ReferenceByCodename(field)
		if _, grounded := state.FieldNest(field); !found || !grounded || ref.Name == "" {
			t.Fatalf("%s has no named open-world field", field)
		}
	}
}

/*
================
TestRestartNeverReplaysAWave

The first ask only sets the clock.
================
*/
func TestRestartNeverReplaysAWave(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	*clock = time.Date(2026, 11, 3, 7, 0, 30, 0, time.UTC)
	rt.advanceHalloweenWaves()
	*clock = clock.Add(2 * time.Second)
	rt.advanceHalloweenWaves()
	if len(detachedActors(rt)) != 0 {
		t.Fatal("a restart replayed the wave")
	}
}

/*
================
TestWavesOffAreNothing
================
*/
func TestWavesOffAreNothing(t *testing.T) {
	rt, clock := waveTestRuntime(t, false)
	rt.advanceHalloweenWaves()
	*clock = time.Date(2026, 11, 3, 6, 59, 0, 0, time.UTC)
	rt.advanceHalloweenWaves()
	*clock = clock.Add(2 * time.Minute)
	rt.advanceHalloweenWaves()
	if len(detachedActors(rt)) != 0 || len(rt.announcer.queued) != 0 || !rt.halloweenWavesAsked.IsZero() {
		t.Fatal("the event off left a trace")
	}
}
