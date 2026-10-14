/*
===========================================================================

sunevent_test.go - degree-9 Sun leaves drops and shops with the flag

Both settings, on the shipped v1.150 data: off, a Sun pick drops and a shop
may sell it, as native; on, a Sun pick drops nothing and no shop offer
holds degree-9 Sun, while degree-8 Sun and degree-9 Moon stay on the shelf
and on the ground.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/loot"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestSunPickDropsNothingWithTheFlag
================
*/
func TestSunPickDropsNothingWithTheFlag(t *testing.T) {
	licensed.RequireGameData(t)
	for _, on := range []bool{false, true} {
		t.Setenv(progression.EnvSunEventOnly, map[bool]string{false: "off", true: "on"}[on])
		rt, _, _, _ := newCombatTestRuntime(t, 1)
		rt.deps.(*enterworld.Deps).Items = shippedItems(t)
		rt.SunEventOnly = progression.SunEventOnlyFromEnv()
		rt.DropRoll = func() (uint32, error) { return 0, nil }
		for codename, excluded := range map[string]bool{
			"ITEM_CH_BLADE_09_C_RARE":  true,
			"ITEM_EU_TSWORD_09_C_RARE": true,
			"ITEM_CH_BLADE_09_B_RARE":  false,
			"ITEM_CH_BLADE_08_C_RARE":  false,
			"ITEM_CH_BLADE_09_C":       false,
		} {
			_, dropped := rt.prepareSelectedDrop(loot.DropItem{Codename: codename, Count: 1}, simulation.Spawn{RegionID: 0x5F76}, "owner", time.UnixMilli(0))
			if want := !(on && excluded); dropped != want {
				t.Fatalf("on=%v %s: dropped=%v, want %v", on, codename, dropped, want)
			}
		}
	}
}

/*
================
shelvedCodenames

How many shop offers hold each item, by codename (an offer's own item and
every item its package holds).
================
*/
func shelvedCodenames(c *commerce.Catalog) map[string]int {
	held := map[string]int{}
	for _, offers := range c.Tabs {
		for _, offer := range offers {
			if offer.Ref != nil {
				held[offer.Ref.Codename]++
			}
			for _, content := range offer.Contents {
				if content.Ref != nil && (offer.Ref == nil || content.Ref.Codename != offer.Ref.Codename) {
					held[content.Ref.Codename]++
				}
			}
		}
	}
	return held
}

/*
================
TestSunLeavesTheShopsWithTheFlag

v1.150 shops list no degree-9 Sun (refshopgoods' Sun packages are the
honor shop's degree 8), so operator extra goods stand in: off, the smith
sells degree-9 Sun as configured; on, it is withheld, while degree-9 Moon
and degree-8 Sun stay on the shelf.
================
*/
func TestSunLeavesTheShopsWithTheFlag(t *testing.T) {
	licensed.RequireGameData(t)
	extra := []commerce.ExtraGood{
		{Tab: "STORE_CH_SMITH_TAB3", Codename: "ITEM_CH_SWORD_09_C_RARE", Price: 1000},
		{Tab: "STORE_CH_SMITH_TAB3", Codename: "ITEM_CH_SWORD_09_B_RARE", Price: 1000},
		{Tab: "STORE_CH_SMITH_TAB3", Codename: "ITEM_CH_SWORD_08_C_RARE", Price: 1000},
	}
	for _, on := range []bool{false, true} {
		t.Setenv(progression.EnvSunEventOnly, map[bool]string{false: "off", true: "on"}[on])
		rt, _, _, _ := newCombatTestRuntime(t, 1)
		rt.deps.(*enterworld.Deps).Items = shippedItems(t)
		rt.SunEventOnly = progression.SunEventOnlyFromEnv()
		if err := rt.ConfigureCommerce(gamedatatest.TextdataDir(t), extra); err != nil {
			t.Fatal(err)
		}
		held := shelvedCodenames(rt.Commerce)
		if sun, want := held["ITEM_CH_SWORD_09_C_RARE"], map[bool]int{false: 1, true: 0}[on]; sun != want {
			t.Fatalf("on=%v: degree-9 Sun on %d offers, want %d", on, sun, want)
		}
		if held["ITEM_CH_SWORD_09_B_RARE"] != 1 || held["ITEM_CH_SWORD_08_C_RARE"] != 1 {
			t.Fatalf("on=%v: degree-9 Moon %d, degree-8 Sun %d", on, held["ITEM_CH_SWORD_09_B_RARE"], held["ITEM_CH_SWORD_08_C_RARE"])
		}
	}
}
