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
	if len(goods) != len(nasrunShopTabs)*len(nasrunSpirits) {
		t.Fatalf("%d goods, want %d", len(goods), len(nasrunShopTabs)*len(nasrunSpirits))
	}
	for _, good := range goods {
		if good.Currency != PaymentNasrunToken || good.Price != nasrunSpiritTokenPrice {
			t.Fatalf("%+v is not a %d-token offer", good, nasrunSpiritTokenPrice)
		}
		if good.Codename == "ITEM_MALL_AVATAR_M_NASRUN_UNIQUE" || good.Codename == "ITEM_MALL_AVATAR_W_NASRUN_UNIQUE" {
			t.Fatalf("the S grade Spirit %s is sold", good.Codename)
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
