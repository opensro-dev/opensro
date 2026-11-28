/*
===========================================================================

mallp2wscrolls.go - the pay-to-win scrolls the shard keeps out of the mall

Port-only, not native (owner, 2026-11-28). The v1.150 mall sells combat
and recovery scrolls for silk, and the later mall import adds more of
the same family: damage increase, absorption and resistance for 10 or
20 percent, the resurrection scrolls and the instant return. With
SRO_MALL_P2W_SCROLLS_OFF on, the GameWorld withholds every mall package
that holds one of them, the single scroll and its pack of eleven alike,
so silk never buys a combat edge. Off (the default) is the shipped mall.

Only the listing goes: the item rows stay in the tree, so a scroll a
player already owns keeps its use, and drops or GM grants still make
them. The EXP and skill EXP boosters and the reverse return scroll are
not on the list and stay sold.

===========================================================================
*/
package commerce

import "opensro.online/server/internal/game/enterworld"

// EnvMallP2WScrollsOff names the switch: "on" withholds the scrolls,
// "off" (the default) is the shipped mall.
const EnvMallP2WScrollsOff = "SRO_MALL_P2W_SCROLLS_OFF"

// P2WScrollItems is every scroll item the switch keeps out of the mall
// (owner's list, 2026-11-28).
var P2WScrollItems = []string{
	"ITEM_MALL_DAMAGE_ABS_10P_SCROLL",
	"ITEM_MALL_DAMAGE_ABS_20P_SCROLL",
	"ITEM_MALL_DAMAGE_INC_10P_SCROLL",
	"ITEM_MALL_DAMAGE_INC_20P_SCROLL",
	"ITEM_MALL_DAMAGE_INC_ABS_10P_SCROLL",
	"ITEM_MALL_DAMAGE_INC_ABS_20P_SCROLL",
	"ITEM_MALL_RESURRECTION_SCROLL",
	"ITEM_MALL_RESURRECTION_60P_SCROLL",
	"ITEM_MALL_RESURRECTION_100P_SCROLL",
	"ITEM_MALL_RETURN_SCROLL_HIGH_SPEED",
}

/*
================
MallP2WScrollsOffFromEnv

Unset or off is the shipped mall; a typo is a boot error.
================
*/
func MallP2WScrollsOffFromEnv() (bool, error) {
	return switchFromEnv(EnvMallP2WScrollsOff, false)
}

/*
================
MallCatalog.WithholdItemsNamed

Removes every mall offer whose package holds one of codenames; the count
is the offers removed. A codename the tree lacks names nothing: the
later items exist only in a variant build.
================
*/
func (c *MallCatalog) WithholdItemsNamed(refs enterworld.ItemRefSource, codenames []string) int {
	ids := make(map[uint32]bool, len(codenames))
	for _, codename := range codenames {
		if ref, ok := refs.ItemRefByCodename(codename); ok && ref != nil {
			ids[ref.RefObjID] = true
		}
	}
	if len(ids) == 0 {
		return 0
	}
	return c.WithholdPackagesHolding(func(refObjID uint32) bool { return ids[refObjID] })
}
