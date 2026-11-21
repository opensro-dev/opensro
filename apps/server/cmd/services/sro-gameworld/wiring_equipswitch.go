/*
===========================================================================

wiring_equipswitch.go - the equipment switch scrolls on the item runtime

Port-only, not native: SRO_WEAPON_SWITCH_SCROLL, SRO_ARMOR_SWITCH_SCROLL
and SRO_ACCESSORY_SWITCH_SCROLL (commerce/equipswitch.go), one flag per
scroll. The action runtime serves a scroll's use and the Item Mall lists
it only while its own flag is on and the build staged its item
(scripts/switch_scroll_import.py); unset keeps the native game.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/commerce"
)

/*
================
applyEquipSwitchScrolls

Reads the three flags into the runtime and returns the enabled scrolls'
mall listings, saying which are on. A flag on whose item the data lacks
(a build with the flag off) serves and lists nothing, and says so.
================
*/
func applyEquipSwitchScrolls(items *action.Runtime, refs enterworld.ItemRefSource) []commerce.MallExtraGood {
	items.EquipSwitch = map[commerce.EquipSwitchKind]bool{}
	for _, scroll := range commerce.EquipSwitchScrolls {
		if commerce.EquipSwitchEnabledFromEnv(scroll) {
			if refs == nil {
				log.Warnf("mall: %s is on but no item table is loaded; the scroll is not sold", scroll.Env)
				continue
			}
			if _, ok := refs.ItemRefByCodename(scroll.Codename); !ok {
				log.Warnf("mall: %s is on but the build staged no %s; the scroll is not sold", scroll.Env, scroll.Codename)
				continue
			}
			items.EquipSwitch[scroll.Kind] = true
			log.Infof("mall: %s listed at %d silk (%s, port-only, not native)", scroll.Codename, scroll.Silk, scroll.Env)
		}
	}
	return commerce.EquipSwitchMallGoods(items.EquipSwitch)
}
