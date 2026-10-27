/*
===========================================================================

sealboard_test.go - the weekly seal board's counting hook (#1641)

Port-only, not native: with no sink installed nothing counts, and the
count never follows the seal-drop notice flag or its degree floor.

===========================================================================
*/
package action

import (
	"math/rand/v2"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
recordingSealSink
================
*/
type recordingSealSink struct {
	byTier        [3]int64
	total         int64
	forced        int64
	lastCharacter int64
	lastDegree    int
	lastAt        time.Time
}

/*
================
NoteSealDrop
================
*/
func (s *recordingSealSink) NoteSealDrop(characterID int64, degree, tier int, forced bool, at time.Time) {
	s.byTier[tier]++
	s.total++
	if forced {
		s.forced++
	}
	s.lastCharacter = characterID
	s.lastDegree = degree
	s.lastAt = at
}

/*
================
TestSealBoardFromEnv
================
*/
func TestSealBoardFromEnv(t *testing.T) {
	t.Setenv(EnvSealBoard, "")
	if SealBoardFromEnv() {
		t.Fatal("unset must be off (the native default)")
	}
	t.Setenv(EnvSealBoard, "off")
	if SealBoardFromEnv() {
		t.Fatal("off must be off")
	}
	t.Setenv(EnvSealBoard, " On ")
	if !SealBoardFromEnv() {
		t.Fatal("on must be on")
	}
}

/*
================
sealTestItems

One sealed blade per tier across degrees the notice floor would hide,
plus a plain blade: the board counts every sealed degree.
================
*/
func sealTestItems() staticItemSource {
	sealed := func(codename string, class float64) *enterworld.ItemRef {
		return &enterworld.ItemRef{
			Codename:     codename,
			TypeIDs:      [4]int64{3, 1, 6, 3},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": class, "rarity": 2}),
		}
	}
	return staticItemSource{
		// Degree 5 Sun (class 15), degree 6 Star (16), degree 8 Moon (23).
		"ITEM_CH_BLADE_05_C_RARE": sealed("ITEM_CH_BLADE_05_C_RARE", 15),
		"ITEM_CH_BLADE_06_A_RARE": sealed("ITEM_CH_BLADE_06_A_RARE", 16),
		"ITEM_CH_BLADE_08_B_RARE": sealed("ITEM_CH_BLADE_08_B_RARE", 23),
		"ITEM_CH_BLADE_09_C": {
			Codename:     "ITEM_CH_BLADE_09_C",
			TypeIDs:      [4]int64{3, 1, 6, 3},
			NativeFields: enterworld.NewNativeFields(map[string]float64{"itemClass": 27, "rarity": 0}),
		},
	}
}

/*
================
TestCountSealDropsClassifiesEveryTier

The notice flag is off and every drop sits under its degree floor: the
board still counts each sealed item once, for the loot winner.
================
*/
func TestCountSealDropsClassifiesEveryTier(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), sealTestItems())
	sink := &recordingSealSink{}
	rt.SealDrops = sink
	rt.Announce.SoxDrop = false
	nowMs := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC).UnixMilli()
	rt.countSealDrops(testCharacter(), []grounditem.Item{
		{Codename: "ITEM_CH_BLADE_05_C_RARE"},
		{Codename: "ITEM_CH_BLADE_06_A_RARE"},
		{Codename: "ITEM_CH_BLADE_08_B_RARE"},
		{Codename: "ITEM_CH_BLADE_08_B_RARE"},
		{Codename: "ITEM_CH_BLADE_09_C"},
		{Codename: "ITEM_CH_MISSING"},
	}, nowMs)
	if sink.byTier != [3]int64{1, 2, 1} {
		t.Fatalf("tiers star/moon/sun = %v, want [1 2 1]", sink.byTier)
	}
	if sink.lastCharacter != testCharacter().ID {
		t.Fatalf("counted for character %d, want the winner %d", sink.lastCharacter, testCharacter().ID)
	}
	if !sink.lastAt.Equal(time.UnixMilli(nowMs).UTC()) {
		t.Fatalf("counted at %v, want %v", sink.lastAt, time.UnixMilli(nowMs).UTC())
	}
	// The last counted drop is the second degree-8 Moon (08_B_RARE);
	// the board records its degree and no drop was pity-forced (#1690).
	if sink.lastDegree != 8 || sink.forced != 0 {
		t.Fatalf("last degree %d, forced %d, want 8 and 0", sink.lastDegree, sink.forced)
	}
}

