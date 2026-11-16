/*
===========================================================================

pandorabox_test.go - Pandora's Box: its kinds and its ten waves

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pandoraTestRuntime

A division holding only the Yeti (the level-59 row) at its field nest,
an opened box at the fixture's ground, and a clock the test moves. Every
wave of a level-59 opener resolves to the Yeti.
================
*/
func pandoraTestRuntime(t *testing.T) (*Runtime, *pandoraEvent, *time.Time, *[]string) {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: "MOB_KK_YETI", Name: "Yeti", TidWord: 0xc6, Level: 59, MaxHP: 7059, WalkSpeed: 8, RunSpeed: 20},
	}
	nests := []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: waveTestRegion, X: 900, Y: 85, Z: 900}, SightRange: 77}}
	state := simulation.NewMonsterState(monster.TemplateFromParts(refs, nests))
	state.SetRandomSource(func() float64 { return 0.5 })
	state.StartDivision(testDivision)
	clock := time.Date(2026, 12, 27, 10, 0, 0, 0, time.UTC)
	var told []string
	rt := &Runtime{Monsters: state, Now: func() time.Time { return clock }}
	rt.PushCharacterFrames = func(_, name string, frames []wire.Frame) {
		for range frames {
			told = append(told, name)
		}
	}
	lease, ok := state.PopulationLease(testDivision, instance.Pack(0, 0))
	if !ok {
		for _, population := range state.OpenWorldPopulations() {
			lease, ok = population.Lease, true
		}
	}
	if !ok {
		t.Fatal("no open-world population")
	}
	event := &pandoraEvent{
		division: testDivision, lease: lease, opener: "asd2", level: 59,
		anchor:   simulation.Spawn{RegionID: waveTestRegion, X: 900, Y: 85, Z: 900},
		endsAtMs: clock.UnixMilli() + pandoraTickMs*pandoraTicks,
	}
	return rt, event, &clock, &told
}

/*
================
killPandoraWave

Defeats every monster of the event's standing wave.
================
*/
func killPandoraWave(t *testing.T, rt *Runtime, event *pandoraEvent, at time.Time) {
	t.Helper()
	for _, gid := range event.gids {
		if !rt.Monsters.Defeat(testDivision, gid, at) {
			t.Fatalf("wave %d: gid %d would not die", event.wave, gid)
		}
	}
}

/*
================
TestPandoraWaveKindClimbsFromNineBelow

Wave k of a box opened at level L reads level min(L, 110) - 9 + k: the
last wave stands at the opener's level; a kind the shard does not hold
falls to the nearest lower one, then the nearest higher one.
================
*/
func TestPandoraWaveKindClimbsFromNineBelow(t *testing.T) {
	all := func(string) bool { return true }
	if kind, _ := pandoraWaveKind(59, 9, all); kind != "MOB_KK_YETI" {
		t.Fatalf("the last wave at 59: %s", kind)
	}
	if kind, _ := pandoraWaveKind(59, 0, all); kind != "MOB_KT_MUJIGI_CLON" {
		t.Fatalf("the first wave at 59 (level 50): %s", kind)
	}
	if kind, _ := pandoraWaveKind(130, 9, all); kind != "MOB_SD_SOPEDU" {
		t.Fatalf("above 110 the top row: %s", kind)
	}
	if kind, ok := pandoraWaveKind(30, 0, all); !ok || kind != "MOB_AM_CRAB_CLON" {
		t.Fatalf("a level-30 opener's first wave reads key 21: %s", kind)
	}
	if kind, _ := pandoraWaveKind(30, 7, all); kind != "MOB_WC_EARTHGHOST_CLON" {
		t.Fatalf("key 28 keeps its first insert: %s", kind)
	}
	onlyYeti := func(codename string) bool { return codename == "MOB_KK_YETI" }
	for wave := range pandoraWaves {
		if kind, ok := pandoraWaveKind(59, wave, onlyYeti); !ok || kind != "MOB_KK_YETI" {
			t.Fatalf("wave %d: %s", wave, kind)
		}
	}
	if _, ok := pandoraWaveKind(59, 0, func(string) bool { return false }); ok {
		t.Fatal("a shard without any kind opened a wave")
	}
}

/*
================
TestPandoraBoxRunsTenWavesThenCloses

The first wave lands four grade-7 monsters (x30 HP) at the box; a
standing wave holds the next; each cleared wave brings the next on the
job's 5 s tick; the tenth cleared closes the box and the opener hears
every step.
================
*/
func TestPandoraBoxRunsTenWavesThenCloses(t *testing.T) {
	rt, event, clock, told := pandoraTestRuntime(t)
	if !rt.landPandoraWave(event, clock.UnixMilli()) || len(event.gids) != pandoraWaveSize || event.wave != 1 {
		t.Fatalf("first wave: %d monsters, wave %d", len(event.gids), event.wave)
	}
	for _, actor := range rt.Monsters.MaterializedInstances(testDivision) {
		if actor.NestDetached && (actor.Rarity() != partySummonGrade || actor.EffectiveMaxHP() != 7059*30 || actor.CurrentHP != actor.EffectiveMaxHP()) {
			t.Fatalf("wave monster grade %d at %d of %d HP", actor.Rarity(), actor.CurrentHP, actor.EffectiveMaxHP())
		}
	}
	rt.pandora.events = []*pandoraEvent{event}
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes()
	if event.wave != 1 || len(rt.pandora.events) != 1 {
		t.Fatal("a standing wave let the next one land")
	}
	for wave := 1; wave < pandoraWaves; wave++ {
		killPandoraWave(t, rt, event, *clock)
		*clock = clock.Add(pandoraTickMs * time.Millisecond)
		rt.advancePandoraBoxes()
		if event.wave != wave+1 || len(event.gids) != pandoraWaveSize {
			t.Fatalf("after clearing wave %d: wave %d, %d monsters", wave, event.wave, len(event.gids))
		}
	}
	killPandoraWave(t, rt, event, *clock)
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes()
	if len(rt.pandora.events) != 0 {
		t.Fatal("the box stayed open after its tenth wave")
	}
	if len(*told) != pandoraWaves {
		t.Fatalf("the opener heard %d lines, want nine waves and the clearing", len(*told))
	}
}

/*
================
TestPandoraBoxClosesWhenAWaveIsNotClearedOrTimeRunsOut

A wave still standing as its 300 s end closes the box; so does the job's
last tick, whatever stands.
================
*/
func TestPandoraBoxClosesWhenAWaveIsNotClearedOrTimeRunsOut(t *testing.T) {
	rt, event, clock, _ := pandoraTestRuntime(t)
	rt.landPandoraWave(event, clock.UnixMilli())
	rt.pandora.events = []*pandoraEvent{event}
	*clock = clock.Add(partySummonLifetimeMs*time.Millisecond - pandoraTickMs*time.Millisecond)
	rt.advancePandoraBoxes()
	if len(rt.pandora.events) != 0 {
		t.Fatal("a wave standing at its 300 s kept the box open")
	}

	rt, event, clock, _ = pandoraTestRuntime(t)
	rt.landPandoraWave(event, clock.UnixMilli())
	rt.pandora.events = []*pandoraEvent{event}
	event.endsAtMs = clock.UnixMilli() + pandoraTickMs
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes()
	if len(rt.pandora.events) != 0 {
		t.Fatal("the job's last tick kept the box open")
	}
}
