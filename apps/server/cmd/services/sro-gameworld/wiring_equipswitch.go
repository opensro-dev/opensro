/*
===========================================================================

wiring_equipswitch.go - the equipment switch scrolls on the item runtime

Port-only, not native: SRO_WEAPON_SWITCH_SCROLL, SRO_ARMOR_SWITCH_SCROLL
and SRO_ACCESSORY_SWITCH_SCROLL (commerce/equipswitch.go), one flag per
scroll. The action runtime serves a scroll's use and the Item Mall lists
it only while its own flag is on; unset keeps the native game.

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/game/action"
	"opensro.online/server/internal/game/item/commerce"
)

/*
================
applyEquipSwitchScrolls

Reads the three flags into the runtime and returns the enabled scrolls'
mall listings, saying which are on.
================
*/
func applyEquipSwitchScrolls(items *action.Runtime) []commerce.MallExtraGood {
	items.EquipSwitch = map[commerce.EquipSwitchKind]bool{}
	for _, scroll := range commerce.EquipSwitchScrolls {
		if commerce.EquipSwitchEnabledFromEnv(scroll) {
			items.EquipSwitch[scroll.Kind] = true
			log.Infof("mall: %s listed at %d silk (%s, port-only, not native)", scroll.Codename, scroll.Silk, scroll.Env)
		}
	}
	return commerce.EquipSwitchMallGoods(items.EquipSwitch)
}
