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
}
