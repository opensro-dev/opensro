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
	// The default chain def also carries Floor 6 (#2373), which resolves
	// by codename and explicit point: its refs must exist here too.
	for codename, id := range tombFloor6Refs() {
		refs[id] = tombChainRef(id, codename)
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

// Floor 6 chain codenames and their test ref ids (region 0x8002).
const (
	tombRefBlackSnake = uint32(31)
	tombRefWhiteSnake = uint32(32)
)

var tombSouthCodenames = []string{
	"MOB_TQ_SNAKEGENERAL", "MOB_TQ_SNAKEGENERAL_CLON",
	"MOB_TQ_SNAKEGENERAL_CLON2", "MOB_TQ_SNAKEGENERAL_CLON3",
}

/*
================
tombFloor6Refs

Every Floor 6 chain codename with a stable ref id for the scripted
templates.
================
*/
func tombFloor6Refs() map[string]uint32 {
	refs := map[string]uint32{
		"MOB_TQ_BLACKSNAKE": tombRefBlackSnake,
		"MOB_TQ_WHITESNAKE": tombRefWhiteSnake,
	}
	next := uint32(41)
	for _, codename := range tombSouthCodenames {
		refs[codename] = next
		next++
	}
	for _, codename := range tombWestCodenames {
		refs[codename] = next
		next++
	}
	return refs
}

var tombWestCodenames = []string{
	"MOB_TQ_TOMBBUGGHOST_CLON2", "MOB_TQ_SNAKESLAVE_CLON2",
	"MOB_TQ_TOMBBUGGHOST_CLON", "MOB_TQ_SNAKEWOMAN_CLON2",
	"MOB_TQ_TOMBBUGGHOST", "MOB_TQ_SNAKESLAVE_CLON",
	"MOB_TQ_SNAKEWOMAN_CLON", "MOB_TQ_SNAKEDEMON_CLON2",
	"MOB_TQ_SNAKESLAVE", "MOB_TQ_SNAKEDEMON_CLON",
	"MOB_TQ_SNAKEMAN_CLON2", "MOB_TQ_SNAKEWOMAN",
	"MOB_TQ_SNAKEMAN_CLON", "MOB_TQ_SNAKEDEMON", "MOB_TQ_SNAKEMAN",
}

const tombFloor6Region = uint16(0x8002)

/*
================
newTombFloor6World

A MonsterState carrying the Floor 5 harness template plus every Floor 6
chain codename as a ref (the Floor 6 spawners resolve to their native
stage points, so they need no nests), with the default chain installed.
================
*/
func newTombFloor6World(t *testing.T) *MonsterState {
	t.Helper()
	refs := map[uint32]monster.MonsterRef{
		tombRefEast:       tombChainRef(tombRefEast, "MOB_TQ_EASTGUARDIAN"),
		tombRefSouth:      tombChainRef(tombRefSouth, "MOB_TQ_SOUTHGUARDIAN"),
		tombRefWest:       tombChainRef(tombRefWest, "MOB_TQ_WESTGUARDIAN"),
		tombRefNorth:      tombChainRef(tombRefNorth, "MOB_TQ_NORTHGUARDIAN"),
		tombRefFlame:      tombChainRef(tombRefFlame, "MOB_TQ_FLAMEMASTER"),
		tombRefBlackSnake: tombChainRef(tombRefBlackSnake, "MOB_TQ_BLACKSNAKE"),
		tombRefWhiteSnake: tombChainRef(tombRefWhiteSnake, "MOB_TQ_WHITESNAKE"),
	}
	nests := []monster.NestRow{
		tombChainNest(tombRefEast, 100),
		tombChainNest(tombRefSouth, 200),
		tombChainNest(tombRefWest, 300),
		tombChainNest(tombRefNorth, 400),
		tombChainNest(tombRefFlame, 500),
	}
	next := uint32(41)
	for _, codename := range append(append([]string{}, tombSouthCodenames...), tombWestCodenames...) {
		refs[next] = tombChainRef(next, codename)
		next++
	}
	s := NewMonsterState(monster.TemplateFromParts(refs, nests))
	now := tombChainBase
	s.SetTimeSource(func() time.Time { return now })
	s.SetRandomSource(constantWord(16384))
	if err := s.InstallTombChain(); err != nil {
		t.Fatalf("InstallTombChain: %v", err)
	}
	s.StartDivision("division")
	return s
}

/*
================
tombAdvanceFloor6

Advances the population in one-second steps to the offset and returns
the live Floor 6 instances.
================
*/
func tombAdvanceFloor6(s *MonsterState, offsetMs int64) []monster.Instance {
	target := tombChainBase.Add(time.Duration(offsetMs) * time.Millisecond)
	for step := tombChainBase.Add(time.Second); !step.After(target); step = step.Add(time.Second) {
		s.AdvancePopulation(step.UnixMilli())
	}
	return s.InstancesInRegions("division", []uint16{tombFloor6Region})
}

/*
================
countCodename
================
*/
func countCodename(live []monster.Instance, codename string) int {
	n := 0
	for _, in := range live {
		if in.Ref.Codename == codename {
			n++
		}
	}
	return n
}

/*
================
liveGidsByCodename
================
*/
func liveGidsByCodename(live []monster.Instance, codename string) []uint32 {
	var gids []uint32
	for _, in := range live {
		if in.Ref.Codename == codename {
			gids = append(gids, in.Gid)
		}
	}
	return gids
}

/*
================
TestTombChainGateWaitsForTheWholeStage

A Floor 6 stage is several codename spawners under one trigger
(Chins_Floor6_South: the SNAKEGENERAL and its three clones). The
stage's portal opens only when every spawner of the stage is fully
dead, never when the first one empties (#2373).
================
*/
func TestTombChainGateWaitsForTheWholeStage(t *testing.T) {
	refs := map[uint32]monster.MonsterRef{}
	var nests []monster.NestRow
	for i, codename := range tombSouthCodenames {
		id := uint32(41 + i)
		refs[id] = tombChainRef(id, codename)
		nests = append(nests, monster.NestRow{
			SpawnPoint:     monster.SpawnPoint{RefObjID: id, RegionID: tombFloor6Region, X: float64(i * 10), Y: 132, Z: -8213},
			PolicyPinned:   true,
			RetailEvidence: true,
		})
	}
	s := NewMonsterState(monster.TemplateFromParts(refs, nests))
	now := tombChainBase
	s.SetTimeSource(func() time.Time { return now })
	s.SetRandomSource(constantWord(16384))
	def := TombChainDef{Floors: []TombFloorDef{{Spawners: []TombSpawnerDef{
		{Name: "Test_South_A", Codename: tombSouthCodenames[0], Count: 1, Initialize: true, Trigger: "Chins_Floor6_Portal2"},
		{Name: "Test_South_B", Codename: tombSouthCodenames[1], Count: 1, Initialize: true, Trigger: "Chins_Floor6_Portal2"},
		{Name: "Test_South_C", Codename: tombSouthCodenames[2], Count: 1, Initialize: true, Trigger: "Chins_Floor6_Portal2"},
		{Name: "Test_South_D", Codename: tombSouthCodenames[3], Count: 1, Initialize: true, Trigger: "Chins_Floor6_Portal2"},
	}}}}
	if err := s.InstallTombChainDef(def); err != nil {
		t.Fatalf("InstallTombChainDef: %v", err)
	}
	s.StartDivision("division")
	live := tombAdvanceFloor6(s, 5000)
	if len(live) != 4 {
		t.Fatalf("south stage live = %d, want 4", len(live))
	}
	for i, codename := range tombSouthCodenames[:3] {
		gid := liveGidsByCodename(live, codename)[0]
		if !s.Defeat("division", gid, tombChainBase.Add(time.Duration(6+i)*time.Second)) {
			t.Fatalf("defeat %s rejected", codename)
		}
		if s.TombGateOpen("division", "Chins_Floor6_Portal2") {
			t.Fatalf("Portal2 open after %d of 4 stage spawners died", i+1)
		}
	}
	gid := liveGidsByCodename(live, tombSouthCodenames[3])[0]
	if !s.Defeat("division", gid, tombChainBase.Add(9*time.Second)) {
		t.Fatal("defeat last south spawner rejected")
	}
	if !s.TombGateOpen("division", "Chins_Floor6_Portal2") {
		t.Fatal("Portal2 still shut after the whole stage died")
	}
}

/*
================
TestTombChainFloor6StagesArmFireAndRearm

The default chain's Floor 6 (#2373): the four stages arm together -
East and North one snake each (the Medusa pair), South four, West
fifteen codenames five each. Every stage's portal stays shut until its
whole wave is dead; the arena exit follows the first entry portal; the
whole floor comes back on the rearm window.
================
*/
func TestTombChainFloor6StagesArmFireAndRearm(t *testing.T) {
	s := newTombFloor6World(t)
	live := tombAdvanceFloor6(s, 5000)
	for codename, want := range map[string]int{
		"MOB_TQ_BLACKSNAKE": 1,
		"MOB_TQ_WHITESNAKE": 1,
	} {
		if got := countCodename(live, codename); got != want {
			t.Fatalf("%s live = %d at arm, want %d", codename, got, want)
		}
	}
	for _, codename := range tombSouthCodenames {
		if got := countCodename(live, codename); got != 1 {
			t.Fatalf("south %s live = %d at arm, want 1", codename, got)
		}
	}
	for _, codename := range tombWestCodenames {
		if got := countCodename(live, codename); got != 5 {
			t.Fatalf("west %s live = %d at arm, want 5", codename, got)
		}
	}
	if len(live) != 81 {
		t.Fatalf("Floor 6 live = %d at arm, want 81 (1+1+4+75)", len(live))
	}
	for _, gate := range []string{"Chins_Floor6_Portal1", "Chins_Floor6_Portal2", "Chins_Floor6_Portal3", "Chins_Floor6_Portal5", "Chins_Floor6_Portal4"} {
		if s.TombGateOpen("division", gate) {
			t.Fatalf("%s open before any stage died", gate)
		}
	}

	kill := func(live []monster.Instance, codename string, atSec int64) {
		t.Helper()
		for _, gid := range liveGidsByCodename(live, codename) {
			if !s.Defeat("division", gid, tombChainBase.Add(time.Duration(atSec)*time.Second)) {
				t.Fatalf("defeat %s rejected", codename)
			}
		}
	}

	// East (the black snake): its portal and, through it, the exit open.
	kill(live, "MOB_TQ_BLACKSNAKE", 6)
	if !s.TombGateOpen("division", "Chins_Floor6_Portal1") {
		t.Fatal("Portal1 shut after the East stage died")
	}
	if !s.TombGateOpen("division", "Chins_Floor6_Portal4") {
		t.Fatal("arena exit (Portal4) shut after an entry portal opened")
	}
	for _, gate := range []string{"Chins_Floor6_Portal2", "Chins_Floor6_Portal3", "Chins_Floor6_Portal5"} {
		if s.TombGateOpen("division", gate) {
			t.Fatalf("%s open before its stage died", gate)
		}
	}

	// West: 74 of 75 dead still holds the portal; the 75th opens it.
	live = tombAdvanceFloor6(s, 10000)
	var westGids []uint32
	for _, codename := range tombWestCodenames {
		westGids = append(westGids, liveGidsByCodename(live, codename)...)
	}
	if len(westGids) != 75 {
		t.Fatalf("west wave live = %d, want 75", len(westGids))
	}
	for _, gid := range westGids[:74] {
		if !s.Defeat("division", gid, tombChainBase.Add(11*time.Second)) {
			t.Fatalf("defeat west gid %d rejected", gid)
		}
	}
	if s.TombGateOpen("division", "Chins_Floor6_Portal3") {
		t.Fatal("Portal3 open with one west instance left")
	}
	if !s.Defeat("division", westGids[74], tombChainBase.Add(12*time.Second)) {
		t.Fatal("defeat last west instance rejected")
	}
	if !s.TombGateOpen("division", "Chins_Floor6_Portal3") {
		t.Fatal("Portal3 shut after the west wave died")
	}

	// South and North.
	live = tombAdvanceFloor6(s, 14000)
	for _, codename := range tombSouthCodenames {
		kill(live, codename, 15)
	}
	kill(live, "MOB_TQ_WHITESNAKE", 16)
	for _, gate := range []string{"Chins_Floor6_Portal2", "Chins_Floor6_Portal5"} {
		if !s.TombGateOpen("division", gate) {
			t.Fatalf("%s shut after its stage died", gate)
		}
	}

	// Rearm: past the last death plus the outer window, every stage is
	// back and every portal shut again.
	live = tombAdvanceFloor6(s, 16_000+3601_000)
	if len(live) != 81 {
		t.Fatalf("Floor 6 live after rearm = %d, want 81 (1+1+4+75)", len(live))
	}
	for _, gate := range []string{"Chins_Floor6_Portal1", "Chins_Floor6_Portal2", "Chins_Floor6_Portal3", "Chins_Floor6_Portal5", "Chins_Floor6_Portal4"} {
		if s.TombGateOpen("division", gate) {
			t.Fatalf("%s still open after the rearm", gate)
		}
	}
}
