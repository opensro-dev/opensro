/*
===========================================================================

nasrungoods_test.go - the token goods and the switch that lists them

===========================================================================
*/
package commerce

import (
	"testing"

	"opensro.online/server/internal/game/nasrun"
)

/*
================
TestNasrunGoodsAreTokenPricedSpiritsOnEveryHonorShop
================
*/
func TestNasrunGoodsAreTokenPricedSpiritsOnEveryHonorShop(t *testing.T) {
	goods := NasrunGoods()
	want := len(nasrunShopTabs) * (len(nasrunSpirits) + 3)
	if len(goods) != want {
		t.Fatalf("%d goods, want %d", len(goods), want)
	}
	jewels, gears := map[string]bool{}, map[string]int{}
	for _, good := range goods {
		price, currency := uint64(nasrunSpiritTokenPrice), PaymentNasrunToken
		switch {
		case good.Codename == nasrunJewel:
			price = nasrunJewelTokenPrice
			jewels[good.Tab] = true
		case good.Codename == nasrunGear && good.Currency == PaymentGold:
			price, currency = nasrunGearGoldPrice, PaymentGold
			gears[good.Tab]++
		case good.Codename == nasrunGear:
			price = nasrunGearTokenPrice
			gears[good.Tab]++
		}
		if good.Currency != currency || good.Price != price {
			t.Fatalf("%+v is not a %d offer in currency %d", good, price, currency)
		}
		if good.Codename == "ITEM_MALL_AVATAR_M_NASRUN_UNIQUE" || good.Codename == "ITEM_MALL_AVATAR_W_NASRUN_UNIQUE" {
			t.Fatalf("the S grade Spirit %s is sold", good.Codename)
		}
	}
	// Sabakun's jewel (step 3c) is on every token tab.
	if len(jewels) != len(nasrunShopTabs) {
		t.Fatalf("the jewel is on %d tabs, want %d", len(jewels), len(nasrunShopTabs))
	}
	// The gear (slice 4) is on every token tab, for tokens and for gold.
	for _, tab := range nasrunShopTabs {
		if gears[tab] != 2 {
			t.Fatalf("tab %s lists the gear %d times, want 2", tab, gears[tab])
		}
	}
}

/*
================
TestNasrunGoodsFollowTheSwitch

Off (and unset) is the native stock; on lists the token goods.
================
*/
func TestNasrunGoodsFollowTheSwitch(t *testing.T) {
	for value, want := range map[string]bool{"": false, "off": false, "on": true} {
		t.Setenv(nasrun.Env, value)
		if got := len(NasrunGoodsFromEnv()) > 0; got != want {
			t.Fatalf("%s=%q lists goods %v, want %v", nasrun.Env, value, got, want)
		}
	}
}

/*
================
TestNasrunIsNotAnExtraGoodsSetting

The rules page shows SRO_SHOP_EXTRA_GOODS's row from ExtraGoodsFromEnv:
with that setting off and SRO_NASRUN on, the row must stay off, so the
token goods never come back through it.
================
*/
func TestNasrunIsNotAnExtraGoodsSetting(t *testing.T) {
	t.Setenv(EnvShopExtraGoods, "")
	t.Setenv(nasrun.Env, "on")
	goods, err := ExtraGoodsFromEnv()
	if err != nil || len(goods) != 0 {
		t.Fatalf("extra goods off, Nasrun on: %d goods, %v; want none", len(goods), err)
	}
	if len(NasrunGoodsFromEnv()) == 0 {
		t.Fatal("Nasrun on lists no token goods")
	}
}
