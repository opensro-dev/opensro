/*
===========================================================================

catalog_test.go - the shipped commerce catalog

The NPC shop catalog loads from the shipped tables.

===========================================================================
*/
package commerce

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestShippedCatalog
================
*/
func TestShippedCatalog(t *testing.T) {
	tier.Long(t, "the whole shipped shop catalogue loads")
	dir := clientdata.RetailTextdataDir(t)
	if _, e := os.Stat(dir); e != nil {
		t.Skip("original media unavailable")
	}
	c, e := Load(dir, enterworld.NewTextdataItems(dir))
	if e != nil {
		t.Fatal(e)
	}
	count := 0
	hp := false
	for _, offers := range c.Tabs {
		for _, o := range offers {
			count++
			if o.Ref.Codename == "ITEM_ETC_HP_POTION_01" {
				if o.Price != 60 {
					t.Fatal(o)
				}
				hp = true
			}
		}
	}
	if !hp {
		t.Fatal("HP potion absent")
	}
	// STORE_CH_STABLE_TAB3 (SN_TAB_PET) sells the Grey Wolf Summon Scroll.
	wolf := false
	for _, o := range c.Tabs[1920] {
		wolf = wolf || o.Ref.Codename == "ITEM_COS_P_FLUTE"
	}
	if !wolf {
		t.Fatal("the stable's Pet tab lacks the Grey Wolf Summon Scroll")
	}
	t.Logf("%d admitted offers across %d tabs", count, len(c.Tabs))
	for _, id := range []int32{2037, 2038, 2039, 2040, 2041, 2042, 2043, 2044, 2045, 2046, 2047, 2048, 2049, 2050, 2051, 2053} {
		if len(c.Tabs[id]) == 0 {
			t.Fatalf("Samarkand tab %d has no admitted goods", id)
		}
	}
	// STORE_CH_HONOR_TAB1 (tab 2292) sells Seal of Star (Honor) weapons for Gold + Honor points.
	honorSword := false
	for _, o := range c.Tabs[2292] {
		if o.Ref.Codename == "ITEM_CH_SWORD_08_A_RARE_HONOR" {
			if o.Price != 2700000 || o.HonorPrice != 1000 {
				t.Fatalf("ITEM_CH_SWORD_08_A_RARE_HONOR price = (%d, %d), want (2700000, 1000)", o.Price, o.HonorPrice)
			}
			honorSword = true
		}
	}
	if !honorSword {
		t.Fatal("STORE_CH_HONOR_TAB1 lacks ITEM_CH_SWORD_08_A_RARE_HONOR")
	}
	// STORE_KT_GUILD_TAB1 and STORE_KT_GUILD_TAB3 sell guild emblems and
	// guild recall scrolls in Gold + Guild Points.
	guildTab1, ok1 := c.tabIDs["STORE_KT_GUILD_TAB1"]
	guildTab3, ok3 := c.tabIDs["STORE_KT_GUILD_TAB3"]
	if !ok1 || !ok3 {
		t.Fatalf("missing STORE_KT_GUILD tabs: tab1=%v tab3=%v", ok1, ok3)
	}
	guildRecall, guildCrest := false, false
	for _, o := range append(append([]Offer{}, c.Tabs[guildTab1]...), c.Tabs[guildTab3]...) {
		switch o.Ref.Codename {
		case "ITEM_ETC_GUILD_RECALL":
			if o.Currency != PaymentGold || o.Price != 100000 || o.GuildPointPrice != 1000 {
				t.Fatalf("ITEM_ETC_GUILD_RECALL = (%d, %d, %d), want (%d, 100000, 1000)",
					o.Currency, o.Price, o.GuildPointPrice, PaymentGold)
			}
			guildRecall = true
		case "ITEM_ETC_GUILD_CREST_USER":
			if o.Currency != PaymentGold || o.Price != 1000000 || o.GuildPointPrice != 20000 {
				t.Fatalf("ITEM_ETC_GUILD_CREST_USER = (%d, %d, %d), want (%d, 1000000, 20000)",
					o.Currency, o.Price, o.GuildPointPrice, PaymentGold)
			}
			guildCrest = true
		}
	}
	if !guildRecall || !guildCrest {
		t.Fatalf("STORE_KT_GUILD missing guild items: recall=%v crest=%v", guildRecall, guildCrest)
	}
	// Every active PACKAGE_ITEM_ETC_TRADE_* row in refshopgoods.txt must
	// resolve to a stocked item with active TradeQuotations rows.
	quotedByItem := map[uint32]int{}
	for key := range c.TradeQuotations {
		quotedByItem[key[1]]++
	}
	tradePackages := 0
	for _, row := range enterworld.ReadTextdataFile(filepath.Join(dir, "refshopgoods.txt")) {
		if len(row) < 4 || row[0] != "1" || !strings.HasPrefix(row[3], "PACKAGE_ITEM_ETC_TRADE_") {
			continue
		}
		tradePackages++
		tabID, ok := c.tabIDs[row[2]]
		if !ok {
			t.Fatalf("missing shop tab %s for %s", row[2], row[3])
		}
		itemCode := strings.TrimPrefix(row[3], "PACKAGE_")
		var refID uint32
		for _, offer := range c.Tabs[tabID] {
			if offer.Ref != nil && offer.Ref.Codename == itemCode {
				refID = offer.Ref.RefObjID
				break
			}
		}
		if refID == 0 {
			t.Fatalf("%s (%s) not admitted in tab %s", row[3], itemCode, row[2])
		}
		if quotedByItem[refID] == 0 {
			t.Fatalf("%s (%s, id %d) resolved to zero quotation rows", row[3], itemCode, refID)
		}
	}
	if tradePackages == 0 {
		t.Fatal("no PACKAGE_ITEM_ETC_TRADE_* rows found in refshopgoods.txt")
	}
}

