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
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/tunables"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
)

/*
================
TestTradeTierForLoad
================
*/
func TestTradeTierForLoad(t *testing.T) {
	// Marks at 1/5, 2/5, 7/10 and all of a 1000-full bag, on both
	// sides of each mark.
	for value, want := range map[uint64]uint8{0: 0, 1: 1, 199: 1, 200: 2, 399: 2,
		400: 3, 699: 3, 700: 4, 999: 4, 1000: 5, 900_000: 5} {
		if got := TradeTierForLoad(value, 1000); got != want {
			t.Fatalf("%d of 1000: tier %d, want %d", value, got, want)
		}
	}
	// An unmeasurable bag: nothing is 0, anything is 1.
	if got := TradeTierForLoad(0, 0); got != 0 {
		t.Fatalf("no cargo, no bag: tier %d, want 0", got)
	}
	if got := TradeTierForLoad(5, 0); got != 1 {
		t.Fatalf("cargo, no bag: tier %d, want 1", got)
	}
}

/*
================
TestTradeGoldGap

#3163: the gold still missing from the next star, at the same marks
TradeTierForLoad reads (1/5, 2/5, 7/10 and all of a 1000-full bag).
================
*/
func TestTradeGoldGap(t *testing.T) {
	for value, want := range map[uint64]uint64{
		0: 1, 1: 199, 199: 1, 200: 200, 399: 1, 400: 300, 699: 1, 700: 300, 999: 1, 1000: 0, 900_000: 0,
	} {
		if got := TradeGoldGap(value, 1000); got != want {
			t.Fatalf("%d of 1000: gap %d, want %d", value, got, want)
		}
	}
	// An unmeasurable bag names no gap; an empty one is the first
	// gold of goods short of its first star.
	if got := TradeGoldGap(5, 0); got != 0 {
		t.Fatalf("cargo, no bag: gap %d, want 0", got)
	}
	if got := TradeGoldGap(0, 0); got != 1 {
		t.Fatalf("empty: gap %d, want 1", got)
	}
}

