/*
===========================================================================

catalog_test.go - the shipped commerce catalog

The NPC shop catalog loads from the shipped tables.

===========================================================================
*/
package commerce

import (
	"os"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestShippedCatalog
================
*/
func TestShippedCatalog(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
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
}
