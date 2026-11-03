/*
===========================================================================

extragoods_test.go - operator NPC shop goods (port-only, not native)

The setting parses strictly, "off" leaves the shipped stock untouched, and
the owner's default list lands after each shop's authored goods.

===========================================================================
*/
package commerce

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/clientdata"
)

/*
================
shippedCatalog
================
*/
func shippedCatalog(t *testing.T) (*Catalog, enterworld.ItemRefSource) {
	t.Helper()
	dir := clientdata.RetailTextdataDir(t)
	if _, e := os.Stat(dir); e != nil {
		t.Skip("original media unavailable")
	}
	refs := enterworld.NewTextdataItems(dir)
	c, e := Load(dir, refs)
	if e != nil {
		t.Fatal(e)
	}
	return c, refs
}

/*
================
TestParseExtraGoods
================
*/
func TestParseExtraGoods(t *testing.T) {
	for _, off := range []string{"", " ", "off", "OFF", "0", "false"} {
		if goods, err := ParseExtraGoods(off); err != nil || goods != nil {
			t.Fatalf("%q = %v, %v; want the native stock", off, goods, err)
		}
	}
	on, err := ParseExtraGoods("on")
	if err != nil || len(on) != 13+28+25+4 {
		t.Fatalf("on = %d goods, %v", len(on), err)
	}
	list, err := ParseExtraGoods(" STORE_CH_POTION_TAB1 : ITEM_ETC_ALL_SPOTION_01 : 1000 ")
	if err != nil || !reflect.DeepEqual(list, []ExtraGood{{"STORE_CH_POTION_TAB1", "ITEM_ETC_ALL_SPOTION_01", 1000}}) {
		t.Fatalf("list = %+v, %v", list, err)
	}
	for _, bad := range []string{"tab:item", "tab:item:0", "tab:item:4294967296", "tab:item:-1", "tab:item:x",
		":item:1", "tab::1", "tab:item:1:extra", "a:b:1,"} {
		if _, err := ParseExtraGoods(bad); err == nil || !strings.Contains(err.Error(), EnvShopExtraGoods) {
			t.Fatalf("%q accepted (%v)", bad, err)
		}
	}
}

/*
================
TestExtraGoodsOffKeepsTheShippedStock
================
*/
func TestExtraGoodsOffKeepsTheShippedStock(t *testing.T) {
	c, refs := shippedCatalog(t)
	before := map[int32][]Offer{}
	for tab, offers := range c.Tabs {
		before[tab] = append([]Offer(nil), offers...)
	}
	if err := c.AddExtraGoods(nil, refs); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, c.Tabs) {
		t.Fatal("no extra goods changed the shipped stock")
	}
}

/*
================
TestDefaultExtraGoodsFollowTheSmithsStock

The owner's list lands in STORE_CH_SMITH_TAB3 after its arrows (slot 0), in
order, at its prices, as one full stack of each item.
================
*/
func TestDefaultExtraGoodsFollowTheSmithsStock(t *testing.T) {
	c, refs := shippedCatalog(t)
	tab := c.tabIDs["STORE_CH_SMITH_TAB3"]
	if len(c.Tabs[tab]) != 1 || c.Tabs[tab][0].Slot != 0 {
		t.Fatalf("shipped smith tab 3 = %+v, want its arrows alone", c.Tabs[tab])
	}
	all, _ := ParseExtraGoods("on")
	if err := c.AddExtraGoods(all, refs); err != nil {
		t.Fatal(err)
	}
	var goods []ExtraGood
	for _, good := range all {
		if good.Tab == "STORE_CH_SMITH_TAB3" {
			goods = append(goods, good)
		}
	}
	offers := c.Tabs[tab]
	if len(offers) != 1+len(goods) {
		t.Fatalf("smith tab 3 has %d offers", len(offers))
	}
	for i, good := range goods {
		offer := offers[1+i]
		if offer.Slot != uint8(1+i) || offer.Ref.Codename != good.Codename || offer.Price != good.Price ||
			offer.Currency != PaymentGold || len(offer.Contents) != 1 || offer.Stack == 0 || offer.Contents[0].Stack != offer.Stack {
			t.Fatalf("offer %d = %+v", i, offer)
		}
	}
	if offers[1].Price != 1000000 || offers[len(offers)-1].Ref.Codename != "ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_09" ||
		offers[len(offers)-1].Price != 5000000 {
		t.Fatalf("default prices: first %d, last %s %d", offers[1].Price, offers[len(offers)-1].Ref.Codename, offers[len(offers)-1].Price)
	}
}

