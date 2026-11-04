/*
===========================================================================

halloween_waves_test.go - a wave lands, pays and leaves; off is nothing

===========================================================================
*/
package action

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
)

const waveTestRegion = 0x655e

// waveTestLevel is every fixture field monster's level.
const waveTestLevel = 40

// waveFields is how many fields one wave lands in.
const waveFields = 3

/*
================
waveTestRuntime

A division with a Gourd and all nine field monsters, each with a hostile
nest at level 40. The clock is the test's.
================
*/
func waveTestRuntime(t *testing.T, on bool) (*Runtime, *time.Time) {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: halloweenGourdCodename, Name: "Gourd", TidWord: 0xc6, MaxHP: 11},
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
	if len(detachedActors(rt)) != 0 || len(rt.announcer.queued) != waveFields+1 {
		t.Fatalf("before the rotating wave: actors %d, notices %d", len(detachedActors(rt)), len(rt.announcer.queued))
	}
	*clock = clock.Add(5*time.Minute + time.Second)
	rt.advanceHalloweenWaves()
	actors := detachedActors(rt)
	want := waveFields * (progression.HalloweenWaveGourdsPerField + progression.HalloweenWaveRingPerField)
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
	if gourds != waveFields*progression.HalloweenWaveGourdsPerField || hostile != waveFields*progression.HalloweenWaveRingPerField {
		t.Fatalf("gourds %d, hostile ring %d", gourds, hostile)
	}
	*clock = clock.Add(time.Minute)
	rt.advanceHalloweenWaves()
	if len(detachedActors(rt)) != want {
		t.Fatal("the wave landed twice")
	}
	if len(rt.announcer.queued) != 2*(waveFields+1) {
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
	want := waveFields * perField
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
	if got := expire(base); got != want-waveFields {
		t.Fatalf("at the lifetime: %d left, want %d (one per field)", got, want-waveFields)
	}
	if got := expire(base.Add(step)); got != want-2*waveFields {
		t.Fatalf("one step later: %d left, want %d", got, want-2*waveFields)
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
	clientdata.RequireGameData(t)
	state := simulation.NewMonsterState(monster.LoadTemplate(clientdata.RetailTextdataDir(t)))
	if _, found := state.ReferenceByCodename(halloweenGourdCodename); !found {
		t.Fatal("the Gourd is not in shipped data")
	}
	rt := &Runtime{Monsters: state}
	for _, field := range progression.HalloweenWaveFields() {
		ref, found := state.ReferenceByCodename(field.Codename)
		if _, grounded := state.FieldNest(field.Codename); !found || !grounded || ref.Name == "" || field.Area == "" {
			t.Fatalf("%s has no named open-world field", field.Codename)
		}
		// Every notice must fit unclipped: clampNotice would silently
		// cut the level off the longest line.
		place, _ := rt.halloweenFieldPlace(field)
		for _, lead := range []string{halloweenWaveNoticeLead, halloweenWaveNoticeNow} {
			if line := fmt.Sprintf(lead, place); clampNotice(line) != line {
				t.Fatalf("notice over budget: %q", line)
			}
		}
	}
	if clampNotice(halloweenWaveNoticeHow) != halloweenWaveNoticeHow {
		t.Fatal("the Gourd line is over budget")
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

/*
================
TestWaveNoticesNameTheAreaTheLevelAndTheGourd

#2545: each field is named by area and level, and a last line says what a
Gourd is; the first live run's notice named only monsters and read as a
unique spawning.
================
*/
func TestWaveNoticesNameTheAreaTheLevelAndTheGourd(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	rt.advanceHalloweenWaves()
	*clock = time.Date(2026, 11, 3, 6, 55, 0, 0, time.UTC)
	rt.advanceHalloweenWaves()
	feed := rt.announcer.feed
	if len(feed) != waveFields+1 {
		t.Fatalf("notices = %d, want one per field and the Gourd line", len(feed))
	}
	wave, ok := rt.Halloween.NextWave(clock.Add(-time.Minute))
	if !ok {
		t.Fatal("no wave on the timetable")
	}
	for i, field := range wave.Fields {
		want := fmt.Sprintf(halloweenWaveNoticeLead, fmt.Sprintf("%s (Lv %d field)", field.Area, waveTestLevel))
		if feed[i].text != want {
			t.Fatalf("notice %d = %q, want %q", i, feed[i].text, want)
		}
	}
	if feed[waveFields].text != halloweenWaveNoticeHow {
		t.Fatalf("last notice = %q, want the Gourd line", feed[waveFields].text)
	}
}

/*
================
TestEntryNoticesOnlyWhileTheEventRuns

A login while the event runs hears that it is on, when it ends and where
the next wave lands; off, or after the window, nothing.
================
*/
func TestEntryNoticesOnlyWhileTheEventRuns(t *testing.T) {
	rt, clock := waveTestRuntime(t, true)
	*clock = time.Date(2026, 11, 3, 8, 0, 0, 0, time.UTC)
	character := &enterworld.Character{Name: "entrant"}
	packets := rt.HalloweenEntryFrames(testDivision, character)
	if len(packets) != 3 {
		t.Fatalf("entry notices = %d, want the window, the next wave and the Gourd line", len(packets))
	}
	wave, _ := rt.Halloween.NextWave(*clock)
	if wave.At != time.Date(2026, 11, 3, 22, 0, 0, 0, time.UTC) {
		t.Fatalf("next wave at %v, want the 22:00 peak", wave.At)
	}
	body := string(packets[1].Payload)
	if !strings.Contains(body, string(utf16Bytes(wave.Fields[2].Area))) {
		t.Fatal("the next-wave notice does not name the end-game area")
	}
	if rt.HalloweenEntryFrames(testDivision, nil) != nil {
		t.Fatal("no character, yet notices")
	}
	*clock = time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC)
	if got := rt.HalloweenEntryFrames(testDivision, character); got != nil {
		t.Fatalf("after the window: %d notices", len(got))
	}
	off, _ := waveTestRuntime(t, false)
	if got := off.HalloweenEntryFrames(testDivision, character); got != nil {
		t.Fatalf("event off: %d notices", len(got))
	}
}

/*
================
utf16Bytes

A string as the notification frame carries it: UTF-16 LE units.
================
*/
func utf16Bytes(text string) []byte {
	var out []byte
	for _, unit := range utf16.Encode([]rune(text)) {
		out = append(out, byte(unit), byte(unit>>8))
	}
	return out
}
