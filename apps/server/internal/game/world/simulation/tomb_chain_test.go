/*
===========================================================================

tomb_chain_test.go - Qin-Shi tomb trigger chain tests (#2372 slice A)

The Floor 5 chain as the v1.188 server runs it: the four guardians spawn
once when the chain arms; the Flame Master stays absent until all four
are dead, then spawns once; its death opens the Floor 6 entry gate; the
chain re-arms on the floor's unique respawn window. A stage with Count 2
proves the wave rule: the trigger fires only when the wave is fully dead.

===========================================================================
*/
package simulation

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/world/monster"
)

const tombChainRegion = uint16(0x8003)

var tombChainBase = time.UnixMilli(1_784_100_000_000)

// Floor 5 chain codenames and their test ref ids.
const (
	tombRefEast      = uint32(11)
	tombRefSouth     = uint32(12)
	tombRefWest      = uint32(13)
	tombRefNorth     = uint32(14)
	tombRefFlame     = uint32(15)
	tombRefWaveA     = uint32(21)
	tombRefWaveBoss  = uint32(22)
	tombWaveStageNam = "Test_Wave"
)

/*
================
tombChainRef

A unique-grade tomb ref (MonsterType 3, as the guardians are).
================
*/
func tombChainRef(id uint32, codename string) monster.MonsterRef {
	return monster.MonsterRef{RefObjID: id, Codename: codename, TidWord: 0x00C6, MaxHP: 100, MonsterType: 3}
}

/*
================
tombChainNest

A chain-owned nest row as the tomb template now carries it: max count 0.
================
*/
func tombChainNest(id uint32, x float64) monster.NestRow {
	// The tomb template pins its rows (PolicyPinned + RetailEvidence),
	// so a zeroed MaxCount stays zero: InstanceLimit only falls back to
	// one slot for an unpinned anchor.
	return monster.NestRow{
		SpawnPoint:     monster.SpawnPoint{RefObjID: id, RegionID: tombChainRegion, X: x, Y: -50, Z: 100},
		MaxCount:       0,
		Respawn:        false,
		PolicyPinned:   true,
		RetailEvidence: true,
	}
}

/*
================
newTombChainWorld

A MonsterState over the Floor 5 chain template (zeroed nests), with the
chain installed or not, on a pinned clock and random source.
================
*/
func newTombChainWorld(t *testing.T, install bool) *MonsterState {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		tombRefEast:  tombChainRef(tombRefEast, "MOB_TQ_EASTGUARDIAN"),
		tombRefSouth: tombChainRef(tombRefSouth, "MOB_TQ_SOUTHGUARDIAN"),
		tombRefWest:  tombChainRef(tombRefWest, "MOB_TQ_WESTGUARDIAN"),
		tombRefNorth: tombChainRef(tombRefNorth, "MOB_TQ_NORTHGUARDIAN"),
		tombRefFlame: tombChainRef(tombRefFlame, "MOB_TQ_FLAMEMASTER"),
	}
	nests := []monster.NestRow{
		tombChainNest(tombRefEast, 100),
		tombChainNest(tombRefSouth, 200),
		tombChainNest(tombRefWest, 300),
		tombChainNest(tombRefNorth, 400),
		tombChainNest(tombRefFlame, 500),
	}
	s := NewMonsterState(monster.TemplateFromParts(refs, nests))
	now := tombChainBase
	s.SetTimeSource(func() time.Time { return now })
	s.SetRandomSource(constantWord(16384))
	if install {
		if err := s.InstallTombChain(); err != nil {
			t.Fatalf("InstallTombChain: %v", err)
		}
	}
	s.StartDivision("division")
	return s
}

/*
================
tombAdvance

Advances the population in one-second steps to the offset.
================
*/
func tombAdvance(s *MonsterState, offsetMs int64) []monster.Instance {
	target := tombChainBase.Add(time.Duration(offsetMs) * time.Millisecond)
	for step := tombChainBase.Add(time.Second); !step.After(target); step = step.Add(time.Second) {
		s.AdvancePopulation(step.UnixMilli())
	}
	return s.InstancesInRegions("division", []uint16{tombChainRegion})
}

