/*
===========================================================================

alchemyshop_test.go - Jangan's alchemy shop regroup (port-only, not native)

The switch on regroups the default list's alchemy goods onto the grocer's
three synthesized tabs at the same prices; off, an operator list, or the
extra goods off each keep today's shops exactly.

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
janganCatalog

The shipped catalogue with the environment's extra goods applied.
================
*/
func janganCatalog(t *testing.T) *Catalog {
	t.Helper()
	dir := clientdata.RetailTextdataDir(t)
	if _, e := os.Stat(dir); e != nil {
		t.Skip("original media unavailable")
	}
	goods, err := ExtraGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	c, err := LoadWithExtraGoods(dir, enterworld.NewTextdataItems(dir), goods)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

/*
================
tabCodenames
================
*/
func tabCodenames(offers []Offer) []string {
	var codes []string
	for _, offer := range offers {
		codes = append(codes, offer.Ref.Codename)
	}
	return codes
}

/*
================
TestJanganAlchemyShopRegroupsTheDefaultList
================
*/
func TestJanganAlchemyShopRegroupsTheDefaultList(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "on")
	t.Setenv(EnvJanganAlchemyShop, "on")
	goods, err := ExtraGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	// The same 70 goods the default list sells, regrouped: nothing on the
	// smith's or grocer's authored tabs, 9 + 14 + 19 on the three new ones.
	if len(goods) != 70 {
		t.Fatalf("regrouped list = %d goods, want the default 70", len(goods))
	}
	counts := map[string]int{}
	for _, good := range goods {
		if good.Tab == janganSmithTab || good.Tab == janganGrocerTab {
			t.Fatalf("regrouped list still stocks %s on %s", good.Codename, good.Tab)
		}
		counts[good.Tab]++
	}
	if counts[janganElixirsTab] != 9 || counts[janganAttrStonesTab] != 14 || counts[janganMagicStonesTab] != 19 {
		t.Fatalf("tab goods = %v", counts)
	}

	c := janganCatalog(t)
	smith := c.Tabs[c.tabIDs[janganSmithTab]]
	if len(smith) != 1 || smith[0].Ref.Codename != "ITEM_ETC_AMMO_ARROW_01" {
		t.Fatalf("smith tab 3 = %v, want its authored arrows alone", tabCodenames(smith))
	}
	// The synthesized ids sit above every authored tab, in tab order.
	var authoredMax int32
	for name, id := range c.tabIDs {
		if name != janganElixirsTab && name != janganAttrStonesTab && name != janganMagicStonesTab && id > authoredMax {
			authoredMax = id
		}
	}
	if c.tabIDs[janganElixirsTab] != authoredMax+1 || c.tabIDs[janganAttrStonesTab] != authoredMax+2 ||
		c.tabIDs[janganMagicStonesTab] != authoredMax+3 {
		t.Fatalf("synthesized ids = %d/%d/%d above authored %d",
			c.tabIDs[janganElixirsTab], c.tabIDs[janganAttrStonesTab], c.tabIDs[janganMagicStonesTab], authoredMax)
	}
	// The shipped tables' largest authored tab id is 2378; the roster
	// projection pins the same synthesized ids (2379-2381).
	if authoredMax != 2378 {
		t.Fatalf("authored tab maximum = %d, want the shipped 2378", authoredMax)
	}
	// The elixirs tab: the grocer's authored alchemy goods first, then the
	// elixir recipes, then the degree 9 powder.
	elixirs := c.Tabs[authoredMax+1]
	wantElixirs := []string{
		"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_01", "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_02",
		"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_03", "ITEM_ETC_ARCHEMY_POTION_SPEED_05",
		"ITEM_ETC_ARCHEMY_POTION_SPEED_11", "ITEM_ETC_ARCHEMY_RONDO_01", "ITEM_ETC_ARCHEMY_RONDO_02",
		"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A", "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_A",
		"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_A", "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_A",
		"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B", "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_SHIELD_B",
		"ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ARMOR_B", "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_ACCESSARY_B",
		"ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09",
	}
	if got := tabCodenames(elixirs); !reflect.DeepEqual(got, wantElixirs) {
		t.Fatalf("elixirs tab = %v", got)
	}
	for i, offer := range elixirs {
		if offer.Slot != uint8(i) {
			t.Fatalf("elixirs offer %d slot = %d", i, offer.Slot)
		}
	}
	if elixirs[7].Price != 1000000 || elixirs[15].Price != 54469 {
		t.Fatalf("elixir prices = %d/%d", elixirs[7].Price, elixirs[15].Price)
	}
	attr := c.Tabs[authoredMax+2]
	if len(attr) != 14 || attr[0].Ref.Codename != "ITEM_ETC_ARCHEMY_ATTRSTONE_PA_09" || attr[0].Price != 2000000 {
		t.Fatalf("attribute tab = %v", tabCodenames(attr))
	}
	magic := c.Tabs[authoredMax+3]
	if len(magic) != 19 {
		t.Fatalf("magic tab has %d offers", len(magic))
	}
	wantTail := []struct {
		code  string
		price uint64
	}{
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_LUCK_09", 5000000},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_SOLID_09", 5000000},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_ASTRAL_09", 50000000},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_ATHANASIA_09", 25000000},
		{"ITEM_ETC_ARCHEMY_MAGICSTONE_REPAIR_09", 5000000},
	}
	for i, want := range wantTail {
		offer := magic[14+i]
		if offer.Ref.Codename != want.code || offer.Price != want.price {
			t.Fatalf("magic offer %d = %s at %d", 14+i, offer.Ref.Codename, offer.Price)
		}
	}
}

