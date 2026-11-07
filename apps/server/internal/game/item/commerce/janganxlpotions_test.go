/*
===========================================================================

janganxlpotions_test.go - the X-Large potions switch (port-only, not native)

===========================================================================
*/
package commerce

import (
	"reflect"
	"strings"
	"testing"
)

/*
================
TestJanganXLPotionsFromEnv

Unset is the owner's default-on; only an off value restores the native
Jangan shop, and anything else names the setting.
================
*/
func TestJanganXLPotionsFromEnv(t *testing.T) {
	for value, want := range map[string]bool{
		"": true, "on": true, "ON": true, "1": true, "true": true,
		"off": false, "OFF": false, "0": false, "false": false,
	} {
		t.Setenv(EnvJanganXLPotions, value)
		on, err := JanganXLPotionsFromEnv()
		if err != nil || on != want {
			t.Fatalf("%q = %v, %v; want %v", value, on, err, want)
		}
	}
	t.Setenv(EnvJanganXLPotions, "banana")
	if _, err := JanganXLPotionsFromEnv(); err == nil || !strings.Contains(err.Error(), EnvJanganXLPotions) {
		t.Fatalf("bad switch accepted (%v)", err)
	}
}

/*
================
TestJanganXLPotionGoodsFromEnv

On, the goods are the X-Large pair on Jangan's potion tab at Hotan's
native price; off, there are none.
================
*/
func TestJanganXLPotionGoodsFromEnv(t *testing.T) {
	t.Setenv(EnvJanganXLPotions, "on")
	goods, err := JanganXLPotionGoodsFromEnv()
	want := []ExtraGood{
		{Tab: "STORE_CH_POTION_TAB1", Codename: "ITEM_ETC_HP_POTION_05", Price: 900},
		{Tab: "STORE_CH_POTION_TAB1", Codename: "ITEM_ETC_MP_POTION_05", Price: 900},
	}
	if err != nil || !reflect.DeepEqual(goods, want) {
		t.Fatalf("on = %+v, %v", goods, err)
	}
	t.Setenv(EnvJanganXLPotions, "off")
	if goods, err := JanganXLPotionGoodsFromEnv(); err != nil || goods != nil {
		t.Fatalf("off = %+v, %v; want the native stock", goods, err)
	}
}

/*
================
TestJanganPotionTabSellsXLPotionsUnlessOff

Against the shipped data: with the switch on, the X-Large pair takes
Jangan potion tab's next free slots after its eight authored goods, at
900; off, the tab is exactly the authored eight.
================
*/
func TestJanganPotionTabSellsXLPotionsUnlessOff(t *testing.T) {
	for _, off := range []bool{false, true} {
		c, refs := shippedCatalog(t)
		t.Setenv(EnvJanganXLPotions, map[bool]string{false: "on", true: "off"}[off])
		goods, err := JanganXLPotionGoodsFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if err := c.AddExtraGoods(goods, refs); err != nil {
			t.Fatal(err)
		}
		offers := c.Tabs[c.tabIDs[janganPotionTab]]
		byCode := map[string]Offer{}
		for _, offer := range offers {
			if offer.Ref != nil {
				byCode[offer.Ref.Codename] = offer
			}
		}
		hp, hpOK := byCode["ITEM_ETC_HP_POTION_05"]
		mp, mpOK := byCode["ITEM_ETC_MP_POTION_05"]
		if off {
			if hpOK || mpOK || len(offers) != 8 {
				t.Fatalf("off: tab = %d offers, HP %v, MP %v; want the authored eight", len(offers), hpOK, mpOK)
			}
			continue
		}
		if !hpOK || !mpOK || len(offers) != 10 {
			t.Fatalf("on: tab = %d offers, HP %v, MP %v; want the authored eight plus the pair", len(offers), hpOK, mpOK)
		}
		if hp.Price != 900 || mp.Price != 900 || hp.Slot != 8 || mp.Slot != 9 {
			t.Fatalf("on: HP %+v, MP %+v; want 900 gold at slots 8 and 9", hp, mp)
		}
	}
}
