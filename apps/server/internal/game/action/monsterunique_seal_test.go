/*
===========================================================================

monsterunique_seal_test.go - a guaranteed seal must match the requested tier

The native degree-9 Moon bucket also contains Star body armour. That mixed
assignment remains valid for ordinary native rolls, but must never turn the
port-only guaranteed Moon reward into another Star.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/world/monster"
)

/*
================
moonBucketStarDraw

Find a real weighted draw that selects Star body armour from the native Moon
bucket. The next draw is zero so the absolute-admission gate accepts it.
================
*/
func moonBucketStarDraw(t *testing.T, country uint8, group int) uint32 {
	t.Helper()
	for draw := uint32(0); draw < dropRollDomain; draw++ {
		calls := 0
		chosen, ok := loot.SelectEquipment(country, group, true, 90, func() (uint32, error) {
			calls++
			if calls == 1 {
				return draw, nil
			}
			return 0, nil
		})
		if ok && (chosen.Codename == "ITEM_CH_M_HEAVY_09_BA_A_RARE" ||
			chosen.Codename == "ITEM_EU_M_HEAVY_09_BA_A_RARE") {
			return draw
		}
	}
	t.Fatal("the shipped Moon bucket has no Star-body-armour witness")
	return 0
}

/*
================
TestGuaranteedMoonRerollsStarBodyArmour

For both countries, reject the mixed bucket's Star before constructing any
properties, then accept the next Moon. This fails on the unfiltered picker.
================
*/
func TestGuaranteedMoonRerollsStarBodyArmour(t *testing.T) {
	for _, country := range []uint8{0, 1} {
		rt, _, player, target := newCombatTestRuntimeAtLevel(t, 1, 20)
		installLootReferences(t, rt)
		group, ok := loot.SealGroupAt(90, enterworld.SealMoon)
		if !ok {
			t.Fatal("no degree-9 Moon group")
		}
		wrong := moonBucketStarDraw(t, country, group)
		calls := 0
		rt.DropRoll = func() (uint32, error) {
			calls++
			if calls == 1 {
				return wrong, nil
			}
			return 0, nil
		}
		item, ok := rt.prepareSealEquipment(country, group, 90, uniqueDropContext{
			mob: target, owner: player.Name, now: rt.Now(),
		})
		if !ok {
			t.Fatalf("country %d: the correct Moon redraw was lost", country)
		}
		ref, _ := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
		seal, sealed := enterworld.EquipmentSeal(ref)
		if !sealed || seal != enterworld.SealMoon || enterworld.EquipmentDegree(ref) != 9 {
			t.Fatalf("country %d: guaranteed Moon became %s (seal %d, sealed %v)", country, item.Codename, seal, sealed)
		}
	}
}

/*
================
TestGuaranteedMoonNeverSubstitutesStarOnExhaustion

A pathological stream that selects the wrong tier forever must terminate
without publishing that Star as a Moon reward.
================
*/
func TestGuaranteedMoonNeverSubstitutesStarOnExhaustion(t *testing.T) {
	rt, _, player, target := newCombatTestRuntimeAtLevel(t, 1, 20)
	installLootReferences(t, rt)
	group, ok := loot.SealGroupAt(90, enterworld.SealMoon)
	if !ok {
		t.Fatal("no degree-9 Moon group")
	}
	wrong := moonBucketStarDraw(t, 0, group)
	rt.DropRoll = constantDropRoll(wrong)
	item, ok := rt.prepareSealEquipment(0, group, 90, uniqueDropContext{
		mob: target, owner: player.Name, now: rt.Now(),
	})
	if ok {
		t.Fatalf("exhausted Moon picker substituted %s", item.Codename)
	}
}

/*
================
TestGuaranteedSealMatchesEachRequestedTier

All three tiers and both countries still construct valid same-degree seals.
================
*/
func TestGuaranteedSealMatchesEachRequestedTier(t *testing.T) {
	rt, _, player, target := newCombatTestRuntimeAtLevel(t, 1, 20)
	installLootReferences(t, rt)
	for _, country := range []uint8{0, 1} {
		for seal := 0; seal < enterworld.SealTiers; seal++ {
			group, ok := loot.SealGroupAt(90, seal)
			if !ok {
				t.Fatalf("no group for seal %d", seal)
			}
			rt.DropRoll = constantDropRoll(0)
			item, ok := rt.prepareSealEquipment(country, group, 90, uniqueDropContext{
				mob: target, owner: player.Name, now: rt.Now(),
			})
			if !ok {
				t.Fatalf("country %d seal %d produced no reward", country, seal)
			}
			ref, _ := rt.deps.ItemReferences().ItemRefByCodename(item.Codename)
			got, sealed := enterworld.EquipmentSeal(ref)
			if !sealed || got != seal || enterworld.EquipmentDegree(ref) != 9 {
				t.Fatalf("country %d seal %d produced %s (seal %d, sealed %v)", country, seal, item.Codename, got, sealed)
			}
		}
	}
}

/*
================
TestGuaranteedSealMissingReferences
================
*/
func TestGuaranteedSealMissingReferences(t *testing.T) {
	rt, _, player, _ := newCombatTestRuntime(t, 1)
	rt.deps.(*enterworld.Deps).Items = nil
	rt.DropRoll = constantDropRoll(0)
	if item, ok := rt.prepareSealEquipment(0, 25, 90, uniqueDropContext{
		mob: monster.Instance{}, owner: player.Name, now: rt.Now(),
	}); ok {
		t.Fatalf("missing references produced %s", item.Codename)
	}
}
