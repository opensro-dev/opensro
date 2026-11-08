/*
===========================================================================

europeanshops_test.go - the shipped European shop rows (#2566)

The v1.150 tables themselves decide which towns sell European goods:
Donwhang's smith and every honor merchant stock Chinese packages only,
while Hotan keeps a dedicated European smith tab the catalog must
admit. These pins keep a future admission filter from silently
dropping the European rows, and keep the native absences visible.

===========================================================================
*/
package commerce

import (
	"strings"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"opensro.online/server/internal/testsupport/tier"
)

/*
================
TestShippedCatalogEuropeanRows

STORE_KT_SMITH_EU_TAB1 lists 45 European weapon packages in
refshopgoods.txt, every one of them priced in refpricepolicyofitem;
the catalog admits all 45. Donwhang (STORE_WC_*) and the honor tabs
list no European packages at all in this version - the original
client shows the same, so the port must not invent any either.
================
*/
func TestShippedCatalogEuropeanRows(t *testing.T) {
	tier.Long(t, "the shipped catalogue admits its European rows")
	dir := gamedatatest.TextdataDir(t)
	c, e := Load(dir, enterworld.NewTextdataItems(dir))
	if e != nil {
		t.Fatal(e)
	}
	id, ok := c.tabIDs["STORE_KT_SMITH_EU_TAB1"]
	if !ok {
		t.Fatal("STORE_KT_SMITH_EU_TAB1 has no tab id")
	}
	offers := c.Tabs[id]
	if len(offers) != 45 {
		t.Fatalf("STORE_KT_SMITH_EU_TAB1 admits %d offers, want the table's 45", len(offers))
	}
	axe := false
	for _, o := range offers {
		if !strings.HasPrefix(o.Ref.Codename, "ITEM_EU_") {
			t.Fatalf("STORE_KT_SMITH_EU_TAB1 sells %s, not a European item", o.Ref.Codename)
		}
		if o.Ref.Codename == "ITEM_EU_AXE_05_A" {
			if o.Price != 167000 || o.Currency != PaymentGold {
				t.Fatalf("ITEM_EU_AXE_05_A = price %d currency %d, want 167000 gold", o.Price, o.Currency)
			}
			axe = true
		}
	}
	if !axe {
		t.Fatal("STORE_KT_SMITH_EU_TAB1 lacks ITEM_EU_AXE_05_A")
	}
	dwID, ok := c.tabIDs["STORE_WC_SMITH_TAB1"]
	if !ok {
		t.Fatal("STORE_WC_SMITH_TAB1 has no tab id")
	}
	for _, o := range c.Tabs[dwID] {
		if strings.HasPrefix(o.Ref.Codename, "ITEM_EU_") {
			t.Fatalf("Donwhang's smith sells %s; the v1.150 table lists none", o.Ref.Codename)
		}
	}
	honorID, ok := c.tabIDs["STORE_EU_HONOR_TAB1"]
	if !ok {
		t.Fatal("STORE_EU_HONOR_TAB1 has no tab id")
	}
	honorSword := false
	for _, o := range c.Tabs[honorID] {
		if strings.HasPrefix(o.Ref.Codename, "ITEM_EU_") {
			t.Fatalf("the Constantinople honor tab sells %s; the v1.150 table lists none", o.Ref.Codename)
		}
		if o.Ref.Codename == "ITEM_CH_BLADE_08_A_RARE_HONOR" {
			if o.Price != 2700000 || o.HonorPrice != 1000 {
				t.Fatalf("ITEM_CH_BLADE_08_A_RARE_HONOR = (%d, %d), want (2700000, 1000)", o.Price, o.HonorPrice)
			}
			honorSword = true
		}
	}
	if !honorSword {
		t.Fatal("STORE_EU_HONOR_TAB1 lacks ITEM_CH_BLADE_08_A_RARE_HONOR")
	}
}
