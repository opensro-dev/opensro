/*
===========================================================================

dropmix_test.go - the beta drop rate per item family (port-only, not native)

With the mix off every family runs DropPassRate times its native passes, as
before; a family the mix names runs its own rate, so a potion family at 1
drops as often as native while equipment keeps the beta rate.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
)

// potionFamily is the planner's first consumable category (potions).
const potionFamily = 2

/*
================
mixedKillCounts

Plans one kill at the given beta rate and mix with every roll at 0 and
returns its equipment (sword) and potion counts.
================
*/
func mixedKillCounts(t *testing.T, rate int, mix progression.DropMix) (swords, potions int) {
	t.Helper()
	rt, _, character, target := newCombatTestRuntime(t, 1)
	installSmallGoldRef(rt)
	items := rt.deps.ItemReferences().(staticItemSource)
	items["ITEM_CH_SWORD_01_A"] = &enterworld.ItemRef{RefObjID: 90000, Codename: "ITEM_CH_SWORD_01_A", TypeIDs: [4]int64{3, 1, 6, 2}}
	potionNames := map[string]bool{}
	for i, chance := range loot.ConsumableAttemptChances(potionFamily, target.Ref.Level) {
		potionNames[chance.Codename] = true
		items[chance.Codename] = &enterworld.ItemRef{RefObjID: uint32(91000 + i), Codename: chance.Codename, TypeIDs: [4]int64{3, 3, 1, 1}}
	}
	if len(potionNames) == 0 {
		t.Fatal("no potion rows at the fixture monster's level")
	}
	rt.DropRoll = constantDropRoll(0)
	rt.DropPassRate = rate
	rt.DropMix = mix
	drops, _ := rt.planMonsterKillLoot(character, target, monster.Pose{
		RegionID: target.Spawn.RegionID, X: target.Spawn.X, Y: target.Spawn.Y, Z: target.Spawn.Z,
	}, rt.Now().UnixMilli())
	for _, drop := range drops {
		switch {
		case drop.Codename == "ITEM_CH_SWORD_01_A":
			swords++
		case potionNames[drop.Codename]:
			potions++
		}
	}
	return swords, potions
}

/*
================
TestBetaDropMixRunsEachFamilyAtItsOwnRate
================
*/
func TestBetaDropMixRunsEachFamilyAtItsOwnRate(t *testing.T) {
	nativeSwords, nativePotions := mixedKillCounts(t, 1, nil)
	if nativeSwords != 1 || nativePotions != 1 {
		t.Fatalf("native kill: %d swords, %d potions; want 1 and 1", nativeSwords, nativePotions)
	}
	swords, potions := mixedKillCounts(t, 3, nil)
	if swords != 3 || potions != 3 {
		t.Fatalf("mix off at rate 3: %d swords, %d potions; want 3 and 3", swords, potions)
	}
	on, err := progression.ParseDropMix("on")
	if err != nil {
		t.Fatal(err)
	}
	swords, potions = mixedKillCounts(t, 3, on)
	if swords != 3 || potions != 1 {
		t.Fatalf("mix on at rate 3: %d swords, %d potions; want 3 and 1", swords, potions)
	}
	swords, potions = mixedKillCounts(t, 3, progression.DropMix{"equipment": 1, "potion": 2})
	if swords != 1 || potions != 2 {
		t.Fatalf("equipment=1,potion=2 at rate 3: %d swords, %d potions; want 1 and 2", swords, potions)
	}
}
