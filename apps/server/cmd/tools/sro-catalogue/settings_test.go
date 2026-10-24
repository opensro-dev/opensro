/*
===========================================================================

settings_test.go - a catalogue built with no settings describes a default
deploy

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/catalogue"
	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestCatalogueResolvesOptionalPopulationSetting

resolveShardSettings writes every unset catalogue setting into the process
environment, so each one is scoped to the subtest first; otherwise the
deployment defaults leak into the package's later tests.
================
*/
func TestCatalogueResolvesOptionalPopulationSetting(t *testing.T) {
	for _, value := range []string{"", "off", "on"} {
		t.Run(value, func(t *testing.T) {
			for _, s := range shardsettings.Settings {
				if s.Catalogue {
					t.Setenv(s.Env, "")
				}
			}
			t.Setenv(monster.EnvCap110Monsters, value)
			if err := resolveShardSettings(); err != nil {
				t.Fatal(err)
			}
			if got := monster.Cap110Enabled(); got != (value == "on") {
				t.Fatalf("population flag = %t for %q", got, value)
			}
		})
	}
}

/*
================
TestUnsetSettingsTakeTheDeploymentDefaults

With every setting the catalogue reads left unset, the readers see what
sro-nomad deploys: native reinforcement ceiling (no +11 cap), beta growth,
the seal rates and the elixir rule. An operator's explicit value still
overrides the deployment default.
================
*/
func TestUnsetSettingsTakeTheDeploymentDefaults(t *testing.T) {
	for _, s := range shardsettings.Settings {
		if s.Catalogue {
			t.Setenv(s.Env, "")
		}
	}
	if err := resolveShardSettings(); err != nil {
		t.Fatal(err)
	}
	if cap, _ := alchemy.ReinforceMaxPlusFromEnv(); cap != 0 {
		t.Fatalf("unset %s became cap %d, want 0 (native ceiling)", alchemy.EnvReinforceMaxPlus, cap)
	}
	if !progression.BetaGrowthFromEnv().Enabled || !progression.BetaSealRatesFromEnv().Enabled() ||
		!progression.ElixirDropsFromEnv().Off() || !progression.BetaShopOnlyDegree9FromEnv().Enabled() {
		t.Fatal("an unset catalogue setting did not take its deployment default")
	}
	if rates, _ := alchemy.ReinforceRatesFromEnv(); rates == nil {
		t.Fatal("the alchemy rates stayed native")
	}
}

/*
================
TestStackSizesPublishTheShardsCaps

GH #891: with SRO_STACK_SIZES unset the catalogue takes the deployment
default, so the published maxStack rows are the caps the shard runs. An
explicit value still wins, and "off" publishes the native caps - the
GameWorld's own unset staying native is the #838 contract, unchanged.
================
*/
func TestStackSizesPublishTheShardsCaps(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	native := enterworld.NewTextdataItems(dir)
	nativeCap := func(codename string) int64 {
		ref, ok := native.ItemRefByCodename(codename)
		if !ok {
			t.Fatalf("no %s reference", codename)
		}
		return int64(ref.NativeFields.Get("maxStack"))
	}
	for _, c := range []struct {
		name    string
		setting string
		arrow   int64
		powder  int64
		elixir  int64
	}{
		{"unset takes the deployment default", "", 2500, 500, nativeCap("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A")},
		{"explicit off stays native", "off",
			nativeCap("ITEM_ETC_AMMO_ARROW_01"), nativeCap("ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09"),
			nativeCap("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A")},
		{"explicit sizes win", "elixir=50",
			nativeCap("ITEM_ETC_AMMO_ARROW_01"), nativeCap("ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09"), 50},
	} {
		t.Run(c.name, func(t *testing.T) {
			for _, s := range shardsettings.Settings {
				if s.Catalogue {
					t.Setenv(s.Env, "")
				}
			}
			t.Setenv(enterworld.EnvStackSizes, c.setting)
			if err := resolveShardSettings(); err != nil {
				t.Fatal(err)
			}
			items, err := shardItems(dir)
			if err != nil {
				t.Fatal(err)
			}
			published := map[string]int64{}
			for _, item := range catalogue.BuildItems(catalogue.ItemSource{
				Refs: itemRefs(items), Usable: action.ItemUseSupported,
			}) {
				published[item.Codename] = item.MaxStack
			}
			if got := published["ITEM_ETC_AMMO_ARROW_01"]; got != c.arrow {
				t.Errorf("arrow maxStack = %d, want %d", got, c.arrow)
			}
			if got := published["ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09"]; got != c.powder {
				t.Errorf("lucky powder maxStack = %d, want %d", got, c.powder)
			}
			if got := published["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"]; got != c.elixir {
				t.Errorf("elixir maxStack = %d, want %d", got, c.elixir)
			}
		})
	}
}

/*
================
TestUnsetShopSettingPublishesTheShardsGoods

With SRO_SHOP_EXTRA_GOODS unset, the published shops are the ones a
default deploy sells: the smith's elixir at 1,000,000 is listed.
================
*/
func TestUnsetShopSettingPublishesTheShardsGoods(t *testing.T) {
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	for _, s := range shardsettings.Settings {
		if s.Catalogue {
			t.Setenv(s.Env, "")
		}
	}
	if err := resolveShardSettings(); err != nil {
		t.Fatal(err)
	}
	shops, err := shardShops(dir, items)
	if err != nil {
		t.Fatal(err)
	}
	wantPrices := map[uint32]uint64{}
	for codename, price := range map[string]uint64{
		"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A": 1000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09":   25000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09":      50000000,
	} {
		ref, ok := items.ItemRefByCodename(codename)
		if !ok {
			t.Fatalf("no %s reference", codename)
		}
		wantPrices[ref.RefObjID] = price
	}
	found := map[uint32]bool{}
	for _, offers := range shops.Tabs {
		for _, offer := range offers {
			if offer.Ref != nil && wantPrices[offer.Ref.RefObjID] == offer.Price {
				found[offer.Ref.RefObjID] = true
			}
		}
	}
	if len(found) != len(wantPrices) {
		t.Fatalf("an unset SRO_SHOP_EXTRA_GOODS matched %d/%d default extra shop prices", len(found), len(wantPrices))
	}
}