/*
================
TestCountSealDropsWithoutSinkCountsNothing
================
*/
func TestCountSealDropsWithoutSinkCountsNothing(t *testing.T) {
	rt, _ := newTestRuntime(testCharacter(), sealTestItems())
	// No sink: the native default. Must simply do nothing.
	rt.countSealDrops(testCharacter(), []grounditem.Item{{Codename: "ITEM_CH_BLADE_08_B_RARE"}}, 0)
	rt.countSealDrops(nil, []grounditem.Item{{Codename: "ITEM_CH_BLADE_08_B_RARE"}}, 0)
}

// sealSettlementKills bounds the seeded hunt for a sealed settlement drop.
const sealSettlementKills = 20000

/*
================
settleSealKills

Settles the combat fixture's fatal kill until one settlement pays a
sealed item (the drop roll is seeded, so the hunt is the same on every
run). Returns the sink, every drop the settlements produced, and whether
a sealed drop appeared at all.
================
*/
func settleSealKills(t *testing.T, counted bool, gmSpawned bool) (*recordingSealSink, []grounditem.Item, bool) {
	t.Helper()
	rt, clock, killer, mob := newCombatTestRuntimeAtLevel(t, 100, 85)
	*killer.Level = 85
	*killer.MaxLevel = 85
	items := shippedItems(t)
	rt.deps.(*enterworld.Deps).Items = items
	sink := &recordingSealSink{}
	rt.SealDrops = sink
	source := rand.New(rand.NewPCG(1641, 9))
	rt.DropRoll = func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
	rt.DropPassRate, rt.DropCap, rt.RareRate = 20, 16, 5
	gid := enterworld.ObjectIDForCharacter(killer)
	impact := simulation.MonsterDamageResult{
		Fatal:    true,
		Instance: mob,
		Contributions: []simulation.MonsterContribution{
			{CreditGID: gid, Damage: mob.EffectiveMaxHP()},
		},
	}
	impact.Instance.GMSpawned = gmSpawned
	roster := rt.monsterRewardRoster(testDivision, killer, clock.NowMs())
	var produced []grounditem.Item
	sealedSeen := false
	for kill := 0; kill < sealSettlementKills && !sealedSeen; kill++ {
		settlement := rt.settleMonsterInsideDoorCounted(testDivision, killer, roster, impact, monster.Pose{}, clock.NowMs(), counted)
		produced = append(produced, settlement.drops...)
		for _, drop := range settlement.drops {
			if ref, ok := rt.deps.ItemReferences().ItemRefByCodename(drop.Codename); ok && ref != nil {
				if _, sealed := enterworld.EquipmentSeal(ref); sealed {
					sealedSeen = true
				}
			}
		}
	}
	return sink, produced, sealedSeen
}

/*
================
TestSettlementCountsSealDrops

The count through a real settlement equals the sealed drops the
settlements produced, tier for tier. Removing the settlement hook
leaves this at zero while the seal notices stay green.
================
*/
func TestSettlementCountsSealDrops(t *testing.T) {
	sink, produced, sealedSeen := settleSealKills(t, true, false)
	if !sealedSeen {
		t.Fatalf("no sealed drop in %d seeded settlements; the fixture cannot prove the hook", sealSettlementKills)
	}
	items := shippedItems(t)
	want := [3]int64{}
	for _, drop := range produced {
		ref, ok := items.ItemRefByCodename(drop.Codename)
		if !ok || ref == nil {
			continue
		}
		if tier, sealed := enterworld.EquipmentSeal(ref); sealed {
			want[tier]++
		}
	}
	if sink.byTier != want {
		t.Fatalf("counted tiers star/moon/sun = %v, want %v from the settlements' own drops", sink.byTier, want)
	}
}

/*
================
TestGMSettlementCountsNothing

The GM kill path settles with counting off: settlements that pay sealed
items still never reach the board.
================
*/
func TestGMSettlementCountsNothing(t *testing.T) {
	sink, _, sealedSeen := settleSealKills(t, false, false)
	if !sealedSeen {
		t.Fatalf("no sealed drop in %d seeded settlements; the fixture cannot prove the exclusion", sealSettlementKills)
	}
	if sink.total != 0 {
		t.Fatalf("GM settlements counted %d seal drops", sink.total)
	}
}

/*
================
TestGMSpawnedMonsterSealDropIsNotCounted

A GM-spawned monster killed through the ordinary counted path pays its
loot as usual, but the board counts none of it: the loot is conjured,
not hunted.
================
*/
func TestGMSpawnedMonsterSealDropIsNotCounted(t *testing.T) {
	sink, _, sealedSeen := settleSealKills(t, true, true)
	if !sealedSeen {
		t.Fatalf("no sealed drop in %d seeded settlements; the fixture cannot prove the exclusion", sealSettlementKills)
	}
	if sink.total != 0 {
		t.Fatalf("a GM-spawned monster's seal drops counted %d through a normal kill", sink.total)
	}
}
