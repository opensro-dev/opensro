/*
===========================================================================

muhan_chain_test.go - the Muhan wave director's run.

One arena's run end to end: the first wave after its delay, no
refill while it stands, the next wave only once the whole wave is
dead and its own delay has passed, the finale's rarity override,
and a fresh run after the rearm window. Install refuses tables a
wave cannot resolve against the template.

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

var muhanChainBase = time.UnixMilli(1_784_200_000_000)

const muhanWaveRegion = uint16(27091)

/*
================
muhanKindRef

One admitted test kind: grade 0, in the clone window.
================
*/
func muhanKindRef(id uint32, codename string, level uint8) monster.MonsterRef {
	return monster.MonsterRef{RefObjID: id, Codename: codename, TidWord: 0x00C6, MaxHP: 100, Level: level}
}

/*
================
muhanTestRefs

The template's Muhan kinds around band 75, plus the rows install
must not admit: a transform form and a graded kind at a named level
(either would double-name level 75 if it counted), and a kind at a
level no wave names.
================
*/
func muhanTestRefs() map[uint32]monster.MonsterRef {
	refs := map[uint32]monster.MonsterRef{
		11: muhanKindRef(11, "MOB_MUHAN_L74", 74),
		12: muhanKindRef(12, "MOB_MUHAN_L75", 75),
		13: muhanKindRef(13, "MOB_MUHAN_L76", 76),
		14: muhanKindRef(14, "MOB_MUHAN_CHANGE_L75", 75),
		16: muhanKindRef(16, "MOB_MUHAN_L50", 50),
	}
	elite := muhanKindRef(15, "MOB_MUHAN_ELITE75", 75)
	elite.MonsterType = 1
	refs[15] = elite
	return refs
}

/*
================
muhanWorld103Waves

Two waves for the solo 75 arena: two level-74 kinds on the authored
point, then one level-76 kind with the finale's rarity, scattered.
================
*/
func muhanWorld103Waves() []monster.MuhanWave {
	return []monster.MuhanWave{
		{WorldID: 103, Seq: 1, RegionID: int(muhanWaveRegion), X: 1248, Y: 378, Z: 1379,
			DelayMs: 1000, Count: 2, LevelOffset: -1, Radius: 0, MonsterType: 0},
		{WorldID: 103, Seq: 2, RegionID: int(muhanWaveRegion), X: 1519, Y: 378, Z: 562,
			DelayMs: 2000, Count: 1, LevelOffset: 1, Radius: 50, MonsterType: 6},
	}
}

/*
================
newMuhanState

A state with the wave worlds registered and the director installed
(not yet started), on the fixture clock and a constant random word.
================
*/
func newMuhanState(t *testing.T, refs map[uint32]monster.MonsterRef, waves []monster.MuhanWave) *MonsterState {
	t.Helper()
	if err := instance.RegisterPortOnly(instance.MuhanDefinitions()); err != nil {
		t.Fatal(err)
	}
	s := NewMonsterState(monster.TemplateFromParts(refs, nil))
	s.SetTimeSource(func() time.Time { return muhanChainBase })
	s.SetRandomSource(constantWord(16384))
	if err := s.InstallMuhanChain(waves); err != nil {
		t.Fatalf("InstallMuhanChain: %v", err)
	}
	return s
}

