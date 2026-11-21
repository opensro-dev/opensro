/*
===========================================================================

equipswitch.go - the equipment switch scrolls: identity, flags and listing

Port-only, not native (owner, 2026-11-04 for the weapon; 2026-11-21 for
armor and accessories, mall only, enhanced items admitted): three Item
Mall scrolls turn an item in the bag into its twin, its plus and blue
options kept (action/weaponswitch.go, action/raceswitch.go):

  - the weapon switch scroll: a weapon into another kind, of either race
    (a Chinese sword into a European dagger of the same degree, grade
    and seal), 1490 silk;
  - the armor switch scroll: a piece of armor or a shield into the same
    piece of the other race (robe <-> garment, light <-> protector,
    heavy <-> armor), 990 silk;
  - the accessory switch scroll: an earring, necklace or ring into the
    same accessory of the other race, 990 silk.

Each scroll has its own flag (default off); off is the native game: the
donor item is sold nowhere and keeps its unsupported-use answer.

v1.150 has no such items, so each scroll is an item of its own the
variant build stages while its flag is on (scripts/switch_scroll_import.py):
a copy of a shipped reinforce scroll's row whose art fits (the +2 weapon,
+1 armor and +1 accessory reinforce scrolls), under its own codename,
identity, name and description. The shipped scrolls stay untouched
(owner, 2026-11-21). A flag on without its item in the data lists
nothing (sro-gameworld wiring_equipswitch.go).

This package owns the table because both the mall catalogue (here) and
the action runtime read it, and action already imports commerce.

===========================================================================
*/
package commerce

import (
	"os"
	"strings"
)

// The scrolls' flags. Off (the default) is the native game.
const (
	EnvWeaponSwitchScroll    = "SRO_WEAPON_SWITCH_SCROLL"
	EnvArmorSwitchScroll     = "SRO_ARMOR_SWITCH_SCROLL"
	EnvAccessorySwitchScroll = "SRO_ACCESSORY_SWITCH_SCROLL"
)

// EquipSwitchKind is what a scroll switches.
type EquipSwitchKind uint8

const (
	SwitchWeapon EquipSwitchKind = iota
	SwitchArmor
	SwitchAccessory
)

/*
================
EquipSwitchScroll

One scroll row: what it switches, its flag, its item and its Silk price
under the authored package MallPackage (mallextra.go authorMallPackage).
================
*/
type EquipSwitchScroll struct {
	Kind        EquipSwitchKind
	Env         string
	Codename    string
	MallPackage string
	Silk        uint32
}

// equipSwitchMallTab is the mall's scroll tab, the name-change scroll's.
const equipSwitchMallTab = "MALL_CONSUME_SCROLL"

// EquipSwitchScrolls is every scroll the feature sells (owner's prices,
// 2026-11-21).
var EquipSwitchScrolls = []EquipSwitchScroll{
	{Kind: SwitchWeapon, Env: EnvWeaponSwitchScroll, Codename: "ITEM_MALL_WEAPON_SWITCH_SCROLL",
		MallPackage: "PACKAGE_ITEM_MALL_WEAPON_SWITCH", Silk: 1490},
	{Kind: SwitchArmor, Env: EnvArmorSwitchScroll, Codename: "ITEM_MALL_ARMOR_SWITCH_SCROLL",
		MallPackage: "PACKAGE_ITEM_MALL_ARMOR_SWITCH", Silk: 990},
	{Kind: SwitchAccessory, Env: EnvAccessorySwitchScroll, Codename: "ITEM_MALL_ACCESSORY_SWITCH_SCROLL",
		MallPackage: "PACKAGE_ITEM_MALL_ACCESSORY_SWITCH", Silk: 990},
}

/*
================
EquipSwitchEnabledFromEnv

A scroll's flag accepts "1", "on" and "true" (NameChangeScrollFromEnv's
idiom); anything else, including unset, is off.
================
*/
func EquipSwitchEnabledFromEnv(scroll EquipSwitchScroll) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(scroll.Env))) {
	case "1", "on", "true":
		return true
	}
	return false
}

/*
================
EquipSwitchScrollByCodename

The scroll row an item is, if it is one.
================
*/
func EquipSwitchScrollByCodename(codename string) (EquipSwitchScroll, bool) {
	for _, scroll := range EquipSwitchScrolls {
		if scroll.Codename == codename {
			return scroll, true
		}
	}
	return EquipSwitchScroll{}, false
}

/*
================
EquipSwitchMallGoods

The enabled scrolls as authored Item Mall packages on the scroll tab,
appended to SRO_MALL_EXTRA_GOODS's listings, in table order.
================
*/
func EquipSwitchMallGoods(enabled map[EquipSwitchKind]bool) []MallExtraGood {
	goods := make([]MallExtraGood, 0, len(EquipSwitchScrolls))
	for _, scroll := range EquipSwitchScrolls {
		if enabled[scroll.Kind] {
			goods = append(goods, MallExtraGood{Tab: equipSwitchMallTab, Package: scroll.MallPackage,
				Item: scroll.Codename, Silk: scroll.Silk})
		}
	}
	return goods
}
