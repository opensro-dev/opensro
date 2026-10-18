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