/*
================
TestEveryFullTransportReachesFiveStars

The owner's rule (#2791, measured per transport #3142) as a property
of the shipped data, not of one hand-picked case: walking every
transport row in characterdata against every good stocked at the five
towns' specialty shops, an ordinary transport reads 2, 3, 4 and 5
stars at 1/5, 2/5, 7/10 and all of its own brim-full load, and a mall
transport reads 5 stars at the 101-slot reference load. A big
transport filled only to the smallest bag's full load is not 5 stars.
With the flag off the tier is the native one; that side is pinned by
TestCaravanTierFollowsCargoGold.
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
	transports, malls := 0, 0
	smallest, largest, horseSlots := uint64(0), uint64(0), uint64(0)
	for _, ref := range items.SummonableCharacterRefs() {
		if !isVehicleCOS(ref.TidWord) {
			continue
		}
		slots := uint64(ref.InventoryCapacity)
		if ref.Codename == "COS_T_DHORSE3" {
			horseSlots = slots
		}
		mall := enterworld.IsMallTransport(&ref)
		if mall {
			malls++
		} else {
			transports++
			if smallest == 0 || slots < smallest {
				smallest = slots
			}
			if slots > largest {
				largest = slots
			}
		}
		for _, good := range goods {
			if !enterworld.IsTradeGoods(good.TypeIDs) {
				t.Fatalf("%s is stocked at a specialty shop but is not a trade good", good.Codename)
			}
			price := good.NativeFields.Get("price")
			stack := good.NativeFields.Get("maxStack")
			stackValue := uint64(stack) * uint64(price)
			full := slots * stackValue
			if mall {
				// Measured against its own bag capped at the
				// 101-slot reference: a bigger mall transport
				// crowns at the reference load and its own larger
				// load is never more; a smaller one is full at
				// its own brim.
				measure := slots
				if measure > mallTierReferenceSlots {
					measure = mallTierReferenceSlots
				}
				reference := measure * stackValue
				if got := TradeTierForLoad(reference, reference); got != 5 {
					t.Fatalf("%s at its measured full load of %s reads %d stars, want 5", ref.Codename, good.Codename, got)
				}
				if got := TradeTierForLoad(full, reference); got != 5 {
					t.Fatalf("%s brim-full of %s reads %d stars, want 5", ref.Codename, good.Codename, got)
				}
				continue
			}
			for _, mark := range []struct {
				value uint64
				want  uint8
			}{{full / 5, 2}, {full * 2 / 5, 3}, {full * 7 / 10, 4}, {full, 5}} {
				if got := TradeTierForLoad(mark.value, full); got != mark.want {
					t.Fatalf("%s with %d of %s (full %d) reads %d stars, want %d", ref.Codename, mark.value, good.Codename, full, got, mark.want)
				}
			}
		}
	}
	if transports+malls < 35 || malls == 0 || smallest == 0 {
		t.Fatalf("walked %d ordinary and %d mall transports (smallest ordinary %d slots), want the shipped fleet", transports, malls, smallest)
	}
	// The mall reference is the #2791 ruling's full ordinary transport,
	// so the constant and the authored data cannot drift apart silently.
	if horseSlots != mallTierReferenceSlots {
		t.Fatalf("COS_T_DHORSE3 has %d slots, the mall reference is %d", horseSlots, mallTierReferenceSlots)
	}
	// A big transport filled only to the smallest bag's full load is
	// nowhere near its own fifth star.
	for _, good := range goods {
		stackValue := uint64(good.NativeFields.Get("maxStack")) * uint64(good.NativeFields.Get("price"))
		if got := TradeTierForLoad(smallest*stackValue, largest*stackValue); got == 5 {
			t.Fatalf("a %d-slot transport filled to a %d-slot load reads 5 stars", largest, smallest)
		}
		break
	}
}

/*
================
TestFullSmallTransportNativeBanditsCoverTheFlagCount

Work item 3 of #3162, measured on the shipped data: the bandit count
is a threat, not a payout, and needs no cargo scaling. The smallest
ordinary transport - the 32-slot donkey, first ridden at level 20 -
brim-full at that level already reads the native top tier, and its
native star rating spawns 10..22 bandits (SpawnCount: the rating,
one more on half the draws, doubled on 2%), covering the flag's
5-star count of 5..12 at both ends. The fifth star therefore never
threatens a small caravan beyond what the identical load draws with
SRO_TRADE_TIERS off. At higher trader levels the native count falls
away (the cargo value shrinks against the level's gold basis) while
the flag keeps the danger the fifth star advertises (#2958), which
is the star's recorded purpose, so the count takes no transport
class into account.
================
*/
func TestFullSmallTransportNativeBanditsCoverTheFlagCount(t *testing.T) {
	clientdata.RequireGameData(t)
	dir := gamedatatest.TextdataDir(t)
	items := enterworld.NewTextdataItems(dir)
	if _, err := items.ApplyTradeGoodsPriceFactor(enterworld.TradeGoodsPriceFactor); err != nil {
		t.Fatal(err)
	}
	levels := enterworld.NewTextdataLevels(dir)
	var smallest *enterworld.CharacterRef
	for _, ref := range items.SummonableCharacterRefs() {
		candidate := ref
		if !isVehicleCOS(candidate.TidWord) || enterworld.IsMallTransport(&candidate) {
			continue
		}
		if smallest == nil || candidate.InventoryCapacity < smallest.InventoryCapacity {
			smallest = &candidate
		}
	}
	if smallest == nil || smallest.Codename != "COS_T_DONKEY" || smallest.InventoryCapacity != 32 || smallest.Level != 20 {
		t.Fatalf("smallest ordinary transport = %+v, want the 32-slot level-20 donkey", smallest)
	}
	good, found := items.ItemRefByCodename("ITEM_ETC_TRADE_WC_01")
	if !found {
		t.Fatal("ITEM_ETC_TRADE_WC_01 missing")
	}
	stack := good.NativeFields.Get("maxStack")
	basis, found := levels.WithdrawalGoldBasis(caravan.CargoLevel(int64(smallest.Level)))
	if !found {
		t.Fatalf("no withdrawal gold basis at level %d", smallest.Level)
	}
	// The native cargo value of the brim-full donkey, exactly as
	// caravanCargoValue computes it for a trader of the first level
	// that can ride it.
	capacity := caravan.CargoCapacity(smallest.Parameters.MonsterType&0x0f, uint16(smallest.InventoryCapacity))
	goods := uint32(smallest.InventoryCapacity) * uint32(stack)
	value := caravan.CargoValue(goods, capacity, caravan.BaseDeathExp(basis))
	if tier := caravan.DifficultyTier(value); tier != 5 {
		t.Fatalf("brim-full donkey at level %d: native value %d reads tier %d, want 5", smallest.Level, value, tier)
	}
	stars := caravan.StarRating(value, false)
	// SpawnCount(stars) draws stars..(stars+1)*2 bandits; the flag's
	// 5-star count is SpawnCount(5) = 5..12.
	if stars < 5 || (stars+1)*2 < 12 {
		t.Fatalf("native rating %d spawns %d..%d bandits, which does not cover the flag's 5..12", stars, stars, (stars+1)*2)
	}
}

