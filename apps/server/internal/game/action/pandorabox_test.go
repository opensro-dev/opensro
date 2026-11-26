/*
===========================================================================

pandorabox_test.go - Pandora's Box: its kinds and its ten waves

===========================================================================
*/
package action

import (
	"bytes"
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
pandoraTestRuntime

A division holding the Yeti (the level-59 row) at its field nest and the
box monster's row (no nest), an opened box at the fixture's ground with
no box monster landed, and a clock the test moves. Every wave of a
level-59 opener resolves to the Yeti.
================
*/
func pandoraTestRuntime(t *testing.T) (*Runtime, *pandoraEvent, *time.Time, *[]string) {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: "MOB_KK_YETI", Name: "Yeti", TidWord: 0xc6, Level: 59, MaxHP: 7059, WalkSpeed: 8, RunSpeed: 20},
		2: {RefObjID: 2, Codename: pandoraBoxMonster, Name: "Pandora's Box", TidWord: 0xc6, Level: 1, MaxHP: pandoraTestBoxHP},
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
		anchor: simulation.Spawn{RegionID: waveTestRegion, X: 900, Y: 85, Z: 900},
	}
	event.open(clock.UnixMilli())
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

The opening's own tick only sets the box up; the next, 5 s later, lands
four grade-7 monsters (x30 HP) on the ring around the box; a standing
wave holds the next; each cleared wave brings the next on the job's 5 s
tick; the tenth cleared closes the box and the opener hears every step.
================
*/
func TestPandoraBoxRunsTenWavesThenCloses(t *testing.T) {
	rt, event, clock, told := pandoraTestRuntime(t)
	rt.pandora.events = []*pandoraEvent{event}
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if event.wave != 0 || len(event.gids) != 0 {
		t.Fatal("a wave landed on the opening's own tick")
	}
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if len(event.gids) != pandoraWaveSize || event.wave != 1 {
		t.Fatalf("first wave: %d monsters, wave %d", len(event.gids), event.wave)
	}
	for _, actor := range rt.Monsters.MaterializedInstances(testDivision) {
		if !actor.NestDetached {
			continue
		}
		if actor.Rarity() != partySummonGrade || actor.EffectiveMaxHP() != 7059*30 || actor.CurrentHP != actor.EffectiveMaxHP() {
			t.Fatalf("wave monster grade %d at %d of %d HP", actor.Rarity(), actor.CurrentHP, actor.EffectiveMaxHP())
		}
		// The fixture's random 0.5 rolls the outer third: 50/3*2 + 0.5*50/3.
		if distance := math.Hypot(actor.Spawn.X-event.anchor.X, actor.Spawn.Z-event.anchor.Z); math.Abs(distance-125.0/3) > 0.01 {
			t.Fatalf("wave monster %.2f from the box, want the ring's outer third", distance)
		}
	}
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if event.wave != 1 || len(rt.pandora.events) != 1 {
		t.Fatal("a standing wave let the next one land")
	}
	for wave := 1; wave < pandoraWaves; wave++ {
		killPandoraWave(t, rt, event, *clock)
		*clock = clock.Add(pandoraTickMs * time.Millisecond)
		rt.advancePandoraBoxes(rt.Now().UnixMilli())
		if event.wave != wave+1 || len(event.gids) != pandoraWaveSize {
			t.Fatalf("after clearing wave %d: wave %d, %d monsters", wave, event.wave, len(event.gids))
		}
	}
	killPandoraWave(t, rt, event, *clock)
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if len(rt.pandora.events) != 0 {
		t.Fatal("the box stayed open after its tenth wave")
	}
	if len(*told) != pandoraWaves+1 {
		t.Fatalf("the opener heard %d lines, want ten waves and the clearing", len(*told))
	}
}