/*
================
TestMuhanChainRunsWavesInSequence
================
*/
func TestMuhanChainRunsWavesInSequence(t *testing.T) {
	s := newMuhanState(t, muhanTestRefs(), muhanWorld103Waves())
	s.StartDivision("division")
	lease, ok := s.PopulationLease("division", instance.Pack(103, 1))
	if !ok {
		t.Fatal("the solo 75 arena did not open at boot")
	}
	advance := func(offsetMs int64) []monster.Instance {
		s.AdvancePopulation(muhanChainBase.Add(time.Duration(offsetMs) * time.Millisecond).UnixMilli())
		return s.PopulationInstances("division", lease, []uint16{muhanWaveRegion})
	}
	defeat := func(offsetMs int64, gid uint32) {
		t.Helper()
		if !s.Defeat("division", gid, muhanChainBase.Add(time.Duration(offsetMs)*time.Millisecond)) {
			t.Fatalf("Defeat(%d) at %+dms refused", gid, offsetMs)
		}
	}

	if got := advance(0); len(got) != 0 {
		t.Fatalf("before the first delay: %d instances, want 0", len(got))
	}
	if got := advance(999); len(got) != 0 {
		t.Fatalf("inside the first delay: %d instances, want 0", len(got))
	}
	first := advance(1000)
	if len(first) != 2 {
		t.Fatalf("wave 1: %d instances, want 2", len(first))
	}
	for _, actor := range first {
		if actor.Ref.Codename != "MOB_MUHAN_L74" {
			t.Fatalf("wave 1 spawned %s, want the level-74 kind (band 75, offset -1)", actor.Ref.Codename)
		}
		if actor.Spawn.X != 1248 || actor.Spawn.Z != 1379 {
			t.Fatalf("wave 1 stands at (%v, %v), want the authored point (1248, 1379)", actor.Spawn.X, actor.Spawn.Z)
		}
	}
	if got := advance(60_000); len(got) != 2 {
		t.Fatalf("wave 1 a minute on: %d instances, want 2 (a wave never refills)", len(got))
	}
	defeat(60_000, first[0].Gid)
	if got := advance(60_000); len(got) != 1 {
		t.Fatalf("one of wave 1 down: %d instances, want 1 (the wave is not cleared)", len(got))
	}
	defeat(60_000, first[1].Gid)
	// The director sees the clearance on its next tick, and wave 2's
	// delay counts from there.
	if got := advance(60_000); len(got) != 0 {
		t.Fatalf("the cleared wave: %d instances, want 0", len(got))
	}
	if got := advance(61_999); len(got) != 0 {
		t.Fatalf("inside wave 2's delay: %d instances, want 0", len(got))
	}
	second := advance(62_000)
	if len(second) != 1 {
		t.Fatalf("wave 2: %d instances, want 1", len(second))
	}
	actor := second[0]
	if actor.Ref.Codename != "MOB_MUHAN_L76" {
		t.Fatalf("wave 2 spawned %s, want the level-76 kind (band 75, offset +1)", actor.Ref.Codename)
	}
	if actor.Rarity() != 6 {
		t.Fatalf("wave 2 rarity %d, want the wave's monster type 6", actor.Rarity())
	}
	if distance := math.Hypot(actor.Spawn.X-1519, actor.Spawn.Z-562); distance > 50 {
		t.Fatalf("wave 2 stands %v from its point, want within the radius 50", distance)
	}
	// The run is over: the arena rests out the rearm window, then
	// wave 1 runs again.
	defeat(62_000, actor.Gid)
	if got := advance(62_100); len(got) != 0 {
		t.Fatalf("the cleared finale: %d instances, want 0", len(got))
	}
	if got := advance(62_100 + 1799_000); len(got) != 0 {
		t.Fatalf("inside the rearm window: %d instances, want 0", len(got))
	}
	if got := advance(62_100 + 3600_000); len(got) != 0 {
		t.Fatalf("the re-armed run inside wave 1's delay: %d instances, want 0", len(got))
	}
	rerun := advance(62_100 + 3600_000 + 1000)
	if len(rerun) != 2 {
		t.Fatalf("the re-armed run: %d instances, want wave 1's 2 again", len(rerun))
	}
	for _, actor := range rerun {
		if actor.Ref.Codename != "MOB_MUHAN_L74" {
			t.Fatalf("the re-armed run spawned %s, want the level-74 kind", actor.Ref.Codename)
		}
	}
}

/*
================
TestInstallMuhanChainRefusesUnresolvableTables
================
*/
func TestInstallMuhanChainRefusesUnresolvableTables(t *testing.T) {
	world := monster.MuhanWave{WorldID: 103, Seq: 1, RegionID: int(muhanWaveRegion), X: 1248, Y: 378, Z: 1379,
		DelayMs: 1000, Count: 1, LevelOffset: 0, Radius: 0, MonsterType: 0}
	dupRefs := muhanTestRefs()
	dupRefs[17] = muhanKindRef(17, "MOB_MUHAN_L75_TWIN", 75)
	cases := []struct {
		name  string
		refs  map[uint32]monster.MonsterRef
		waves []monster.MuhanWave
	}{
		{"a wave in an unknown world", muhanTestRefs(),
			[]monster.MuhanWave{{WorldID: 999, Seq: 1, RegionID: 27091, DelayMs: 1, Count: 1}}},
		{"a wave whose level has no kind", muhanTestRefs(),
			[]monster.MuhanWave{{WorldID: 104, Seq: 1, RegionID: 27091, DelayMs: 1, Count: 1}}},
		{"a gap in the sequence", muhanTestRefs(),
			[]monster.MuhanWave{world, {WorldID: 103, Seq: 3, RegionID: 27091, DelayMs: 1, Count: 1}}},
		{"a wave that spawns nothing", muhanTestRefs(),
			[]monster.MuhanWave{{WorldID: 103, Seq: 1, RegionID: 27091, DelayMs: 1, Count: 0}}},
		{"two admitted kinds at one level", dupRefs,
			[]monster.MuhanWave{world}},
		{"no waves at all", muhanTestRefs(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewMonsterState(monster.TemplateFromParts(tc.refs, nil))
			if err := s.InstallMuhanChain(tc.waves); err == nil {
				t.Fatal("installed, want a refusal")
			}
			if s.muhanChain != nil {
				t.Fatal("a refused install left a director behind")
			}
		})
	}
}
