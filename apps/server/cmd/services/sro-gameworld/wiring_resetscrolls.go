/*
===========================================================================

wiring_resetscrolls.go - the reset scrolls' switches, listing and hooks

Port-only, not native (commerce/resetscrolls.go). Each scroll is on only
while its switch is on and the build staged its item: a switch on
without the item logs one warning and lists nothing, and the boot goes
on.

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
applyResetScrolls

Turns on each reset scroll whose switch is on and whose item stands in
the data, and returns their mall listings.
================
*/
func applyResetScrolls(items *action.Runtime, refs enterworld.ItemRefSource) []commerce.MallExtraGood {
	enabled := map[commerce.ResetScrollKind]bool{}
	for _, scroll := range commerce.ResetScrolls {
		if !commerce.ResetScrollEnabledFromEnv(scroll) {
			continue
		}
		if refs == nil {
			log.Warnf("mall: %s is on but no item data is loaded; %s is not listed", scroll.Env, scroll.Codename)
			continue
		}
		if _, found := refs.ItemRefByCodename(scroll.Codename); !found {
			log.Warnf("mall: %s is on but the build staged no %s (scripts/reset_scroll_import.py); not listed", scroll.Env, scroll.Codename)
			continue
		}
		enabled[scroll.Kind] = true
		log.Infof("mall: %s listed at %d silk (%s, port-only, not native)", scroll.Codename, scroll.Silk, scroll.Env)
	}
	items.ResetScrolls = enabled
	return commerce.ResetScrollMallGoods(enabled)
}
