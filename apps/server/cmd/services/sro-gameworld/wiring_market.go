/*
===========================================================================

wiring_market.go - the market's expiry tick (opensro#3529,
opensro-web#148)

Port-only, not native (SRO_CONSIGNMENT, off by default): with the flag on,
the GameWorld returns expired listings every market.ExpiryInterval
(game/market). Off, nothing runs.

===========================================================================
*/
package main

import (
	"context"

	"golang.org/x/sync/errgroup"
	"opensro.online/server/internal/game/market"
)

/*
================
goMarketExpiry
================
*/
func (application *gameWorldApplication) goMarketExpiry(ctx context.Context, group *errgroup.Group) {
	if application.authority == nil || !market.FromEnv() {
		return
	}
	group.Go(func() error {
		market.Run(ctx, application.authority)
		return nil
	})
}
