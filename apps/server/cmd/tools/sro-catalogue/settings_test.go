/*
===========================================================================

settings_test.go - a catalogue built with no settings describes a default
deploy

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/config/shardsettings"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/alchemy"
	"opensro.online/server/internal/game/progression"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestUnsetSettingsTakeTheDeploymentDefaults

With every setting the catalogue reads left unset, the readers see what
sro-nomad deploys: the +11 cap, beta growth, the seal rates and the
elixir rule. An operator's explicit "off" still publishes the native rule.
================
*/
func TestUnsetSettingsTakeTheDeploymentDefaults(t *testing.T) {
	for _, s := range shardsettings.Settings {
		if s.Catalogue {
			t.Setenv(s.Env, "")
		}
	}
	t.Setenv(alchemy.EnvReinforceMaxPlus, "off")
	if err := resolveShardSettings(); err != nil {
		t.Fatal(err)
	}
	if cap, _ := alchemy.ReinforceMaxPlusFromEnv(); cap != 0 {
		t.Fatalf("an explicit off became cap %d", cap)
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
	elixir, ok := items.ItemRefByCodename("ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A")
	if !ok {
		t.Fatal("no elixir reference")
	}
	for _, offers := range shops.Tabs {
		for _, offer := range offers {
			if offer.Ref != nil && offer.Ref.RefObjID == elixir.RefObjID && offer.Price == 1000000 {
				return
			}
		}
	}
	t.Fatal("an unset SRO_SHOP_EXTRA_GOODS published the v1.150 stock only")
}
