/*
===========================================================================

shoponly_test.go - tests for the degree-9 shop-only rule in prepareSelectedDrop

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
TestDegree9AlchemyComesOnlyFromTheShop

With the rule on, a drop that picked a degree-9 Lucky Powder, magic stone
or attribute stone, or Luck/Steady stone (by the shipped item's own ItemClass) drops nothing;
degree 8 and elixirs still drop. Off, every one of them drops.
================
*/
func TestDegree9AlchemyComesOnlyFromTheShop(t *testing.T) {
	clientdata.RequireGameData(t)
	rt, clock, _, _ := newCombatTestRuntime(t, 1)
	rt.deps.(*enterworld.Deps).Items = shippedItems(t)
	cases := map[string]bool{
		"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09":    true,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_09":         true,
		"ITEM_ETC_ARCHEMY_ATTRSTONE_PA_09":           true,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_09":        true,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_08":       false,
		"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_08":    false,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_08":         false,
		"ITEM_ETC_ARCHEMY_ATTRSTONE_PA_08":           false,
		"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B": false,
	}
	drops := func(codename string) bool {
		_, ok := rt.prepareSelectedDrop(loot.DropItem{Codename: codename, Count: 1}, simulation.Spawn{RegionID: 0x62A8}, "", clock.Now())
		return ok
	}
	for codename := range cases {
		if !drops(codename) {
			t.Fatalf("off: %s did not drop", codename)
		}
	}
	t.Setenv(progression.EnvBetaShopOnlyDegree9, "on")
	rt.ShopOnly = progression.BetaShopOnlyDegree9FromEnv()
	for codename, shopOnly := range cases {
		if dropped := drops(codename); dropped == shopOnly {
			t.Fatalf("on: %s dropped=%v, want shop-only=%v", codename, dropped, shopOnly)
		}
	}
}