/*
================
TestPandoraWaveThatCannotLandStillCounts

A wave none of whose monsters lands (here a cave floor, where the port
lands no quest monster) still counts, as the native counter advances:
the next tick brings the next wave, and after the tenth the box closes
cleared.
================
*/
func TestPandoraWaveThatCannotLandStillCounts(t *testing.T) {
	rt, event, clock, told := pandoraTestRuntime(t)
	event.anchor.RegionID |= world.DungeonRegionBit
	rt.pandora.events = []*pandoraEvent{event}
	for wave := 1; wave <= pandoraWaves; wave++ {
		*clock = clock.Add(pandoraTickMs * time.Millisecond)
		rt.advancePandoraBoxes(rt.Now().UnixMilli())
		if event.wave != wave || len(event.gids) != 0 {
			t.Fatalf("tick %d: wave %d with %d monsters", wave, event.wave, len(event.gids))
		}
	}
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if len(rt.pandora.events) != 0 || len(*told) != pandoraWaves+1 {
		t.Fatalf("after ten empty waves: %d boxes open, %d lines", len(rt.pandora.events), len(*told))
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
	rt.pandora.events = []*pandoraEvent{event}
	*clock = clock.Add(pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	*clock = clock.Add(partySummonLifetimeMs*time.Millisecond - pandoraTickMs*time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if len(rt.pandora.events) != 0 {
		t.Fatal("a wave standing at its 300 s kept the box open")
	}

	// The job's 240th tick falls 1195 s after the opening, whatever
	// stands.
	rt, event, clock, _ = pandoraTestRuntime(t)
	opened := *clock
	rt.pandora.events = []*pandoraEvent{event}
	*clock = opened.Add((pandoraTicks - 2) * pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if len(rt.pandora.events) != 1 {
		t.Fatal("the box closed before the job's last tick")
	}
	*clock = opened.Add((pandoraTicks - 1) * pandoraTickMs * time.Millisecond)
	rt.advancePandoraBoxes(rt.Now().UnixMilli())
	if len(rt.pandora.events) != 0 {
		t.Fatal("the job's last tick kept the box open")
	}
}

/*
================
pandoraUseFixture

A level-59 user holding two boxes in So-Ok's Christmas Gift's mode,
standing in region, in a division whose template holds the Yeti.
================
*/
func pandoraUseFixture(t *testing.T, region uint16) (*Runtime, *enterworld.Character, persistentSummonUse) {
	t.Helper()
	c := testCharacter()
	level := int64(59)
	c.Level, c.MaxLevel = &level, &level
	regionID, x, y, z := int64(region), 900.0, 85.0, 900.0
	c.World = &domain.CharacterWorld{Spawn: &domain.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z}}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: 23, RefObjID: 9961, Codename: PandoraBoxItem, TypeFlags: wire.PackTypeFlags(3, 3, 15, 1), StackCount: 2})
	rt := sookChristmasRuntime(t, c, true)
	refs := map[uint32]monster.MonsterRef{
		1: {RefObjID: 1, Codename: "MOB_KK_YETI", Name: "Yeti", TidWord: 0xc6, Level: 59, MaxHP: 7059, WalkSpeed: 8, RunSpeed: 20},
	}
	nests := []monster.NestRow{{SpawnPoint: monster.SpawnPoint{RefObjID: 1, RegionID: waveTestRegion, X: 900, Y: 85, Z: 900}, SightRange: 77}}
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(refs, nests))
	rt.Monsters.SetRandomSource(func() float64 { return 0.5 })
	rt.Monsters.StartDivision(testDivision)
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	use := persistentSummonUse{division: testDivision, character: c, row: len(c.MissionInventory) - 1,
		request: wire.ItemUseRequest{Slot: 23}, nowMs: rigidTestNowMs}
	return rt, c, use
}

/*
================
hasBanner

Whether frames carry the notice line.
================
*/
func hasBanner(frames []wire.Frame, line string) bool {
	want := exchangeBanner(line)
	for _, frame := range frames {
		if frame.Opcode == want.Opcode && bytes.Equal(frame.Payload, want.Payload) {
			return true
		}
	}
	return false
}

/*
================
TestPandoraBoxOpensWithTheNativeLines

A town answers the box's own line ("You cannot use Pandora's Box.", not
the scrolls') and keeps it. In the field the box is spent with the
native opening line, and its job waits a tick: no monster stands until
the first wave's tick, 5 s on.
================
*/
func TestPandoraBoxOpensWithTheNativeLines(t *testing.T) {
	const jangan = 0x61a8
	rt, c, use := pandoraUseFixture(t, jangan)
	var result OpResult
	if rt.usePandoraBox(use, &result) || summonScrollRowLeft(c, PandoraBoxItem) != 2 {
		t.Fatal("the box was spent in a town")
	}
	if !hasBanner(result.Frames, pandoraTownLine) || hasBanner(result.Frames, partySummonTownLine) {
		t.Fatalf("the town answered %+v, want the box's own line", result.Frames)
	}

	rt, c, use = pandoraUseFixture(t, waveTestRegion)
	result = OpResult{}
	if !rt.usePandoraBox(use, &result) {
		t.Fatalf("refused in the field: %q %+v", result.DiagnosticRefusal, result.Frames)
	}
	if summonScrollRowLeft(c, PandoraBoxItem) != 1 || !hasBanner(result.Frames, pandoraOpenLine) {
		t.Fatal("the opened box was not spent with the native line")
	}
	if len(rt.pandora.events) != 1 || len(detachedActors(rt)) != 0 {
		t.Fatal("a wave landed at the opening")
	}
	if event := rt.pandora.events[0]; event.nextMs != rigidTestNowMs+pandoraTickMs ||
		event.endsAtMs != rigidTestNowMs+(pandoraTicks-1)*pandoraTickMs {
		t.Fatalf("the job's next tick at %d and last at %d", event.nextMs-rigidTestNowMs, event.endsAtMs-rigidTestNowMs)
	}
}
