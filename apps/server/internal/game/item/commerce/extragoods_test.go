/*
===========================================================================

extragoods_test.go - operator NPC shop goods (port-only, not native)

The setting parses strictly, "off" leaves the shipped stock untouched, and
the owner's default list lands in Jangan's smith after its authored goods.

===========================================================================
*/
package commerce

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
shippedCatalog
================
*/
func shippedCatalog(t *testing.T) (*Catalog, enterworld.ItemRefSource) {
	t.Helper()
	dir := licensed.RetailTextdataDir(t)
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
	if err != nil || len(on) != 14 {
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
	goods, _ := ParseExtraGoods("on")
	if err := c.AddExtraGoods(goods, refs); err != nil {
		t.Fatal(err)
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
	if offers[1].Price != 1000000 || offers[len(offers)-1].Ref.Codename != "ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_03" ||
		offers[len(offers)-1].Price != 5000000 {
		t.Fatalf("default prices: first %d, last %s %d", offers[1].Price, offers[len(offers)-1].Ref.Codename, offers[len(offers)-1].Price)
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