/*
================
TestListedWithoutPriceNamesEachUnpricedPackageOnce

The native load check's report set (the v1.188 server's package
price check): a live goods row whose package has no price the shop takes is
named once, sorted; a priced or condition-held package, a retired row
and a short row are not.
================
*/
func TestListedWithoutPriceNamesEachUnpricedPackageOnce(t *testing.T) {
	goods := [][]string{
		{"1", "11", "TAB_A", "PACKAGE_SILK_ONLY", "0"},
		{"1", "11", "TAB_B", "PACKAGE_SILK_ONLY", "3"},
		{"1", "11", "TAB_A", "PACKAGE_GOLD", "1"},
		{"1", "11", "TAB_A", "PACKAGE_HELD", "2"},
		{"0", "11", "TAB_A", "PACKAGE_RETIRED", "4"},
		{"1", "11", "TAB_A", "PACKAGE_A_UNPRICED", "5"},
		{"1", "11", "TAB_A"},
	}
	settled := map[string]bool{"PACKAGE_GOLD": true, "PACKAGE_HELD": true}
	got := listedWithoutPrice(goods, func(code string) bool { return settled[code] })
	want := []string{"PACKAGE_A_UNPRICED", "PACKAGE_SILK_ONLY"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("unpriced %v, want %v", got, want)
	}
}

/*
================
TestShippedCatalogPricesEveryGood

Every good the shipped v1.150 shops list carries a gold, guild point or
honor price, as the original data does (census 2026-11-28: 1419 gold,
80 gold+honor, 25 gold+guild): the load report stays empty.
================
*/
func TestShippedCatalogPricesEveryGood(t *testing.T) {
	tier.Long(t, "the whole shipped shop catalogue loads")
	dir := clientdata.RetailTextdataDir(t)
	if _, e := os.Stat(dir); e != nil {
		t.Skip("original media unavailable")
	}
	c, e := Load(dir, enterworld.NewTextdataItems(dir))
	if e != nil {
		t.Fatal(e)
	}
	if len(c.Unpriced) != 0 {
		t.Fatalf("unpriced shipped goods %v", c.Unpriced)
	}
}

/*
================
TestEachCatalogueReadsItsOwnDevicesOfASharedPackage

A degree 10 stone that joined the Silk Mall on its gold-shop package
(#4658) carries gold and mall rows: the NPC shop prices it in gold and
ignores the mall's silk and zero-amount rows, the mall sells it for silk
and ignores the gold row (#4702). A device neither owns (a later coin,
128) still blocks the NPC good and fails the mall.
================
*/
func TestEachCatalogueReadsItsOwnDevicesOfASharedPackage(t *testing.T) {
	shared := [][]string{
		{"1", "11", "PACKAGE", "1", "225750"},
		{"1", "11", "PACKAGE", "2", "57"},
		{"1", "11", "PACKAGE", "4", "0"},
		{"1", "11", "PACKAGE", "16", "0"},
	}
	blocked := map[string]bool{}
	prices := npcShopPrices(shared, blocked)
	if blocked["PACKAGE"] || prices.gold["PACKAGE"] != 225750 {
		t.Fatalf("npc shop: blocked %v gold %d", blocked["PACKAGE"], prices.gold["PACKAGE"])
	}
	tables := mallFixtureTables()
	tables["refpricepolicyofitem"] = shared
	catalog, err := buildMallCatalog(tables)
	if err != nil || len(catalog.Offers) != 1 || catalog.Offers[0].Silk != 57 || !catalog.Offers[0].AllowsPoints {
		t.Fatalf("mall: %v %+v", err, catalog)
	}
	if catalog.Offers[0].CurrencyMask&PaymentGold != 0 {
		t.Fatalf("mall offer carries the gold device: mask %d", catalog.Offers[0].CurrencyMask)
	}
	coin := append(append([][]string(nil), shared...), []string{"1", "11", "PACKAGE", "128", "5"})
	blocked = map[string]bool{}
	npcShopPrices(coin, blocked)
	if !blocked["PACKAGE"] {
		t.Fatal("npc shop sold a package with a device it cannot charge")
	}
	tables["refpricepolicyofitem"] = coin
	if _, err := buildMallCatalog(tables); err == nil {
		t.Fatal("mall accepted a device it cannot charge")
	}
}
