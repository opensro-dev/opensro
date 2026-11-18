/*
===========================================================================

pvpshop_test.go - the PVP shard's Samarkand tabs (port-only, not native)

===========================================================================
*/

package commerce

import (
	"os"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/pvpserver"
	"opensro.online/server/internal/testsupport/clientdata"
)

// testBagEnd is a 45-slot bag after the 13 equipment slots.
const testBagEnd uint8 = 58

/*
================
retailCatalog
================
*/
func retailCatalog(t *testing.T) (*Catalog, enterworld.ItemRefSource) {
	t.Helper()
	dir := clientdata.RetailTextdataDir(t)
	if _, e := os.Stat(dir); e != nil {
		t.Skip("original media unavailable")
	}
	refs := enterworld.NewTextdataItems(dir)
	c, err := LoadWithExtraGoods(dir, refs, nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, refs
}

/*
================
TestRegisterPvpTabsSellsAtOneGoldAndPlus12
================
*/
func TestRegisterPvpTabsSellsAtOneGoldAndPlus12(t *testing.T) {
	c, refs := retailCatalog(t)
	tabs := []pvpserver.ShopTab{
		{Npc: "NPC_CA_SMITH", TabID: 90001, Codenames: []string{"ITEM_CH_SWORD_01_A"}, Plus: pvpserver.ShopPlus},
		{Npc: "NPC_CA_ACCESSORY", TabID: 90002, Codenames: []string{"ITEM_ETC_ARCHEMY_ATTRSTONE_PA_09"}},
	}

	if err := c.RegisterPvpTabs(tabs, refs); err != nil {
		t.Fatal(err)
	}

	gear, stone := c.Tabs[90001], c.Tabs[90002]
	if len(gear) != 1 || gear[0].Price != pvpserver.ShopPrice || gear[0].Currency != PaymentGold {
		t.Fatalf("gear offers %+v", gear)
	}
	if gear[0].Contents[0].Plus != 12 || stone[0].Contents[0].Plus != 0 {
		t.Fatalf("plus gear %d stone %d", gear[0].Contents[0].Plus, stone[0].Contents[0].Plus)
	}
	inv := inventory.New(nil, testBagEnd)
	slots, err := GrantPackage(inv, gear[0].Contents, 1, uint16(testBagEnd))
	if err != nil || len(slots) != 1 {
		t.Fatalf("GrantPackage %v %v", slots, err)
	}
	if item, ok := inv.At(slots[0]); !ok || item.Plus != 12 {
		t.Fatalf("bought item %+v", item)
	}
}

/*
================
TestRegisterPvpTabsRefusesAMissingItem
================
*/
func TestRegisterPvpTabsRefusesAMissingItem(t *testing.T) {
	c, refs := retailCatalog(t)
	err := c.RegisterPvpTabs([]pvpserver.ShopTab{{TabID: 90001, Codenames: []string{"ITEM_NOT_THERE"}}}, refs)
	if err == nil {
		t.Fatal("a missing item registered")
	}
}

/*
================
TestPvpTabsOffKeepsTheNativeCatalogue
================
*/
func TestPvpTabsOffKeepsTheNativeCatalogue(t *testing.T) {
	t.Setenv(pvpserver.EnvPvpServer, "on")
	t.Setenv("SRO_SHARD_ID", "global-official")
	c, _ := retailCatalog(t)
	for name := range c.tabIDs {
		if len(name) > len(pvpTabPrefix) && name[:len(pvpTabPrefix)] == pvpTabPrefix {
			t.Fatalf("PVP tab %s registered on another shard", name)
		}
	}
}
