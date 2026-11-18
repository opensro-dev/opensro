/*
===========================================================================

market.go - the market's rules and its expiry tick (opensro#3529,
opensro-web#148)

Port-only, not native (SRO_CONSIGNMENT, off by default): v1.150 has no
consignment. Off, nothing here runs, no market route is served and the
market tables are never written, which is native.

The escrow itself is the store's (data/store/market.go): one transaction
per door. This package owns the numbers and the clock. The numbers are
the owner's (2026-11-16, the market design doc): a 1% fee taken from the
seller, matching the stall network's commission; listings open for 24
hours; 20 open listings per account; no minimum price. On 2026-11-19 the
owner added offers, a named seller with an anonymous choice, the buyer's
pick of bag or warehouse, and gold paid to the listing character.
The owner left two numbers to this port (2026-11-19). OpenOfferCap
mirrors the listing cap: 20 offers lets a player haggle across the
market, and the cap bounds lowball spam, because an offer holds no gold
(an accept checks the buyer can still pay). The fee rounds down: a sale
under 100 pays none. A floor of 1 would take 20% of a 5 Silk sale, and
the fee is a sink on large sales, not a tax on small trades.

===========================================================================
*/
package market

import (
	"context"
	"os"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// EnvConsignment switches the market on ("on", "1" or "true").
const EnvConsignment = "SRO_CONSIGNMENT"

const (
	// FeePercent is the seller's fee on a sale, rounded down (a sale
	// under 100 pays none).
	FeePercent = 1
	// ListingDuration is how long a listing stays open.
	ListingDuration = 24 * time.Hour
	// OpenListingCap bounds one account's open listings.
	OpenListingCap = 20
	// OpenOfferCap bounds one account's open offers.
	OpenOfferCap = 20
	// ExpiryInterval is how often expired listings are returned.
	ExpiryInterval = time.Minute
	// ExpiryBatch bounds one pass; a longer backlog drains over passes.
	ExpiryBatch = 200
)

/*
================
FromEnv
================
*/
func FromEnv() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(EnvConsignment))) {
	case "on", "1", "true":
		return true
	}
	return false
}

/*
================
Rules
================
*/
func Rules() domain.MarketRules {
	return domain.MarketRules{
		FeePercent: FeePercent,
		DurationMs: ListingDuration.Milliseconds(),
		OpenCap:    OpenListingCap,
		OfferCap:   OpenOfferCap,
	}
}

/*
================
Store

The store door the expiry tick needs.
================
*/
type Store interface {
	ExpireMarketListings(limit int) (int, error)
	SettleOrphanedMarketProceeds(limit int) (int, error)
}

/*
================
Expire

One pass: returns up to ExpiryBatch expired listings. A failure is logged
and retried next pass; nothing is lost, since a listing stays open (and
unbuyable, being past its time) until it is returned.
================
*/
func Expire(store Store) int {
	returned, err := store.ExpireMarketListings(ExpiryBatch)
	if err != nil {
		log.WithError(err).Warn("market: expiry pass failed; retrying next pass")
	}
	// Gold owed to a character that no longer exists goes to its account
	// warehouse (store market_proceeds.go), on the same clock.
	if paid, err := store.SettleOrphanedMarketProceeds(ExpiryBatch); err != nil {
		log.WithError(err).Warn("market: orphaned proceeds pass failed; retrying next pass")
	} else if paid > 0 {
		log.Infof("market: %d sale(s) owed to a removed character paid to its warehouse", paid)
	}
	return returned
}

/*
================
Run

Returns expired listings every ExpiryInterval until ctx ends.
================
*/
func Run(ctx context.Context, store Store) {
	ticker := time.NewTicker(ExpiryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if returned := Expire(store); returned > 0 {
				log.Infof("market: %d expired listing(s) returned", returned)
			}
		}
	}
}