/*
================
TestTradeLoadDoorLabelNamesTheLoad

A trade-goods purchase into a transport commits under its own ledger
door (#3162), stamped with the transport's size and the load's stars
once the purchase is aboard; anything else stays a shop transaction.
================
*/
func TestTradeLoadDoorLabelNamesTheLoad(t *testing.T) {
	rt, c := traderFixture(t, false)
	rt.TradeTiers = true
	good, found := rt.deps.ItemReferences().ItemRefByCodename("ITEM_ETC_TRADE_WC_01")
	if !found {
		t.Fatal("fixture good missing")
	}
	// The fixture transport holds 4 slots of 40: 160 goods brim it.
	offer := &commerce.Offer{Ref: good}
	q := wire.ItemMoveRequest{MovementType: wire.MoveTypeCosShopBuy, CosGID: c.ActiveCOS.GID, Quantity: 160}
	if got := rt.tradeLoadDoorLabel(c, q, offer); got != "trade-load-4s5" {
		t.Fatalf("brim-full load door = %q, want trade-load-4s5", got)
	}
	// One stack is a quarter of the brim: two stars.
	q.Quantity = 40
	if got := rt.tradeLoadDoorLabel(c, q, offer); got != "trade-load-4s2" {
		t.Fatalf("one-stack load door = %q, want trade-load-4s2", got)
	}
	if got := rt.tradeLoadDoorLabel(c, q, nil); got != "shop-transaction" {
		t.Fatalf("unknown offer door = %q, want shop-transaction", got)
	}
	ordinary := &commerce.Offer{Ref: &enterworld.ItemRef{Codename: "ITEM_ETC_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 1}}}
	if got := rt.tradeLoadDoorLabel(c, q, ordinary); got != "shop-transaction" {
		t.Fatalf("ordinary goods door = %q, want shop-transaction", got)
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

Under SRO_TRADE_TIERS the stars measure how full the bag stands: the
brim-full horse (101 stacks in its 101-slot bag) is five stars at the
reference price and at the scaled one alike - the price cancels, only
the fill counts - and a fifth of the load aboard is two stars. The
cargo value itself is still count x reference price. Off, the native
units rule applies.
================
*/
func TestCaravanTierFollowsCargoGold(t *testing.T) {
	rt, c := traderFixture(t, false)
	ref, _ := rt.deps.ItemReferences().ItemRefByCodename("ITEM_ETC_TRADE_WC_01")
	full := func(price float64) {
		ref.NativeFields = enterworld.NewNativeFields(map[string]float64{"price": price, "maxStack": 40})
		c.ActiveCOS.Container.Capacity = 101
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
	if tier, stars := rt.caravanTier(c); tier != 5 || stars != 5 || rt.cargoGoldValue(c) != 101*40*348 {
		t.Fatalf("on, full: tier %d stars %d gold %d", tier, stars, rt.cargoGoldValue(c))
	}
	// A fifth of the load aboard (21 of 101 stacks) is two stars.
	c.ActiveCOS.Container.Rows = c.ActiveCOS.Container.Rows[:21]
	if tier, _ := rt.caravanTier(c); tier != 2 {
		t.Fatalf("on, a fifth full: tier %d, want 2", tier)
	}
	full(348 * enterworld.TradeGoodsPriceFactor)
	if tier, stars := rt.caravanTier(c); tier != 5 || stars != 5 || rt.cargoGoldValue(c) != 101*40*348*enterworld.TradeGoodsPriceFactor {
		t.Fatalf("on, scaled: tier %d stars %d gold %d", tier, stars, rt.cargoGoldValue(c))
	}
	if level() != 5 {
		t.Fatalf("on, scaled: trade level %d, want the gold tier 5", level())
	}
}
