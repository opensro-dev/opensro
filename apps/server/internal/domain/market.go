/*
===========================================================================

market.go - the market escrow's records (opensro#3529, opensro-web#148)

Port-only, not native: v1.150 has no consignment. A listed item lives in
one market_listings row and nowhere else until it is sold, returned or
taken back; the store moves it in one transaction per door
(store/market.go). The in-game window and the website are two front ends
on the same rows.

The owner's rules (2026-11-19): a listing names its seller unless the
seller lists anonymously; the buyer picks the bag or the warehouse; a
gold sale pays the character that listed it (MarketProceeds), a Silk
sale the account; buyers may offer below the price (MarketOffer).

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

// Where a bought or returned item goes. The bag is the acting
// character's (the game window); the website only has the warehouse.
const (
	MarketToWarehouse = "warehouse"
	MarketToBag       = "bag"
)

// An offer's states. Open waits for the seller; the rest are final:
// accepted made the sale, rejected and withdrawn are the two parties'
// answers, and closed is an offer the listing outlived (sold, cancelled,
// expired) or the buyer could no longer pay.
const (
	MarketOfferOpen      = "open"
	MarketOfferAccepted  = "accepted"
	MarketOfferRejected  = "rejected"
	MarketOfferWithdrawn = "withdrawn"
	MarketOfferClosed    = "closed"
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
	ErrMarketBagFull       = errors.New("market: the inventory is full")
	ErrMarketOfferPrice    = errors.New("market: an offer must be below the price")
	ErrMarketOfferOpen     = errors.New("market: an offer on this listing is already open")
	ErrMarketOfferCap      = errors.New("market: too many open offers")
	ErrMarketOfferGone     = errors.New("market: the offer is no longer open")
	ErrMarketBuyerFunds    = errors.New("market: the offering character can no longer pay")
	ErrMarketBuyerFull     = errors.New("market: the buyer has no room for the item")
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
	// OfferCap bounds one account's open offers (game/market OpenOfferCap).
	OfferCap int
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
	SellerChar int64 `json:"sellerChar"`
	// SellerName is the listing character's name when it was listed, ""
	// for a warehouse listing. Anonymous hides it from every viewer; the
	// row keeps it for the audit trail.
	SellerName string       `json:"sellerName"`
	Anonymous  bool         `json:"anonymous"`
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
	// SellerChar is owed Price - Fee gold while Unpaid: a gold sale of a
	// bag listing pays the listing character, on its own action lane
	// (MarketClaimProceeds), never by a write the lane does not own.
	SellerChar int64 `json:"sellerChar"`
	Unpaid     bool  `json:"unpaid"`
	// Codename names the item for the seller's notice.
	Codename string `json:"codename"`
	// To is where the item went (MarketToBag or MarketToWarehouse) and
	// Slot the slot it took there.
	To   string `json:"to"`
	Slot int64  `json:"slot"`
}

/*
================
MarketOffer

A buyer's offer below a listing's price. Nothing is held: the buyer must
be able to pay when it is made and again when the seller accepts, and the
sale then runs at the offer's price as a buy would.
================
*/
type MarketOffer struct {
	ID           int64  `json:"id"`
	ListingID    int64  `json:"listingId"`
	Division     string `json:"division"`
	BuyerAccount string `json:"buyerAccount"`
	BuyerChar    int64  `json:"buyerChar"`
	BuyerName    string `json:"buyerName"`
	Price        int64  `json:"price"`
	// To is the buyer's pick for the item; a full pick falls back to the
	// other place at accept, and both full refuses (ErrMarketBuyerFull).
	To    string `json:"to"`
	State string `json:"state"`
	AtMs  int64  `json:"atMs"`
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
	Anonymous bool
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

To picks the destination. A bag buy names Receiver (the character whose
bag it is, the payer's own on a gold buy) and BagSlot, a free bag slot
the action lane chose, since only the lane knows the bag's size.
================
*/
type MarketBuyRequest struct {
	ListingID    int64
	Version      int64
	BuyerAccount string
	Payer        *Character
	To           string
	Receiver     *Character
	BagSlot      int64
	Rules        MarketRules
}

/*
================
MarketReclaimRequest

The game window's cancel or take-back: To is the seller's pick, and a bag
reclaim names Receiver (one of the account's characters) and BagSlot.
================
*/
type MarketReclaimRequest struct {
	Account   string
	ListingID int64
	To        string
	Receiver  *Character
	BagSlot   int64
}

/*
================
MarketOfferRequest

A new offer, made by the acting character (Buyer): it is the gold payer
of a gold listing and the receiver of a bag offer.
================
*/
type MarketOfferRequest struct {
	ListingID    int64
	BuyerAccount string
	Buyer        *Character
	Price        int64
	To           string
	Rules        MarketRules
}

/*
================
MarketAcceptRequest

The seller accepts an offer. Buyer is the offering character's record,
resolved by the caller with the same ownership rule as a buy's payer
(the lane's live record, or an offline one under the entry gate).
BagSlot is a free slot of its bag, or -1 when it has none.
================
*/
type MarketAcceptRequest struct {
	OfferID       int64
	SellerAccount string
	Buyer         *Character
	BagSlot       int64
	Rules         MarketRules
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
