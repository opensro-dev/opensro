/*
===========================================================================

shops_test.go - the published shops are the shard's shops

The site's NPC shop tabs come from the same setting and loader the
GameWorld sells from (shardShops, commerce.LoadWithExtraGoods): with
SRO_SHOP_EXTRA_GOODS off they are the v1.150 stock, on they also list the
operator's goods (the elixirs at 1,000,000, the degree 9 stones).

===========================================================================
*/
package main

import (
	"testing"

	"opensro.online/server/internal/catalogue"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
publishedGoods

Every published NPC tab's goods, keyed by tab id; NPCs that share a tab
publish it identically.
================
*/
func publishedGoods(t *testing.T, npcs []catalogue.Npc) map[int32][]catalogue.NpcGood {
	t.Helper()
	tabs := map[int32][]catalogue.NpcGood{}
	for _, npc := range npcs {
		for _, tab := range npc.Tabs {
			tabs[tab.ID] = tab.Goods
		}
	}
	return tabs
}

/*
================
TestPublishedShopsAreTheShardsShops

For "off" and "on": every published tab lists exactly the offers the
shard's catalogue holds for it (the GameWorld's loader, from the same
parse), and only "on" publishes the smith's elixir at 1,000,000 and the
grocer's degree 9 STR stone at 2,000,000.
================
*/
func TestPublishedShopsAreTheShardsShops(t *testing.T) {
	tier.Long(t, "the published shops are the shard's shipped shops")
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	priced := map[string]uint64{
		"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A": 1000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_STR_09":         2000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09":   25000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09":      50000000,
	}
	for _, setting := range []string{"off", "on"} {
		t.Setenv(commerce.EnvShopExtraGoods, setting)
		// The roster and both catalogues read the same environment, as the
		// GameWorld and the catalogue tool do when they load.
		roster := simulation.LoadNpcWorldRoster(dir)
		published, err := shardShops(dir, items)
		if err != nil {
			t.Fatalf("%s: %v", setting, err)
		}
		extra, err := commerce.ExtraGoodsFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		shard, err := commerce.LoadWithExtraGoods(dir, items, extra)
		if err != nil {
			t.Fatal(err)
		}
		tabs := publishedGoods(t, catalogue.BuildNpcs(catalogue.NpcSource{Roster: roster, Shops: published}))
		if len(tabs) == 0 {
			t.Fatal("no shop tab was published")
		}
		found := map[string]bool{}
		for id, goods := range tabs {
			var want []commerce.Offer
			for _, offer := range shard.Tabs[id] {
				if offer.Ref != nil {
					want = append(want, offer)
				}
			}
			if len(goods) != len(want) {
				t.Fatalf("%s: tab %d publishes %d goods, the shard sells %d", setting, id, len(goods), len(want))
			}
			for i, good := range goods {
				if good.ItemID != want[i].Ref.RefObjID || good.Price != want[i].Price {
					t.Fatalf("%s: tab %d good %d is %d at %d, the shard sells %d at %d", setting, id, i,
						good.ItemID, good.Price, want[i].Ref.RefObjID, want[i].Price)
				}
				for codename, price := range priced {
					if ref, ok := items.ItemRefByCodename(codename); ok && ref.RefObjID == good.ItemID && good.Price == price {
						found[codename] = true
					}
				}
			}
		}
		for codename := range priced {
			if found[codename] != (setting == "on") {
				t.Fatalf("%s: %s published=%v", setting, codename, found[codename])
			}
		}
	}
}
