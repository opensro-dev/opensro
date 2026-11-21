/*
===========================================================================

weaponswitch.go - the weapon switch scrolls: identity, rules and listing

Port-only, not native (owner, 2026-11-04; mall only at 1490 silk,
2026-11-21): a scroll bought in the Item Mall turns a weapon in the bag
into the same weapon of another kind, of either race (a Chinese sword
into a Chinese spear or a European dagger of the same degree, grade and
seal), its plus and blue options kept. Behind SRO_WEAPON_SWITCH_SCROLL
(default off) the mall's scroll tab sells it and the action runtime
serves its use (action/weaponswitch.go); off is the native game: the
donor item is sold nowhere and keeps its unsupported-use answer.

v1.150 has no such item, so the scroll reuses a shipped one the native
game never sells, drops or uses: ITEM_MALL_REINFORCE_WEAPON_INC_2_SCROLL
(ref 3790, TypeIDs 3/3/13/11, the weapon reinforce scroll art). Its +1
twin, ref 3789, is already the name-change scroll (action/namechange.go).

This package owns the table because both the shop catalogue (here) and
the action runtime read it, and action already imports commerce.

===========================================================================
*/
package commerce

import (
	"os"
	"strings"
)

// EnvWeaponSwitchScroll gates the whole feature: the shop listing and the
// scroll's use. Off (the default) is the native game.
const EnvWeaponSwitchScroll = "SRO_WEAPON_SWITCH_SCROLL"

/*
================
WeaponSwitchScroll

One scroll row: its item and its Silk price under the authored package
MallPackage (mallextra.go authorMallPackage).
================
*/
type WeaponSwitchScroll struct {
	Codename    string
	MallPackage string
	Silk        uint32
}

// weaponSwitchMallTab is the mall's scroll tab, the name-change scroll's.
const weaponSwitchMallTab = "MALL_CONSUME_SCROLL"

// WeaponSwitchScrolls is every scroll the feature sells (owner,
// 2026-11-21: 1490 silk).
var WeaponSwitchScrolls = []WeaponSwitchScroll{
	{Codename: "ITEM_MALL_REINFORCE_WEAPON_INC_2_SCROLL",
		MallPackage: "PACKAGE_ITEM_MALL_WEAPON_SWITCH", Silk: 1490},
}

/*
================
WeaponSwitchScrollFromEnv

The flag accepts "1", "on" and "true" (NameChangeScrollFromEnv's idiom);
anything else, including unset, is off.
================
*/
func WeaponSwitchScrollFromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvWeaponSwitchScroll))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
WeaponSwitchScrollByCodename

The scroll row an item is, if it is one.
================
*/
func WeaponSwitchScrollByCodename(codename string) (WeaponSwitchScroll, bool) {
	for _, scroll := range WeaponSwitchScrolls {
		if scroll.Codename == codename {
			return scroll, true
		}
	}
	return WeaponSwitchScroll{}, false
}

/*
================
WeaponSwitchMallGoods

The scrolls as authored Item Mall packages on the scroll tab, appended to
SRO_MALL_EXTRA_GOODS's listings while the flag is on.
================
*/
func WeaponSwitchMallGoods() []MallExtraGood {
	goods := make([]MallExtraGood, 0, len(WeaponSwitchScrolls))
	for _, scroll := range WeaponSwitchScrolls {
		goods = append(goods, MallExtraGood{Tab: weaponSwitchMallTab, Package: scroll.MallPackage,
			Item: scroll.Codename, Silk: scroll.Silk})
	}
	return goods
}
