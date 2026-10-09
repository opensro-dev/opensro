/*
===========================================================================

dropcap_test.go - the beta per-kill item cap (port-only, not native)

capKillDrops keeps a unique's prepass and every gold heap, and a uniform
random subset of the other planned items up to the cap. The planner applies
it before admission, so a capped kill leaves at most the cap in ordinary
items however high the drop rate grows its capacity.

===========================================================================
*/
package action

import (
	"errors"
	"math/rand/v2"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
plannedDrops

uniques unique-prepass items, then ordinary items interleaved with a gold
heap every fourth slot. RefObjID numbers each item by its planned index.
================
*/
func plannedDrops(uniques, total int) []grounditem.Item {
	items := make([]grounditem.Item, total)
	for i := range items {
		items[i].RefObjID = uint32(i)
		if i >= uniques && i%4 == 3 {
			items[i].GoldAmount = 100
		}
	}
	return items
}

/*
================
TestCapKillDropsKeepsUniquesAndGold
================
*/
func TestCapKillDropsKeepsUniquesAndGold(t *testing.T) {
	source := rand.New(rand.NewPCG(1, 2))
	roll := func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
	planned := plannedDrops(3, 40)
	if kept, ok := capKillDrops(planned, 3, 0, roll); !ok || len(kept) != len(planned) {
		t.Fatalf("cap 0 changed the plan: %d of %d", len(kept), len(planned))
	}
	kept, ok := capKillDrops(planned, 3, 5, roll)
	if !ok {
		t.Fatal("capped plan failed")
	}
	uniques, gold, ordinary, last := 0, 0, 0, -1
	for _, item := range kept {
		if int(item.RefObjID) <= last {
			t.Fatalf("kept items left planned order: %d after %d", item.RefObjID, last)
		}
		last = int(item.RefObjID)
		switch {
		case item.RefObjID < 3:
			uniques++
		case item.IsGold():
			gold++
		default:
			ordinary++
		}
	}
	wantGold := 0
	for _, item := range planned[3:] {
		if item.IsGold() {
			wantGold++
		}
	}
	if uniques != 3 || gold != wantGold || ordinary != 5 {
		t.Fatalf("kept %d uniques, %d gold, %d ordinary; want 3, %d, 5", uniques, gold, ordinary, wantGold)
	}
	small := plannedDrops(0, 6)
	if kept, ok := capKillDrops(small, 0, 16, roll); !ok || len(kept) != len(small) {
		t.Fatalf("a plan under the cap was cut: %d of %d", len(kept), len(small))
	}
}

/*
================
TestCapKillDropsIsUniform

Every ordinary item survives a 4-of-12 cut about a third of the time; the
planner's fill order (gold, equipment, then each consumable family) earns
no advantage.
================
*/
func TestCapKillDropsIsUniform(t *testing.T) {
	const trials, keep = 30000, 4
	source := rand.New(rand.NewPCG(7, 11))
	roll := func() (uint32, error) { return source.Uint32N(dropRollDomain), nil }
	planned := make([]grounditem.Item, 12)
	for i := range planned {
		planned[i].RefObjID = uint32(i)
	}
	counts := make([]int, len(planned))
	for range trials {
		kept, ok := capKillDrops(planned, 0, keep, roll)
		if !ok || len(kept) != keep {
			t.Fatalf("cut kept %d", len(kept))
		}
		for _, item := range kept {
			counts[item.RefObjID]++
		}
	}
	want := float64(trials*keep) / float64(len(planned))
	for i, n := range counts {
		if deviation := (float64(n) - want) / want; deviation > 0.05 || deviation < -0.05 {
			t.Fatalf("item %d kept %d times, want about %.0f", i, n, want)
		}
	}
}

/*
================
TestCapKillDropsRejectsBiasedRolls

A 3-way choice accepts rolls below 32766 only; 32766 and 32767 are drawn
again. A failed or out-of-domain roll fails the plan, and so does a source
that keeps landing in the rejected range (it must not spin forever).
================
*/
func TestCapKillDropsRejectsBiasedRolls(t *testing.T) {
	planned := plannedDrops(0, 3)
	planned[3-1].GoldAmount = 0
	rolls := []uint32{32767, 32766, 1}
	used := 0
	roll := func() (uint32, error) {
		value := rolls[used]
		used++
		return value, nil
	}
	kept, ok := capKillDrops(planned, 0, 1, roll)
	if !ok || used != 3 || len(kept) != 1 || kept[0].RefObjID != 1 {
		t.Fatalf("rejection sampling: ok=%v used=%d kept=%+v", ok, used, kept)
	}
	if _, ok := capKillDrops(planned, 0, 1, func() (uint32, error) { return 0, errors.New("no roll") }); ok {
		t.Fatal("a failed roll was accepted")
	}
	if _, ok := capKillDrops(planned, 0, 1, func() (uint32, error) { return dropRollDomain, nil }); ok {
		t.Fatal("an out-of-domain roll was accepted")
	}
	if _, ok := capKillDrops(planned, 0, 1, nil); ok {
		t.Fatal("a missing roll was accepted")
	}
	stuck := 0
	if _, ok := capKillDrops(planned, 0, 1, func() (uint32, error) { stuck++; return 32767, nil }); ok || stuck != maxCapRejections {
		t.Fatalf("a source stuck in the rejected range: ok=%v draws=%d", ok, stuck)
	}
}

/*
================
TestCappedKillLeavesAtMostTheCap

A level-80 kill at the beta rate 20 leaves at most DropCap ordinary items
over many kills, and the uncapped planner can leave more.
================
*/
func TestCappedKillLeavesAtMostTheCap(t *testing.T) {
	const kills, dropCap = 400, 4
	items := shippedItems(t)
	rt, _, c, target := newCombatTestRuntimeAtLevel(t, 1, yieldProbeLevel)
	rt.deps.(*enterworld.Deps).Items = items
	level := int64(yieldProbeLevel)
	c.Level = &level
	source := rand.New(rand.NewPCG(yieldProbeSeed, 20))
	rt.DropRoll = func() (uint32, error) { return source.Uint32N(yieldProbeRollRange), nil }
	rt.DropPassRate = 20
	pose := monster.Pose{RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z}
	maxOrdinary := func() int {
		most := 0
		for range kills {
			ordinary := 0
			for _, drop := range rt.planMonsterKillLoot(c, target, pose, rt.Now().UnixMilli()) {
				if !drop.IsGold() {
					ordinary++
				}
			}
			most = max(most, ordinary)
		}
		return most
	}
	if most := maxOrdinary(); most <= dropCap {
		t.Fatalf("uncapped kills never passed %d items (most %d); the fixture cannot show the cap", dropCap, most)
	}
	rt.DropCap = dropCap
	if most := maxOrdinary(); most > dropCap {
		t.Fatalf("a capped kill left %d ordinary items, cap %d", most, dropCap)
	}
}
