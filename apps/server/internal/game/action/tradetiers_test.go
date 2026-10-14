/*
===========================================================================

tradetiers_test.go - trade stars by cargo gold, and Jangan route profit

Port-only, not native: off, the sale and the caravan tier are native; on,
cargo gold sets the stars and a Jangan route raises the sale's profit.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/caravan"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestTradeTierForGold
================
*/
func TestTradeTierForGold(t *testing.T) {
	for value, want := range map[uint64]uint8{0: 0, 1: 1, 4_999_999: 1, 9_999_999: 1, 10_000_000: 2, 19_999_999: 2,
		20_000_000: 3, 35_000_000: 4, 49_999_999: 4, 50_000_000: 5, 900_000_000: 5} {
		if got := TradeTierForGold(value); got != want {
			t.Fatalf("%d: tier %d, want %d", value, got, want)
		}
	}
}

/*
================
TestRoutePercentBothWays

Jangan to and from each town, nothing between two other towns, nothing
for a good of no specialty town (an outpost trader's).
================
*/
func TestRoutePercentBothWays(t *testing.T) {
	origins := map[uint32]tradeTown{1: townJangan, 2: townDonwhang, 3: townHotan, 4: townSamarkand, 5: townConstantinople}
	for _, tc := range []struct {
		npc  string
		item uint32
		want int64
	}{
		{"NPC_WC_SPECIAL", 1, 50}, {"NPC_CH_SPECIAL", 2, 50},
		{"NPC_KT_SPECIAL", 1, 100}, {"NPC_CH_SPECIAL", 3, 100},
		{"NPC_CA_SPECIAL", 1, 200}, {"NPC_CH_SPECIAL", 4, 200},
		{"NPC_EU_SPECIAL", 1, 300}, {"NPC_CH_SPECIAL", 5, 300},
		{"NPC_CH_SPECIAL", 1, 0}, {"NPC_WC_SPECIAL", 5, 0}, {"NPC_CH_SPECIAL2", 1, 0}, {"NPC_CH_SPECIAL", 99, 0},
	} {
		if got := routePercent(origins, tc.npc, tc.item); got != tc.want {
			t.Fatalf("%s item %d: %d%%, want %d%%", tc.npc, tc.item, got, tc.want)
		}
	}
}

/*
================
TestTradeOriginsFromTheShippedShops
================
*/
func TestTradeOriginsFromTheShippedShops(t *testing.T) {
	licensed.RequireGameData(t)
	items := shippedItems(t)
	shops, err := commerce.Load(gamedatatest.TextdataDir(t), items)
	if err != nil {
		t.Fatal(err)
	}
	origins := tradeOriginTowns(shops)
	for codename, want := range map[string]tradeTown{
		"ITEM_ETC_TRADE_CH_01": townJangan, "ITEM_ETC_TRADE_WC_01": townDonwhang, "ITEM_ETC_TRADE_KT_01": townHotan,
		"ITEM_ETC_TRADE_CA_01": townSamarkand, "ITEM_ETC_TRADE_EU_01": townConstantinople,
		"ITEM_ETC_TRADE_CH_05": townNone, "ITEM_ETC_TRADE_RM_01": townNone,
	} {
		ref, found := items.ItemRefByCodename(codename)
		if !found {
			t.Fatalf("%s missing", codename)
		}
		if got := origins[ref.RefObjID]; got != want {
			t.Fatalf("%s: town %d, want %d", codename, got, want)
		}
	}
}

/*
================
TestJanganRouteRaisesTheSaleProfit

Through tradeSaleValue: a Donwhang good sold to Jangan's trader earns half
its native profit again with the flag on, and exactly native with it off.
================
*/
func TestJanganRouteRaisesTheSaleProfit(t *testing.T) {
	sale := func(on bool) (int64, int64) {
		rt, c := traderFixture(t, false)
		rt.TradeTiers = on
		rt.tradeOrigins = map[uint32]tradeTown{2151: townDonwhang}
		npc := rt.NpcRoster[0]
		npc.Codename = "NPC_CH_SPECIAL"
		row := c.MissionInventory[1]
		taxed, profit, collected, ok := rt.tradeSaleValue(testDivision, c, npc, inventory.Item{RefObjID: row.RefObjID,
			Codename: row.Codename, TypeFlags: row.TypeFlags, Quantity: 2})
		if !ok {
			t.Fatal("sale refused")
		}
		return int64(taxed) + collected, profit
	}
	nativeCredit, nativeProfit := sale(false)
	credit, profit := sale(true)
	if nativeProfit <= 0 || profit != nativeProfit+nativeProfit/2 || credit != nativeCredit+nativeProfit/2 {
		t.Fatalf("native %d/%d, on %d/%d", nativeCredit, nativeProfit, credit, profit)
	}
}

/*
================
TestCaravanTierFollowsCargoGold

With the flag on, 40 goods of reference price 300,000 (12M) are two stars
whatever the trader's level; off, the native units rule applies.
================
*/
func TestCaravanTierFollowsCargoGold(t *testing.T) {
	rt, c := traderFixture(t, false)
	ref, _ := rt.deps.ItemReferences().ItemRefByCodename("ITEM_ETC_TRADE_WC_01")
	ref.NativeFields = enterworld.NewNativeFields(map[string]float64{"price": 300_000, "maxStack": 40})
	c.ActiveCOS.Container.Rows = []domain.InventoryRow{{Slot: 0, RefObjID: ref.RefObjID, Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 40}}
	nativeTier, nativeStars := rt.caravanTier(c)
	value := rt.caravanCargoValue(c)
	if nativeTier != caravan.DifficultyTier(value) || nativeStars != caravan.StarRating(value, false) {
		t.Fatalf("off: tier %d stars %d", nativeTier, nativeStars)
	}
	rt.TradeTiers = true
	if tier, stars := rt.caravanTier(c); tier != 2 || stars != 2 || rt.cargoGoldValue(c) != 12_000_000 {
		t.Fatalf("on: tier %d stars %d gold %d", tier, stars, rt.cargoGoldValue(c))
	}
}