/*
================
TestDefaultExtraGoodsBetaBalance

Owner, 2026-10-13: the Luck, Steady, Astral, Athanasia and Repair stones are
the smith's, degree 9 only, at 5,000,000; the other degree 9 magic stones and
every degree 9 attribute stone at Jangan's grocer (her alchemy tab, after its
seven authored goods) for 2,000,000, so no stone is sold in two shops; and
the five vigor potions at every town's herbalist for their itemdata price.
================
*/
func TestDefaultExtraGoodsBetaBalance(t *testing.T) {
	c, refs := shippedCatalog(t)
	grocer := c.tabIDs["STORE_CH_ACCESSORY_TAB3"]
	authored := len(c.Tabs[grocer])
	goods, _ := ParseExtraGoods("on")
	if err := c.AddExtraGoods(goods, refs); err != nil {
		t.Fatal(err)
	}
	var stones []Offer
	for _, offer := range c.Tabs[grocer][authored:] {
		if !strings.Contains(offer.Ref.Codename, "_PROB_UP_") {
			stones = append(stones, offer)
		}
	}
	if authored != 7 || len(stones) != 28 {
		t.Fatalf("grocer tab: %d authored, %d stones; want 7 and 28", authored, len(stones))
	}
	smithPrices := map[string]uint64{
		"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_09":      5000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_09":     5000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09":    50000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09": 25000000,
		"ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_09":    5000000,
	}
	smith := map[string]bool{}
	for _, offer := range c.Tabs[c.tabIDs["STORE_CH_SMITH_TAB3"]] {
		code := offer.Ref.Codename
		if strings.Contains(code, "_MAGICSTONE_") {
			want, ok := smithPrices[code]
			if !ok || offer.Price != want {
				t.Fatalf("smith sells %s at %d; want %d", code, offer.Price, want)
			}
			smith[code] = true
		}
	}
	if len(smith) != 5 {
		t.Fatalf("smith sells %d stones, want Luck, Steady, Astral, Athanasia and Repair", len(smith))
	}
	for _, offer := range stones {
		code := offer.Ref.Codename
		if smith[code] {
			t.Fatalf("%s is sold by both the smith and the grocer", code)
		}
		if offer.Price != 2000000 || offer.Currency != PaymentGold || !strings.HasSuffix(code, "_09") ||
			!(strings.Contains(code, "_MAGICSTONE_") || strings.Contains(code, "_ATTRSTONE_")) {
			t.Fatalf("grocer offer %s = %+v", code, offer)
		}
	}
	vigor := map[string]uint64{"ITEM_ETC_ALL_POTION_01": 100, "ITEM_ETC_ALL_POTION_02": 180,
		"ITEM_ETC_ALL_POTION_03": 320, "ITEM_ETC_ALL_POTION_04": 600, "ITEM_ETC_ALL_POTION_05": 900}
	for _, town := range []string{"CH", "WC", "KT", "EU", "CA"} {
		offers := c.Tabs[c.tabIDs["STORE_"+town+"_POTION_TAB1"]]
		found := 0
		for _, offer := range offers {
			if price, ok := vigor[offer.Ref.Codename]; ok {
				found++
				if offer.Price != price {
					t.Fatalf("%s %s costs %d, want its itemdata %d", town, offer.Ref.Codename, offer.Price, price)
				}
			}
		}
		if found != len(vigor) {
			t.Fatalf("%s herbalist sells %d vigor potions, want %d", town, found, len(vigor))
		}
	}
}