/*
================
countRef
================
*/
func countRef(live []monster.Instance, ref uint32) int {
	n := 0
	for _, in := range live {
		if in.Ref.RefObjID == ref {
			n++
		}
	}
	return n
}

/*
================
liveGid
================
*/
func liveGid(t *testing.T, live []monster.Instance, ref uint32) uint32 {
	t.Helper()
	for _, in := range live {
		if in.Ref.RefObjID == ref {
			return in.Gid
		}
	}
	t.Fatalf("no live instance of ref %d in %+v", ref, live)
	return 0
}

/*
================
TestTombChainArmsGuardiansOnceAndFlameMasterWaits
================
*/
func TestTombChainArmsGuardiansOnceAndFlameMasterWaits(t *testing.T) {
	s := newTombChainWorld(t, true)
	live := tombAdvance(s, 5000)
	for _, ref := range []uint32{tombRefEast, tombRefSouth, tombRefWest, tombRefNorth} {
		if got := countRef(live, ref); got != 1 {
			t.Fatalf("guardian ref %d live = %d, want exactly 1", ref, got)
		}
	}
	if got := countRef(live, tombRefFlame); got != 0 {
		t.Fatalf("Flame Master live = %d before the chain, want 0", got)
	}
	// Time alone never spawns more: no nest respawn owns these rows.
	live = tombAdvance(s, 3600_000)
	if len(live) != 4 {
		t.Fatalf("live after 1h = %d, want the same 4 guardians", len(live))
	}
	if s.TombGateOpen("division", TombGateFloor6Entry) {
		t.Fatal("Floor 6 entry gate open before the Flame Master died")
	}
}

/*
================
TestTombChainFlameMasterAfterFourthGuardian
================
*/
func TestTombChainFlameMasterAfterFourthGuardian(t *testing.T) {
	s := newTombChainWorld(t, true)
	live := tombAdvance(s, 5000)
	guards := []uint32{tombRefEast, tombRefSouth, tombRefWest}
	for _, ref := range guards {
		if !s.Defeat("division", liveGid(t, live, ref), tombChainBase.Add(6*time.Second)) {
			t.Fatalf("defeat guardian %d rejected", ref)
		}
	}
	live = tombAdvance(s, 10000)
	if got := countRef(live, tombRefFlame); got != 0 {
		t.Fatalf("Flame Master live = %d after 3 of 4 guardians, want 0", got)
	}
	if !s.Defeat("division", liveGid(t, live, tombRefNorth), tombChainBase.Add(11*time.Second)) {
		t.Fatal("defeat fourth guardian rejected")
	}
	live = tombAdvance(s, 15000)
	if got := countRef(live, tombRefFlame); got != 1 {
		t.Fatalf("Flame Master live = %d after all guardians, want exactly 1", got)
	}
	// It spawns once: more time adds no second Flame Master.
	live = tombAdvance(s, 120000)
	if got := countRef(live, tombRefFlame); got != 1 {
		t.Fatalf("Flame Master live = %d later, want still 1", got)
	}
}

/*
================
TestTombChainGateOpensAndChainRearms
================
*/
func TestTombChainGateOpensAndChainRearms(t *testing.T) {
	s := newTombChainWorld(t, true)
	live := tombAdvance(s, 5000)
	for _, ref := range []uint32{tombRefEast, tombRefSouth, tombRefWest, tombRefNorth} {
		if !s.Defeat("division", liveGid(t, live, ref), tombChainBase.Add(6*time.Second)) {
			t.Fatalf("defeat guardian %d rejected", ref)
		}
		live = tombAdvance(s, 7000)
	}
	live = tombAdvance(s, 10000)
	flame := liveGid(t, live, tombRefFlame)
	if s.TombGateOpen("division", TombGateFloor6Entry) {
		t.Fatal("gate open while the Flame Master lives")
	}
	if !s.Defeat("division", flame, tombChainBase.Add(30*time.Second)) {
		t.Fatal("defeat Flame Master rejected")
	}
	if !s.TombGateOpen("division", TombGateFloor6Entry) {
		t.Fatal("gate still closed after the Flame Master died")
	}
	// The rearm window is the floor's unique respawn window (1800..3600s).
	live = tombAdvance(s, 30_000+3601_000)
	if s.TombGateOpen("division", TombGateFloor6Entry) {
		t.Fatal("gate still open after the rearm")
	}
	for _, ref := range []uint32{tombRefEast, tombRefSouth, tombRefWest, tombRefNorth} {
		if got := countRef(live, ref); got != 1 {
			t.Fatalf("guardian ref %d live = %d after rearm, want 1", ref, got)
		}
	}
	if got := countRef(live, tombRefFlame); got != 0 {
		t.Fatalf("Flame Master live = %d after rearm, want 0 (chain waits again)", got)
	}
}

