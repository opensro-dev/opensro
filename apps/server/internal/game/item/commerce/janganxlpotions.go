/*
===========================================================================

janganxlpotions.go - X-Large potions at Jangan's potion merchant

Port-only, not native (owner, 2026-11-07, #2771). Each v1.150 town sells
its own potion sizes: Jangan's herbalist (STORE_CH_POTION_TAB1) stocks the
two smallest HP and MP potions, and the X-Large pair sells only at Hotan's
herbalist (STORE_KT_POTION_TAB1), at 900 gold each (refpricepolicyofitem).
While SRO_JANGAN_XL_POTIONS is on - the default - the same two rows join
Jangan's tab at Hotan's price, as extra goods (extragoods.go), so quotes,
the purchase limit and selling back follow the authored path. "off"
restores the shipped v1.150 Jangan shop exactly.

===========================================================================
*/
package commerce

import (
	"fmt"
	"os"
	"strings"
)

// EnvJanganXLPotions names the switch: "on" (the default) or "off".
const EnvJanganXLPotions = "SRO_JANGAN_XL_POTIONS"

// janganPotionTab is Jangan's potion tab, the one the extra-goods
// defaults already stock with vigor potions.
const janganPotionTab = "STORE_CH_POTION_TAB1"

// janganXLPotionPrice is the X-Large potions' native price: Hotan's
// price-policy rows charge 900 for both packages.
const janganXLPotionPrice = 900

/*
================
JanganXLPotionsFromEnv

The switch alone: unset or on is the owner's default-on; anything else
but an off value is a boot error naming the setting.
================
*/
func JanganXLPotionsFromEnv() (bool, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvJanganXLPotions))) {
	case "", "1", "on", "true":
		return true, nil
	case "0", "off", "false":
		return false, nil
	}
	return false, fmt.Errorf("%s=%q: want on or off", EnvJanganXLPotions, os.Getenv(EnvJanganXLPotions))
}

/*
================
JanganXLPotionGoodsFromEnv

The X-Large pair as extra goods on Jangan's potion tab while the switch
is on; nil while off (the native Jangan shop). Callers append these to
the extra-goods list wherever the shard's catalogue is loaded.
================
*/
func JanganXLPotionGoodsFromEnv() ([]ExtraGood, error) {
	on, err := JanganXLPotionsFromEnv()
	if err != nil || !on {
		return nil, err
	}
	return []ExtraGood{
		{Tab: janganPotionTab, Codename: "ITEM_ETC_HP_POTION_05", Price: janganXLPotionPrice},
		{Tab: janganPotionTab, Codename: "ITEM_ETC_MP_POTION_05", Price: janganXLPotionPrice},
	}, nil
}