/*
================
TestDefaultExtraGoodsLuckyPowderAtEveryGrocer

F7, owner 2026-10-13 (Lucky Powder option 1): every grocer's alchemy tab
sells degree 9 Lucky Powder (A_09) once, for its native Hotan price of
54,469 gold. Hotan's two grocer tabs keep their native A_09 and gain
nothing; the large powder (B_09) is not sold until F8 (owner, 2026-10-14).
The authored goods keep their slots: the extras follow.
================
*/
func TestDefaultExtraGoodsLuckyPowderAtEveryGrocer(t *testing.T) {
	const a09, b09, price = "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09", "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_B_09", 54469
	c, refs := shippedCatalog(t)
	grocers := []string{"STORE_CH_ACCESSORY_TAB3", "STORE_WC_ACCESSORY_TAB3", "STORE_KT_ACCESSORY_TAB3",
		"STORE_KT_ACCESSORY_EU_TAB3", "STORE_EU_ACCESSORY_TAB3", "STORE_CA_ACCESSORY_TAB3"}
	shipped := map[string][]Offer{}
	for _, tab := range grocers {
		shipped[tab] = append([]Offer(nil), c.Tabs[c.tabIDs[tab]]...)
	}
	// Native: only Hotan sells A_09, at the price its package names.
	native := 0
	for _, offer := range shipped["STORE_KT_ACCESSORY_TAB3"] {
		if offer.Ref.Codename == a09 {
			native++
			if offer.Price != price || offer.Currency != PaymentGold {
				t.Fatalf("Hotan's native A_09 = %+v; want %d gold each", offer, price)
			}
		}
	}
	if native != 1 {
		t.Fatalf("Hotan sells A_09 %d time(s) natively", native)
	}
	goods, _ := ParseExtraGoods("on")
	if err := c.AddExtraGoods(goods, refs); err != nil {
		t.Fatal(err)
	}
	for _, tab := range grocers {
		offers := c.Tabs[c.tabIDs[tab]]
		if !reflect.DeepEqual(offers[:len(shipped[tab])], shipped[tab]) {
			t.Fatalf("%s: the authored goods moved", tab)
		}
		sold := map[string]int{}
		for _, offer := range offers {
			if code := offer.Ref.Codename; code == a09 || code == b09 {
				sold[code]++
				if offer.Price != price || offer.Currency != PaymentGold || len(offer.Contents) != 1 {
					t.Fatalf("%s sells %s as %+v", tab, code, offer)
				}
			}
		}
		if sold[a09] != 1 || sold[b09] != 0 {
			t.Fatalf("%s sells A_09 %d time(s), B_09 %d time(s); want 1 and 0", tab, sold[a09], sold[b09])
		}
	}
}

/*
================
TestAnOperatorCanSellTheGoldDragonFlag

The owner wants the flag to be one setting away (2026-10-11).
================
*/
func TestAnOperatorCanSellTheGoldDragonFlag(t *testing.T) {
	c, refs := shippedCatalog(t)
	goods, err := ParseExtraGoods("STORE_CH_POTION_TAB1:ITEM_ETC_E060529_GOLDDRAGONFLAG_M:1000000000")
	if err != nil {
		t.Fatal(err)
	}
	if err := c.AddExtraGoods(goods, refs); err != nil {
		t.Fatal(err)
	}
	offers := c.Tabs[c.tabIDs["STORE_CH_POTION_TAB1"]]
	last := offers[len(offers)-1]
	if last.Ref.Codename != "ITEM_ETC_E060529_GOLDDRAGONFLAG_M" || last.Price != 1000000000 {
		t.Fatalf("flag offer = %+v", last)
	}
}

/*
================
TestExtraGoodsRefuseWhatAShopCannotSell

Each refusal names the setting and leaves the catalogue unchanged.
================
*/
func TestExtraGoodsRefuseWhatAShopCannotSell(t *testing.T) {
	c, refs := shippedCatalog(t)
	full := c.tabIDs["STORE_CH_SMITH_TAB2"]
	c.Tabs[full] = append(c.Tabs[full], Offer{Slot: maxShopSlot, Ref: c.Tabs[full][0].Ref, Price: 1, Currency: PaymentGold})
	for name, good := range map[string]ExtraGood{
		"unknown tab":  {"STORE_NOWHERE", "ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_01", 1},
		"unknown item": {"STORE_CH_SMITH_TAB3", "ITEM_NOT_A_THING", 1},
		"gold heap":    {"STORE_CH_SMITH_TAB3", "ITEM_ETC_GOLD_01", 1},
		"full tab":     {"STORE_CH_SMITH_TAB2", "ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_01", 1},
	} {
		before := len(c.Tabs[c.tabIDs[good.Tab]])
		ok := []ExtraGood{{"STORE_CH_SMITH_TAB3", "ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_02", 1}}
		err := c.AddExtraGoods(append(ok, good), refs)
		if err == nil || !strings.Contains(err.Error(), EnvShopExtraGoods) {
			t.Fatalf("%s accepted (%v)", name, err)
		}
		if len(c.Tabs[c.tabIDs["STORE_CH_SMITH_TAB3"]]) != 1 || len(c.Tabs[c.tabIDs[good.Tab]]) != before {
			t.Fatalf("%s: a refused list changed the catalogue", name)
		}
	}
}