/*
================
TestTombChainWaveFiresOnlyWhenFullyDead

A stage spawning Count 2 fires its trigger on the second death, not the
first: the native death handler erases the dead from the wave's live set
and the stage completes only when the set is empty.
================
*/
func TestTombChainWaveFiresOnlyWhenFullyDead(t *testing.T) {
	refs := map[uint32]monster.MonsterRef{
		tombRefWaveA:    tombChainRef(tombRefWaveA, "MOB_TQ_WAVE_A"),
		tombRefWaveBoss: tombChainRef(tombRefWaveBoss, "MOB_TQ_WAVE_BOSS"),
	}
	nests := []monster.NestRow{tombChainNest(tombRefWaveA, 100), tombChainNest(tombRefWaveBoss, 200)}
	s := NewMonsterState(monster.TemplateFromParts(refs, nests))
	s.SetTimeSource(func() time.Time { return tombChainBase })
	s.SetRandomSource(constantWord(16384))
	def := TombChainDef{Floors: []TombFloorDef{{
		Spawners: []TombSpawnerDef{
			{Name: tombWaveStageNam, Codename: "MOB_TQ_WAVE_A", Count: 2, Initialize: true, Trigger: "Test_Boss"},
			{Name: "Test_Boss", Codename: "MOB_TQ_WAVE_BOSS", Count: 1, Needed: []string{tombWaveStageNam}},
		},
	}}}
	if err := s.InstallTombChainDef(def); err != nil {
		t.Fatalf("InstallTombChainDef: %v", err)
	}
	s.StartDivision("division")
	live := tombAdvance(s, 5000)
	if got := countRef(live, tombRefWaveA); got != 2 {
		t.Fatalf("wave stage live = %d, want 2", got)
	}
	first := live[0].Gid
	if !s.Defeat("division", first, tombChainBase.Add(6*time.Second)) {
		t.Fatal("first wave defeat rejected")
	}
	live = tombAdvance(s, 10000)
	if got := countRef(live, tombRefWaveBoss); got != 0 {
		t.Fatalf("boss live = %d with one wave member left, want 0", got)
	}
	second := liveGid(t, live, tombRefWaveA)
	if !s.Defeat("division", second, tombChainBase.Add(11*time.Second)) {
		t.Fatal("second wave defeat rejected")
	}
	live = tombAdvance(s, 15000)
	if got := countRef(live, tombRefWaveBoss); got != 1 {
		t.Fatalf("boss live = %d after the wave died, want 1", got)
	}
}

/*
================
TestTombChainNotInstalledSpawnsNothing

The zeroed nests alone spawn nothing: the chain and the zeroing ship
together behind SRO_QINSHI_TOMB. With the chain installed the same
template arms (covered above); without it the floor uniques never
appear. With the flag off the tomb template is never built at all.
================
*/
func TestTombChainNotInstalledSpawnsNothing(t *testing.T) {
	s := newTombChainWorld(t, false)
	live := tombAdvance(s, 120000)
	if len(live) != 0 {
		t.Fatalf("live = %d without the chain, want 0", len(live))
	}
	if s.TombGateOpen("division", TombGateFloor6Entry) {
		t.Fatal("gate open without a chain")
	}
}
