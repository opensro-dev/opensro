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
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestTradeTierForGold
================
*/
func TestTradeTierForGold(t *testing.T) {
	for value, want := range map[uint64]uint8{0: 0, 1: 1, 1_870_847: 1, 1_870_848: 2, 3_741_695: 2,
		3_741_696: 3, 6_547_967: 3, 6_547_968: 4, 9_354_239: 4, 9_354_240: 5, 900_000_000: 5} {
		if got := TradeTierForGold(value); got != want {
			t.Fatalf("%d: tier %d, want %d", value, got, want)
		}
	}
}

/*
================
TestEveryFullTransportReachesFiveStars

The owner's rule (#2791) as a property of the shipped data, not of one
hand-picked case (#3142): walking every transport row in characterdata
against every good stocked at the five towns' specialty shops, a
brim-full load of 40-stacks at the flag's scaled prices reads 5 stars.
The smallest bag walked must be the ladder's own calibration - its full
load is exactly the 5-star mark - so the marks and the authored data
cannot drift apart silently. With the flag off the tier is the native
one; that side is pinned by TestCaravanTierFollowsCargoGold.
================
*/
func TestEveryFullTransportReachesFiveStars(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	if _, err := items.ApplyTradeGoodsPriceFactor(enterworld.TradeGoodsPriceFactor); err != nil {
		t.Fatal(err)
	}
	shops, err := commerce.Load(dir, items)
	if err != nil {
		t.Fatal(err)
	}
	goods := map[uint32]*enterworld.ItemRef{}
	for _, shop := range tradeTownShops {
		offers := shops.TabOffers(shop.tab)
		if len(offers) == 0 {
			t.Fatalf("%s stocks nothing", shop.tab)
		}
		for _, offer := range offers {
			if offer.Ref != nil {
				goods[offer.Ref.RefObjID] = offer.Ref
			}
			for _, content := range offer.Contents {
				if content.Ref != nil {
					goods[content.Ref.RefObjID] = content.Ref
				}
			}
		}
	}
	if len(goods) == 0 {
		t.Fatal("no town stocks any trade good")
	}
	transports := 0
	smallest := uint64(0)
	for _, ref := range items.SummonableCharacterRefs() {
		if !isVehicleCOS(ref.TidWord) {
			continue
		}
		transports++
		slots := uint64(ref.InventoryCapacity)
		if smallest == 0 || slots < smallest {
			smallest = slots
		}
		for _, good := range goods {
			if !enterworld.IsTradeGoods(good.TypeIDs) {
				t.Fatalf("%s is stocked at a specialty shop but is not a trade good", good.Codename)
			}
			price := good.NativeFields.Get("price")
			stack := good.NativeFields.Get("maxStack")
			full := slots * uint64(stack) * uint64(price)
			if got := TradeTierForGold(full); got != 5 {
				t.Fatalf("%s brim-full of %s: value %d reads %d stars, want 5", ref.Codename, good.Codename, full, got)
			}
		}
	}
	if transports < 35 || smallest == 0 {
		t.Fatalf("walked %d transports (smallest %d slots), want the shipped fleet", transports, smallest)
	}
	if full := smallest * 40 * 348 * enterworld.TradeGoodsPriceFactor; full != tradeTierGold[len(tradeTierGold)-1] {
		t.Fatalf("the smallest full bag is %d, the 5-star mark is %d", full, tradeTierGold[len(tradeTierGold)-1])
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
		if got := routePercent(origins, tc.npc, tc.item, tunables.Current()); got != tc.want {
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
	clientdata.RequireGameData(t)
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
			Codename: row.Codename, TypeFlags: row.TypeFlags, Quantity: 2}, tunables.Current())
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

A full horse (101 stacks of 40 goods) at the shipped reference price 348 is
about 1.4M, one star; at SRO_TRADE_TIERS' scaled 7,308 it is 29,524,320 -
the brim-full ordinary transport, five stars (#2791). Off, the native
units rule applies.
================
*/
func TestCaravanTierFollowsCargoGold(t *testing.T) {
	rt, c := traderFixture(t, false)
	ref, _ := rt.deps.ItemReferences().ItemRefByCodename("ITEM_ETC_TRADE_WC_01")
	full := func(price float64) {
		ref.NativeFields = enterworld.NewNativeFields(map[string]float64{"price": price, "maxStack": 40})
		c.ActiveCOS.Container.Rows = nil
		for slot := int64(0); slot < 101; slot++ {
			c.ActiveCOS.Container.Rows = append(c.ActiveCOS.Container.Rows, domain.InventoryRow{Slot: slot, RefObjID: ref.RefObjID,
				Codename: ref.Codename, TypeFlags: ref.TypeFlags(), StackCount: 40})
		}
	}
	full(348)
	// The bag's trade level (0x352D) carries the same tier, off and on.
	level := func() uint8 { return rt.tradeStarFrame(c).Payload[0] }
	nativeTier, nativeStars := rt.caravanTier(c)
	if level() != nativeTier {
		t.Fatalf("off: trade level %d, tier %d", level(), nativeTier)
	}
	value := rt.caravanCargoValue(c)
	if nativeTier != caravan.DifficultyTier(value) || nativeStars != caravan.StarRating(value, false) {
		t.Fatalf("off: tier %d stars %d", nativeTier, nativeStars)
	}
	rt.TradeTiers = true
	if tier, stars := rt.caravanTier(c); tier != 1 || stars != 1 || rt.cargoGoldValue(c) != 101*40*348 {
		t.Fatalf("on, unscaled: tier %d stars %d gold %d", tier, stars, rt.cargoGoldValue(c))
	}
	full(348 * enterworld.TradeGoodsPriceFactor)
	if tier, stars := rt.caravanTier(c); tier != 5 || stars != 5 || rt.cargoGoldValue(c) != 101*40*348*enterworld.TradeGoodsPriceFactor {
		t.Fatalf("on, scaled: tier %d stars %d gold %d", tier, stars, rt.cargoGoldValue(c))
	}
	if level() != 5 {
		t.Fatalf("on, scaled: trade level %d, want the gold tier 5", level())
	}
}
