/*
===========================================================================

nasrungoods.go - the Devil's Spirit token shop's goods

Port-only, not native (#3456 step 3b), on with SRO_NASRUN; off ships the
v1.150 stock exactly. Tokens are earned on unique kills (step 3a,
game/nasrun/tokens.go) and spent here.

v1.150 has no token item and no token shop. The closest native shape is
the Training Camp honor shop: a per-character point balance spent at an
NPC shop row priced in that point. So the token goods are offers priced
in PaymentNasrunToken on the second tab of every town's honor shop
(STORE_*_HONOR_TAB2), the tab whose authored stock is a single package
(inferred placement; recorded on #3456).

The owner's prices (2026-11-16, #3456): the Spirit 60 tokens. Only goods
whose use is built are listed: Sabakun's jewel joins with Nasrun alchemy
(step 3c) and the extension gear with its use (step 4), so the shop never
sells an item that does nothing. The A grade Spirits are the plain, yellow
and blue rows; the _UNIQUE pair is the S grade and is not sold.

===========================================================================
*/
package commerce

import (
	"os"

	"opensro.online/server/internal/game/nasrun"
)

// nasrunSpiritTokenPrice is the owner's Spirit price in tokens.
const nasrunSpiritTokenPrice = 60

// nasrunShopTabs are the honor shops' second tabs, one per town.
var nasrunShopTabs = []string{
	"STORE_CH_HONOR_TAB2",
	"STORE_WC_HONOR_TAB2",
	"STORE_KT_HONOR_TAB2",
	"STORE_EU_HONOR_TAB2",
	"STORE_CA_HONOR_TAB2",
}

// nasrunSpirits are the A grade Spirits (scripts/nasrun_import.py).
var nasrunSpirits = []string{
	"ITEM_MALL_AVATAR_M_NASRUN",
	"ITEM_MALL_AVATAR_W_NASRUN",
	"ITEM_MALL_AVATAR_M_NASRUN_YELLOW",
	"ITEM_MALL_AVATAR_W_NASRUN_YELLOW",
	"ITEM_MALL_AVATAR_M_NASRUN_BLUE",
	"ITEM_MALL_AVATAR_W_NASRUN_BLUE",
}

/*
================
NasrunGoods

The token goods: every A grade Spirit on every honor shop's second tab,
priced in tokens.
================
*/
func NasrunGoods() []ExtraGood {
	var goods []ExtraGood
	for _, tab := range nasrunShopTabs {
		for _, spirit := range nasrunSpirits {
			goods = append(goods, ExtraGood{Tab: tab, Codename: spirit, Price: nasrunSpiritTokenPrice, Currency: PaymentNasrunToken})
		}
	}
	return goods
}

/*
================
NasrunGoodsFromEnv

NasrunGoods while SRO_NASRUN is on, else nil (the native stock).
================
*/
func NasrunGoodsFromEnv() []ExtraGood {
	if !nasrun.Enabled(os.Getenv(nasrun.Env)) {
		return nil
	}
	return NasrunGoods()
}
