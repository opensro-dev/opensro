/*
===========================================================================

market.go - the market escrow's records (opensro#3529, opensro-web#148)

Port-only, not native: v1.150 has no consignment. A listed item lives in
one market_listings row and nowhere else until it is sold, returned or
taken back; the store moves it in one transaction per door
(store/market.go). The in-game window and the website are two front ends
on the same rows.

===========================================================================
*/
package domain

import "errors"

// The currencies a listing is priced in.
const (
	MarketGold = "gold"
	MarketSilk = "silk"
)

// A listing's states. Open is buyable; sold and returned are final; held
// is a returned item that found the seller's warehouse full and waits in
// My listings until it is taken back.
const (
	MarketOpen     = "open"
	MarketSold     = "sold"
	MarketReturned = "returned"
	MarketHeld     = "held"
)

// The refusals every front end maps to its own words. None changes
// anything: a refused door leaves the item and the money where they were.
var (
	ErrMarketNotFound      = errors.New("market: no such listing")
	ErrMarketChanged       = errors.New("market: the listing changed")
	ErrMarketOwnListing    = errors.New("market: a seller cannot buy their own listing")
	ErrMarketNoItem        = errors.New("market: no such item")
	ErrMarketNotTradeable  = errors.New("market: the item cannot be traded")
	ErrMarketPrice         = errors.New("market: invalid price")
	ErrMarketCap           = errors.New("market: too many open listings")
	ErrMarketFunds         = errors.New("market: not enough to pay")
	ErrMarketWarehouseFull = errors.New("market: the warehouse is full")
	ErrMarketNotHeld       = errors.New("market: the listing is not waiting to be taken back")
	ErrMarketUnavailable   = errors.New("market: unavailable")
)

/*
================
MarketRules

The owner's numbers (2026-11-16, the design doc): the fee in percent of
the price taken from the seller, how long a listing stays open, and how
many open listings one account may have. game/market owns the values.
================
*/
type MarketRules struct {
	FeePercent int64
	DurationMs int64
	OpenCap    int
}

/*
================
MarketFee

The fee on one sale, rounded down: a sale below 100 pays none.
================
*/
func (r MarketRules) MarketFee(price int64) int64 {
	return price * r.FeePercent / 100
}

/*
================
MarketListing
================
*/
type MarketListing struct {
	ID            int64  `json:"id"`
	Division      string `json:"division"`
	SellerAccount string `json:"sellerAccount"`
	// SellerChar is the listing character (a bag listing), 0 for one made
	// from the warehouse on the website.
	SellerChar int64        `json:"sellerChar"`
	Item       InventoryRow `json:"item"`
	Currency   string       `json:"currency"`
	Price      int64        `json:"price"`
	ListedMs   int64        `json:"listedMs"`
	ExpiresMs  int64        `json:"expiresMs"`
	State      string       `json:"state"`
	// Version rises on every change: a buy names the version it saw, so a
	// listing that changed underneath it is refused, never bought blind.
	Version int64 `json:"version"`
}

/*
================
MarketSale

One settled sale, both parties named: the audit trail.
================
*/
type MarketSale struct {
	ListingID    int64  `json:"listingId"`
	BuyerAccount string `json:"buyerAccount"`
	BuyerChar    int64  `json:"buyerChar"`
	Currency     string `json:"currency"`
	Price        int64  `json:"price"`
	Fee          int64  `json:"fee"`
	AtMs         int64  `json:"atMs"`
}

/*
================
MarketListRequest

One listing: from the warehouse (Character nil, the website) or from a
character's bag (the game, on the action lane). Admit is the trade rule
(the exchange's: RefObjCommon CanTrade, not forge-bound, not a summoned
pet); the store knows no item references, so the caller supplies it.
================
*/
type MarketListRequest struct {
	Division  string
	Account   string
	Character *Character
	Slot      int64
	Currency  string
	Price     int64
	Admit     func(InventoryRow) error
	Rules     MarketRules
}

/*
================
MarketBuyRequest

A buy names the listing and the version the buyer saw. A gold listing is
paid from Payer, one of the buyer's characters; a Silk listing from the
buyer's purchased Silk. The caller makes sure a website buy's Payer is
offline (game/market), since its record is then the store's alone.
================
*/
type MarketBuyRequest struct {
	ListingID    int64
	Version      int64
	BuyerAccount string
	Payer        *Character
	Rules        MarketRules
}

/*
================
MarketQuery

Which open listings a search wants. Codenames, when given, narrows the
search to those items (the caller resolves a category or a name to
codenames from the item references).
================
*/
type MarketQuery struct {
	Division  string
	Currency  string
	Codenames []string
	Limit     int
	Offset    int
}