/*
================
TestJanganAlchemyShopOffKeepsTodaysShops
================
*/
func TestJanganAlchemyShopOffKeepsTodaysShops(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "on")
	t.Setenv(EnvJanganAlchemyShop, "off")
	goods, err := ExtraGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	on, _ := ParseExtraGoods("on")
	if !reflect.DeepEqual(goods, on) {
		t.Fatal("switch off changed the default list")
	}
	c := janganCatalog(t)
	if _, ok := c.tabIDs[janganElixirsTab]; ok {
		t.Fatal("switch off registered the synthesized tabs")
	}
	if offers := c.Tabs[c.tabIDs[janganSmithTab]]; len(offers) != 1+13 {
		t.Fatalf("smith tab 3 = %d offers, want its arrows and the 13 goods", len(offers))
	}
	grocer := c.Tabs[c.tabIDs[janganGrocerTab]]
	if len(grocer) != 7+29 || grocer[len(grocer)-1].Ref.Codename != "ITEM_ETC_ARCHEMY_REINFORCE_PROB_UP_A_09" {
		t.Fatalf("grocer tab 3 = %d offers", len(grocer))
	}
}

/*
================
TestJanganAlchemyShopLeavesOperatorListsAlone
================
*/
func TestJanganAlchemyShopLeavesOperatorListsAlone(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "STORE_CH_POTION_TAB1:ITEM_ETC_ALL_POTION_01:100")
	t.Setenv(EnvJanganAlchemyShop, "on")
	goods, err := ExtraGoodsFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if len(goods) != 1 || goods[0].Tab != "STORE_CH_POTION_TAB1" {
		t.Fatalf("operator list = %+v", goods)
	}
	c := janganCatalog(t)
	if _, ok := c.tabIDs[janganElixirsTab]; ok {
		t.Fatal("an operator list registered the synthesized tabs")
	}
}

/*
================
TestJanganAlchemyShopNeedsTheDefaultList
================
*/
func TestJanganAlchemyShopNeedsTheDefaultList(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "off")
	t.Setenv(EnvJanganAlchemyShop, "on")
	goods, err := ExtraGoodsFromEnv()
	if err != nil || goods != nil {
		t.Fatalf("extra goods off = %v, %v", goods, err)
	}
}

/*
================
TestJanganAlchemyShopRefusesABadSwitch
================
*/
func TestJanganAlchemyShopRefusesABadSwitch(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "on")
	t.Setenv(EnvJanganAlchemyShop, "banana")
	if _, err := ExtraGoodsFromEnv(); err == nil || !strings.Contains(err.Error(), EnvJanganAlchemyShop) {
		t.Fatalf("bad switch accepted (%v)", err)
	}
}
