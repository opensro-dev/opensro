/*
===========================================================================

wiring_premium.go - the premium ticket upgrade rule for one division

Port-only, not native (SRO_PREMIUM_UPGRADE, #1624): with the flag on, a
strictly higher-grade premium ticket of the same family replaces the
running lower one at use time instead of the native 0x1894 refusal.
Off is native. The grade order and replacement semantics live with the
tickets (premiumticket.go).

===========================================================================
*/
package main

import (
	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/game/action"
)

/*
================
wirePremiumUpgrade
================
*/
func (game *gameplayPlane) wirePremiumUpgrade() {
	game.items.PremiumUpgrade = action.PremiumUpgradeFromEnv()
	if game.items.PremiumUpgrade {
		log.Infof("items: a higher-grade premium replaces the running lower one (%s, port-only, not native)", action.EnvPremiumUpgrade)
	}
}
