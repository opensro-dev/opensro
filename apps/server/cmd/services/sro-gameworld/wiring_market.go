/*
===========================================================================

wiring_market.go - the market's expiry tick and in-game desk
(opensro#3529, opensro-web#148)

Port-only, not native (SRO_CONSIGNMENT, off by default): with the flag on,
the GameWorld returns expired listings every market.ExpiryInterval
(game/market), and the action runtime serves the in-game window
(game/action market.go). Off, nothing runs and the window's lane answers
"unavailable".

===========================================================================
*/
package main

import (
	"context"

	"golang.org/x/sync/errgroup"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/game/market"
	"opensro.online/server/internal/transport"
	"opensro.online/server/internal/transport/worldsession"
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

/*
================
wireGameMarket

The in-game window's desk: the same game/market.Service the website's
routes build (public_market.go), over the same store and references.
Online is the published session list, which the account view shows
beside each character.
================
*/
func wireGameMarket(gameplay *gameplayPlane, hub *transport.Hub, authority *store.Store, shard string) {
	if authority == nil || !market.FromEnv() {
		return
	}
	bridge := worldsession.New(hub)
	online := func(characterID int64) bool {
		for _, view := range bridge.SnapshotSessionViews() {
			if view.DivisionID == shard && view.CharacterID == characterID {
				return true
			}
		}
		return false
	}
	gameplay.items.Market = marketService(gameplay, authority, shard, online)
	// The client learns at world entry that the storage keeper offers it.
	gameplay.deps.Consignment = true
}
