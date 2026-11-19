/*
===========================================================================

market.go - the consignment market's in-game lane (opensro#3529)

Port-only, not native (SRO_CONSIGNMENT, off by default): v1.150 has no
consignment. The browser's market window rides one control frame
(opMarket) both ways, JSON like the Item Mall's silk history: a request
names an op, the answer names the same op and carries what the window
shows. The escrow, its rules and its refusals are game/market's and the
store's (data/store/market.go); this lane only adds what an online
character changes:

  - A listing comes from the bag, never an equipment socket, and never
    while a stall or an exchange holds the bag (the item move's rule).
  - A buy, an accepted offer and a reclaim deliver to the bag slot this
    lane picks (the first free one) when the player picks the bag, with
    the native pickup result; a listing empties its slot with the native
    drop result (the PK death's silent removal). Gold moves send the new
    balance.
  - Gold a sale owes the listing character is paid on its own lane: at
    once when it is in the world here, else by the proceeds hook once it
    enters (market_proceeds.go), with a notice either way.
  - The other party of a sale or an offer, when in the world, gets its
    frames as Recipients: an accepted offer's delivery, an offer to
    answer, a refusal of its offer, a Silk sale's notice.

The character is always the session's own (hubHandler). No account,
character or seller ever rides in from the payload.

===========================================================================
*/
package action

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/transport"
)

// opMarket is the market control frame (client commerce-controls.ts
// MARKET_CONTROL). The control numbers below 256 are allocated one per
// lane: 19 is the crest upload's (crestregister.go), so the market
// takes 20.
const opMarket = 20

// maxMarketRequest bounds a request: the longest is a search, whose
// text is itself bounded (publicstats.maxMarketQuery).
const maxMarketRequest = 512

// maxMarketText bounds a search's text (publicstats maxMarketQuery,
// which the site's parser keeps to).
const maxMarketText = 64

// marketAnswerVersion is the answer layout: 2 added sellers, offers,
// destinations and events.
const marketAnswerVersion = 2

// The ops a request names; marketOpEvent only ever rides an answer the
// server pushes (a sale's notice, an offer to answer).
const (
	marketOpSearch   = "search"
	marketOpMine     = "mine"
	marketOpList     = "list"
	marketOpBuy      = "buy"
	marketOpCancel   = "cancel"
	marketOpTakeBack = "takeback"
	marketOpOffer    = "offer"
	marketOpAccept   = "accept"
	marketOpReject   = "reject"
	marketOpWithdraw = "withdraw"
	marketOpEvent    = "event"
)

// The kinds of a pushed event.
const (
	marketEventSold          = "sold"
	marketEventOffer         = "offer"
	marketEventOfferAccepted = "offer-accepted"
	marketEventOfferRejected = "offer-rejected"
	marketEventOfferWithdrew = "offer-withdrawn"
)

// errMarketPreview is an item the window cannot preview (an invalid
// body); the answer refuses rather than send a row it cannot draw.
var errMarketPreview = errors.New("market: item preview failed")

// The lane's own refusal codes, beside the store's (publicstats
// MarketErrorCode): a market that is off, a malformed request, a bag
// held by a stall or an exchange (the actor's, or an accepted offer's
// buyer's), and a slot that is no bag slot.
const (
	marketRefusedUnavailable = "unavailable"
	marketRefusedRequest     = "request"
	marketRefusedBusy        = "busy"
	marketRefusedBuyerBusy   = "buyer-busy"
	marketRefusedSlot        = "no-item"
)

// marketProceedsIntervalMs paces the proceeds hook: an online seller is
// paid within two seconds of a website sale or of its entry.
const marketProceedsIntervalMs = 2000

/*
================
MarketDesk

The market the lane serves (game/market.Service). Account names the
session character's own account; the Game doors act for the characters
they are given, under the division lock.
================
*/
type MarketDesk interface {
	Categories() []publicstats.MarketCategory
	Search(q publicstats.MarketSearch) (publicstats.MarketListingsResponse, error)
	GameAccount(c *domain.Character) (publicstats.OwnMarketResponse, error)
	GameList(c *domain.Character, slot int64, currency string, price int64, anonymous bool) (publicstats.MarketListingView, error)
	GameBuy(c *domain.Character, listing, version int64, to string, bagSlot int64) (publicstats.MarketSaleView, error)
	GameReclaim(c *domain.Character, listing int64, to string, bagSlot int64) error
	GameOffer(c *domain.Character, listing, price int64, to string) (publicstats.MarketOfferView, error)
	OfferBuyer(offer int64) (domain.MarketOffer, error)
	GameAccept(c *domain.Character, offer int64, buyer *domain.Character, bagSlot int64) (publicstats.MarketSaleView, error)
	GameReject(c *domain.Character, offer int64) (publicstats.MarketOfferView, error)
	GameWithdraw(c *domain.Character, offer int64) (publicstats.MarketOfferView, error)
	Owed() (map[int64]bool, error)
	ClaimProceeds(c *domain.Character) ([]publicstats.MarketSaleView, []string, error)
}

/*
================
marketRequest

One window request. Which fields count depends on Op: a search reads
the filters and the page; a listing the slot, currency, price and the
anonymous choice; a buy the listing, the version it saw and where to;
a cancel or take-back the listing and where to; an offer the listing,
price and where to; an accept, reject or withdraw the offer.
================
*/
type marketRequest struct {
	Op        string `json:"op"`
	Q         string `json:"q,omitempty"`
	Category  string `json:"category,omitempty"`
	Degree    int    `json:"degree,omitempty"`
	Race      string `json:"race,omitempty"`
	Rarity    string `json:"rarity,omitempty"`
	Currency  string `json:"currency,omitempty"`
	Sort      string `json:"sort,omitempty"`
	Page      int    `json:"page,omitempty"`
	Slot      int64  `json:"slot,omitempty"`
	Price     int64  `json:"price,omitempty"`
	Listing   int64  `json:"listing,omitempty"`
	Version   int64  `json:"version,omitempty"`
	To        string `json:"to,omitempty"`
	Anonymous bool   `json:"anonymous,omitempty"`
	Offer     int64  `json:"offer,omitempty"`
}

/*
================
marketRow

One listing as the window shows it: the listing's own numbers, the
seller's name (none when anonymous; the seller's own rows carry the
choice), the item's category and degree for the filters, and the item
itself as a commerce preview, so the window draws the native icon and
tooltip.
================
*/
type marketRow struct {
	ID        int64            `json:"id"`
	Version   int64            `json:"version"`
	Currency  string           `json:"currency"`
	Price     int64            `json:"price"`
	ListedMs  int64            `json:"listedMs"`
	ExpiresMs int64            `json:"expiresMs"`
	State     string           `json:"state"`
	Seller    string           `json:"seller,omitempty"`
	Anonymous bool             `json:"anonymous,omitempty"`
	Category  string           `json:"category"`
	Degree    int              `json:"degree"`
	Sealed    bool             `json:"sealed,omitempty"`
	Item      shopInventoryRow `json:"item"`
}

/*
================
marketOfferRow
================
*/
type marketOfferRow struct {
	ID        int64            `json:"id"`
	ListingID int64            `json:"listingId"`
	Currency  string           `json:"currency"`
	Asked     int64            `json:"asked"`
	Price     int64            `json:"price"`
	Buyer     string           `json:"buyer"`
	To        string           `json:"to"`
	State     string           `json:"state"`
	AtMs      int64            `json:"atMs"`
	Item      shopInventoryRow `json:"item"`
}

/*
================
marketPage
================
*/
type marketPage struct {
	Rows  []marketRow `json:"rows"`
	Page  int         `json:"page"`
	Pages int         `json:"pages"`
	Total int         `json:"total"`
}

/*
================
marketMine

The account's own side: its listings (open, sold, returned or held), the
offers on them and its own offers, its purchases, the Silk it can pay
with, the warehouse gold, and the rules.
================
*/
type marketMine struct {
	Listings      []marketRow                  `json:"listings"`
	OffersIn      []marketOfferRow             `json:"offersIn"`
	OffersOut     []marketOfferRow             `json:"offersOut"`
	Purchases     []publicstats.MarketSaleView `json:"purchases"`
	Silk          uint32                       `json:"silk"`
	WarehouseGold int64                        `json:"warehouseGold"`
	Rules         publicstats.MarketRulesView  `json:"rules"`
}

/*
================
marketEvent

A notice pushed to the party a change concerns: Item names the item,
Who the other party (the buyer of an offer), Paid the gold that reached
the character.
================
*/
type marketEvent struct {
	Kind     string `json:"kind"`
	Item     string `json:"item"`
	Currency string `json:"currency"`
	Price    int64  `json:"price"`
	Paid     int64  `json:"paid,omitempty"`
	Who      string `json:"who,omitempty"`
}

/*
================
marketAnswer

The lane's one answer. OK false carries the refusal's code; a search
answers Search and the categories, "mine" the account's side, a listing
the new listing, a buy or accept the sale, an offer the offer, and an
event its notices.
================
*/
type marketAnswer struct {
	Version    int                          `json:"version"`
	Op         string                       `json:"op"`
	OK         bool                         `json:"ok"`
	Error      string                       `json:"error,omitempty"`
	Categories []publicstats.MarketCategory `json:"categories,omitempty"`
	Search     *marketPage                  `json:"search,omitempty"`
	Mine       *marketMine                  `json:"mine,omitempty"`
	Listing    *marketRow                   `json:"listing,omitempty"`
	Sale       *publicstats.MarketSaleView  `json:"sale,omitempty"`
	Offer      *marketOfferRow              `json:"offer,omitempty"`
	Events     []marketEvent                `json:"events,omitempty"`
}

/*
================
marketPreviews

The items as commerce previews, with the reference delta they need
(published before the answer, as a shop's delivery publishes its own).
A preview's slot is its index; it names no bag slot.
================
*/
func (rt *Runtime) marketPreviews(items []domain.InventoryRow) ([]shopInventoryRow, wire.Frame, error) {
	rows := make([]enterworld.InventoryRow, len(items))
	for i, item := range items {
		rows[i] = item
		rows[i].Slot = int64(i)
	}
	inv := invItemsFromRowsWithin(rows, int64(len(rows)))
	previews, err := rt.shopInventoryRows(inv, nil)
	if err != nil || len(previews) != len(items) {
		return nil, wire.Frame{}, errMarketPreview
	}
	return previews, rt.commerceReferences(inv, nil), nil
}

/*
================
marketListingRow
================
*/
func marketListingRow(view publicstats.MarketListingView, preview shopInventoryRow) marketRow {
	return marketRow{ID: view.ID, Version: view.Version, Currency: view.Currency, Price: view.Price,
		ListedMs: view.ListedMs, ExpiresMs: view.ExpiresMs, State: view.State, Seller: view.Seller,
		Anonymous: view.Anonymous, Category: view.Item.Category, Degree: view.Item.Degree,
		Sealed: view.Item.Sealed, Item: preview}
}

/*
================
marketOfferRowOf
================
*/
func marketOfferRowOf(view publicstats.MarketOfferView, preview shopInventoryRow) marketOfferRow {
	return marketOfferRow{ID: view.ID, ListingID: view.ListingID, Currency: view.Currency, Asked: view.Asked,
		Price: view.Price, Buyer: view.Buyer, To: view.To, State: view.State, AtMs: view.AtMs, Item: preview}
}

/*
================
marketRows

The listings as window rows, with their reference frame.
================
*/
func (rt *Runtime) marketRows(views []publicstats.MarketListingView) ([]marketRow, wire.Frame, error) {
	items := make([]domain.InventoryRow, len(views))
	for i, view := range views {
		items[i] = view.Row
	}
	previews, references, err := rt.marketPreviews(items)
	if err != nil {
		return nil, wire.Frame{}, err
	}
	out := make([]marketRow, len(views))
	for i, view := range views {
		out[i] = marketListingRow(view, previews[i])
	}
	return out, references, nil
}

/*
================
marketMineOf

The account's side as the window shows it, every item previewed in one
reference frame.
================
*/
func (rt *Runtime) marketMineOf(account publicstats.OwnMarketResponse) (marketMine, wire.Frame, error) {
	items := make([]domain.InventoryRow, 0, len(account.Listings)+len(account.OffersIn)+len(account.OffersOut))
	for _, view := range account.Listings {
		items = append(items, view.Row)
	}
	for _, view := range account.OffersIn {
		items = append(items, view.Row)
	}
	for _, view := range account.OffersOut {
		items = append(items, view.Row)
	}
	previews, references, err := rt.marketPreviews(items)
	if err != nil {
		return marketMine{}, wire.Frame{}, err
	}
	mine := marketMine{Listings: []marketRow{}, OffersIn: []marketOfferRow{}, OffersOut: []marketOfferRow{},
		Purchases: account.Purchases, Silk: account.Silk, WarehouseGold: account.WarehouseGold, Rules: account.Rules}
	next := 0
	for _, view := range account.Listings {
		mine.Listings = append(mine.Listings, marketListingRow(view, previews[next]))
		next++
	}
	for _, view := range account.OffersIn {
		mine.OffersIn = append(mine.OffersIn, marketOfferRowOf(view, previews[next]))
		next++
	}
	for _, view := range account.OffersOut {
		mine.OffersOut = append(mine.OffersOut, marketOfferRowOf(view, previews[next]))
		next++
	}
	if mine.Purchases == nil {
		mine.Purchases = []publicstats.MarketSaleView{}
	}
	return mine, references, nil
}

/*
================
registerMarket
================
*/
func (rt *Runtime) registerMarket(hub *transport.Hub) {
	hub.Handle(opMarket, rt.hubHandler(hub, rt.HandleMarket))
}

/*
================
marketFrame
================
*/
func marketFrame(answer marketAnswer) wire.Frame {
	answer.Version = marketAnswerVersion
	payload, err := json.Marshal(answer)
	if err != nil {
		payload = []byte(`{"version":2,"ok":false,"error":"unavailable"}`)
	}
	return wire.Frame{Opcode: opMarket, Payload: payload}
}

/*
================
marketEventFrame
================
*/
func marketEventFrame(events ...marketEvent) wire.Frame {
	return marketFrame(marketAnswer{Op: marketOpEvent, OK: true, Events: events})
}

/*
================
marketRefusal
================
*/
func marketRefusal(op, code string) OpResult {
	return OpResult{Frames: []wire.Frame{marketFrame(marketAnswer{Op: op, Error: code})}}
}

/*
================
decodeMarketRequest

Strict: unknown fields, trailing data, an unknown op and an unknown
destination all refuse.
================
*/
func decodeMarketRequest(payload []byte) (marketRequest, bool) {
	var request marketRequest
	if len(payload) == 0 || len(payload) > maxMarketRequest {
		return request, false
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil || decoder.More() {
		return request, false
	}
	switch request.Op {
	case marketOpSearch, marketOpMine, marketOpList, marketOpAccept, marketOpReject, marketOpWithdraw:
		return request, true
	case marketOpBuy, marketOpOffer, marketOpCancel, marketOpTakeBack:
		return request, request.To == domain.MarketToBag || request.To == domain.MarketToWarehouse
	}
	return request, false
}

/*
================
marketSearch

The window's filters, held to the site's bounds (publicstats market.go):
a page from 1 to MarketMaxPage, a degree up to MarketMaxDegree, a known
category, currency and order, and a bounded text.
================
*/
func (rt *Runtime) marketSearch(request marketRequest) (publicstats.MarketSearch, bool) {
	q := publicstats.MarketSearch{Q: strings.TrimSpace(request.Q), Category: request.Category, Degree: request.Degree,
		Race: request.Race, Rarity: request.Rarity, Currency: request.Currency, Sort: request.Sort, Page: request.Page}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.Page < 1 || q.Page > publicstats.MarketMaxPage || q.Degree < 0 || q.Degree > publicstats.MarketMaxDegree ||
		len(q.Q) > maxMarketText || !publicstats.MarketFilterValid(q.Race, q.Rarity) {
		return q, false
	}
	switch q.Currency {
	case "", domain.MarketGold, domain.MarketSilk:
	default:
		return q, false
	}
	switch q.Sort {
	case "", publicstats.MarketSortNewest, publicstats.MarketSortEnding, publicstats.MarketSortPriceAsc,
		publicstats.MarketSortPriceDesc:
	default:
		return q, false
	}
	if q.Category != "" {
		known := false
		for _, category := range rt.Market.Categories() {
			known = known || category.Slug == q.Category
		}
		if !known {
			return q, false
		}
	}
	return q, true
}

/*
================
HandleMarket

The session character's market request, answered on the market lane.
Every op holds the division lock for the whole door, so each record the
store changes (the actor's, an offer's buyer's, a paid seller's) is one
this lane owns.
================
*/
func (rt *Runtime) HandleMarket(division string, character *enterworld.Character, payload []byte) OpResult {
	request, ok := decodeMarketRequest(payload)
	if !ok {
		return marketRefusal(request.Op, marketRefusedRequest)
	}
	if character == nil || rt.Market == nil {
		return marketRefusal(request.Op, marketRefusedUnavailable)
	}
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, character.Name)
	if c == nil || c.DeletePending || c.AccountID == "" {
		return marketRefusal(request.Op, marketRefusedUnavailable)
	}
	// No NPC is needed: the window opens from the HUD anywhere in the world
	// (the owner's rule, 2026-11-19), as the website serves the same market.
	switch request.Op {
	case marketOpSearch:
		q, ok := rt.marketSearch(request)
		if !ok {
			return marketRefusal(request.Op, marketRefusedRequest)
		}
		page, err := rt.Market.Search(q)
		if err != nil {
			return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
		}
		rows, references, err := rt.marketRows(page.Listings)
		if err != nil {
			return marketRefusal(request.Op, marketRefusedUnavailable)
		}
		return OpResult{Frames: []wire.Frame{references, marketFrame(marketAnswer{Op: request.Op, OK: true,
			Categories: rt.Market.Categories(),
			Search:     &marketPage{Rows: rows, Page: page.Page, Pages: page.Pages, Total: page.Total}})}}
	case marketOpMine:
		account, err := rt.Market.GameAccount(c)
		if err != nil {
			return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
		}
		mine, references, err := rt.marketMineOf(account)
		if err != nil {
			return marketRefusal(request.Op, marketRefusedUnavailable)
		}
		return OpResult{Frames: []wire.Frame{references, marketFrame(marketAnswer{Op: request.Op, OK: true, Mine: &mine})}}
	case marketOpList:
		return rt.marketList(division, c, request)
	case marketOpBuy:
		return rt.marketBuy(division, c, request)
	case marketOpCancel, marketOpTakeBack:
		return rt.marketReclaim(division, c, request)
	case marketOpOffer:
		return rt.marketOffer(division, c, request)
	case marketOpAccept:
		return rt.marketAccept(division, c, request)
	}
	return rt.marketAnswerOffer(division, c, request)
}

//============================================================================

/*
================
marketBagBusy

A stall's owner keeps its bag as the stall shows it, and an open
exchange locks the bag and the gold (itemmove.go); neither may list,
pay or receive.
================
*/
func (rt *Runtime) marketBagBusy(division string, c *enterworld.Character) bool {
	return rt.Stalls.Keeping(division, c.Name) || rt.Exchanges.Trading(division, c.Name)
}

/*
================
marketAdmitted

Whether c holds a world admission in the division: then its session
gets its frames. Under the division lock either way, its record is this
lane's to write (WhileOffline's gate).
================
*/
func (rt *Runtime) marketAdmitted(division string, c *enterworld.Character) bool {
	if c == nil {
		return false
	}
	_, admitted := rt.characterAdmissions.Load(simulation.WorldKey(division, c.Name))
	return admitted
}

/*
================
marketBagSlot

The first free bag slot of c, or -1 when the bag is full.
================
*/
func marketBagSlot(c *enterworld.Character) int64 {
	if c == nil {
		return -1
	}
	slot, free := inventory.New(invItemsFromBag(c), inventory.BagEnd(c)).FirstFreeBagSlot()
	if !free {
		return -1
	}
	return int64(slot)
}

/*
================
MarketFreeBagSlot

marketBagSlot for the website's accept (game/market Service.FreeBagSlot),
which runs under WhileOffline.
================
*/
func (rt *Runtime) MarketFreeBagSlot(c *domain.Character) int64 {
	return marketBagSlot(c)
}

/*
================
marketDelivered

The frames that show an item the store just put in c's bag at slot: the
native pickup result, then the quest counters.
================
*/
func (rt *Runtime) marketDelivered(c *enterworld.Character, slot int64) []wire.Frame {
	for _, item := range invItemsFromBag(c) {
		if int64(item.Slot) == slot {
			frames := []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.EncodePickupItemResult(item.Slot, item.Body())}}
			return append(frames, rt.updateQuestInventory(c)...)
		}
	}
	return nil
}

/*
================
marketPaySeller

After a sale: the listing character, when in the world here, is paid
what the sale owes it at once, with its notice; a Silk sale only tells
it. A seller elsewhere is paid by the proceeds hook on entry. The frames
are the actor's own when the actor is the seller.
================
*/
func (rt *Runtime) marketPaySeller(division string, actor *enterworld.Character, sale publicstats.MarketSaleView, item string) (own []wire.Frame, others []RecipientFrames) {
	seller := rt.findCharacterByID(division, sale.SellerChar)
	if seller == nil {
		return nil, nil
	}
	if sale.Currency == domain.MarketSilk {
		frames := []wire.Frame{marketEventFrame(marketEvent{Kind: marketEventSold, Item: item, Currency: sale.Currency,
			Price: sale.Price, Paid: sale.Price - sale.Fee})}
		if seller == actor {
			return frames, nil
		}
		return nil, rt.marketTell(division, actor, seller.ID, frames...)
	}
	if !rt.marketAdmitted(division, seller) {
		return nil, nil
	}
	frames := rt.marketClaim(seller)
	if seller == actor {
		return frames, nil
	}
	if len(frames) == 0 {
		return nil, nil
	}
	return nil, []RecipientFrames{{CharacterID: seller.ID, Frames: frames}}
}

/*
================
marketTell

The frames for the other party of a change, by character id: none when
it is the actor or not in the world here.
================
*/
func (rt *Runtime) marketTell(division string, actor *enterworld.Character, id int64, frames ...wire.Frame) []RecipientFrames {
	party := rt.findCharacterByID(division, id)
	if party == nil || party == actor || !rt.marketAdmitted(division, party) {
		return nil
	}
	return []RecipientFrames{{CharacterID: party.ID, Frames: frames}}
}

/*
================
marketClaim

Pays c every sale that owes it and returns its gold and notice frames;
the caller holds the division lock.
================
*/
func (rt *Runtime) marketClaim(c *enterworld.Character) []wire.Frame {
	sales, names, err := rt.Market.ClaimProceeds(c)
	if err != nil || len(sales) == 0 {
		return nil
	}
	events := make([]marketEvent, len(sales))
	for i, sale := range sales {
		events[i] = marketEvent{Kind: marketEventSold, Item: names[i], Currency: sale.Currency, Price: sale.Price,
			Paid: sale.Price - sale.Fee}
	}
	return []wire.Frame{goldFrame(c), marketEventFrame(events...)}
}

/*
================
MarketProceedsTick

The mission-clock hook: every marketProceedsIntervalMs, pays each
character in the world here that a sale owes gold (a website sale, or
one made while it was away). Nothing owed costs one indexed query, and
the session list is read only when something is owed.
================
*/
func (rt *Runtime) MarketProceedsTick(division string, sessions func() []simulation.SessionView, nowMs int64) []simulation.DivisionFrames {
	if rt.Market == nil {
		return nil
	}
	if last := rt.marketProceedsAt.Load(); last != 0 && nowMs-last < marketProceedsIntervalMs {
		return nil
	}
	rt.marketProceedsAt.Store(nowMs)
	owed, err := rt.Market.Owed()
	if err != nil || len(owed) == 0 {
		return nil
	}
	var recipients []RecipientFrames
	for _, view := range sessions() {
		if view.DivisionID != division || !owed[view.CharacterID] {
			continue
		}
		unlock := rt.lockDivision(division)
		c := rt.findCharacterByID(division, view.CharacterID)
		if c != nil && rt.marketAdmitted(division, c) {
			if frames := rt.marketClaim(c); len(frames) != 0 {
				recipients = append(recipients, RecipientFrames{CharacterID: c.ID, Frames: frames})
			}
		}
		unlock()
	}
	return recipientDivisionFrames(division, recipients)
}

//============================================================================

/*
================
marketList

A listing from a bag slot. On success the slot empties on the client
through the native drop result (the store already took the row), and
the quest counters follow the bag.
================
*/
func (rt *Runtime) marketList(division string, c *enterworld.Character, request marketRequest) OpResult {
	if rt.marketBagBusy(division, c) {
		return marketRefusal(request.Op, marketRefusedBusy)
	}
	if request.Slot < int64(inventory.EquipmentSlotEnd) || request.Slot >= int64(inventory.BagEnd(c)) {
		return marketRefusal(request.Op, marketRefusedSlot)
	}
	// Any item operation supersedes a pickup approach in flight.
	rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	listing, err := rt.Market.GameList(c, request.Slot, request.Currency, request.Price, request.Anonymous)
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	// The listing has committed: a preview that fails now still empties
	// the slot, and the answer carries no row rather than refuse.
	answer := marketAnswer{Op: request.Op, OK: true}
	frames := []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: wire.EncodeGroundDropResult(uint8(request.Slot))}}
	if rows, references, err := rt.marketRows([]publicstats.MarketListingView{listing}); err == nil {
		answer.Listing = &rows[0]
		frames = append(frames, references)
	}
	frames = append(frames, marketFrame(answer))
	frames = append(frames, rt.updateQuestInventory(c)...)
	return OpResult{Frames: frames}
}

/*
================
marketBuy

A buy for the session character, into the bag or the warehouse it
picked. The answer comes first, then the bag's pickup result, the new
gold balance, and the seller's payment when it is here.
================
*/
func (rt *Runtime) marketBuy(division string, c *enterworld.Character, request marketRequest) OpResult {
	if rt.marketBagBusy(division, c) {
		return marketRefusal(request.Op, marketRefusedBusy)
	}
	bagSlot := int64(-1)
	if request.To == domain.MarketToBag {
		if bagSlot = marketBagSlot(c); bagSlot < 0 {
			return marketRefusal(request.Op, publicstats.MarketErrorCode(domain.ErrMarketBagFull))
		}
		rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	}
	sale, err := rt.Market.GameBuy(c, request.Listing, request.Version, request.To, bagSlot)
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	frames := []wire.Frame{marketFrame(marketAnswer{Op: request.Op, OK: true, Sale: &sale})}
	if sale.To == domain.MarketToBag {
		frames = append(frames, rt.marketDelivered(c, sale.Slot)...)
	}
	if sale.Currency == domain.MarketGold {
		frames = append(frames, goldFrame(c))
	}
	own, others := rt.marketPaySeller(division, c, sale, sale.Item)
	return OpResult{Frames: append(frames, own...), Recipients: others}
}

/*
================
marketReclaim

The seller's cancel or take-back, into the bag or the warehouse it
picked; a held item whose warehouse is full can always come back to a
free bag slot.
================
*/
func (rt *Runtime) marketReclaim(division string, c *enterworld.Character, request marketRequest) OpResult {
	bagSlot := int64(-1)
	if request.To == domain.MarketToBag {
		if rt.marketBagBusy(division, c) {
			return marketRefusal(request.Op, marketRefusedBusy)
		}
		if bagSlot = marketBagSlot(c); bagSlot < 0 {
			return marketRefusal(request.Op, publicstats.MarketErrorCode(domain.ErrMarketBagFull))
		}
		rt.Pending.Clear(grounditem.PendingKey(division, c.Name))
	}
	if err := rt.Market.GameReclaim(c, request.Listing, request.To, bagSlot); err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	frames := []wire.Frame{marketFrame(marketAnswer{Op: request.Op, OK: true})}
	if request.To == domain.MarketToBag {
		frames = append(frames, rt.marketDelivered(c, bagSlot)...)
	}
	return OpResult{Frames: frames}
}

/*
================
marketOffer

A new offer; the listing's seller, when here, is told at once.
================
*/
func (rt *Runtime) marketOffer(division string, c *enterworld.Character, request marketRequest) OpResult {
	offer, err := rt.Market.GameOffer(c, request.Listing, request.Price, request.To)
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	answer := marketAnswer{Op: request.Op, OK: true}
	frames := []wire.Frame{}
	if previews, references, err := rt.marketPreviews([]domain.InventoryRow{offer.Row}); err == nil {
		row := marketOfferRowOf(offer, previews[0])
		answer.Offer = &row
		frames = append(frames, references)
	}
	frames = append(frames, marketFrame(answer))
	others := rt.marketTell(division, c, offer.SellerChar, marketEventFrame(marketEvent{Kind: marketEventOffer,
		Item: offer.Item.Name, Currency: offer.Currency, Price: offer.Price, Who: offer.Buyer}))
	return OpResult{Frames: frames, Recipients: others}
}

/*
================
marketAccept

The seller accepts an offer. The offering character's record is this
lane's under the division lock whether it is in the world or not; when
it is, its bag or gold may not be held by a stall or an exchange, and
its session gets the delivery.
================
*/
func (rt *Runtime) marketAccept(division string, c *enterworld.Character, request marketRequest) OpResult {
	offer, err := rt.Market.OfferBuyer(request.Offer)
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	buyer := rt.findCharacterByID(division, offer.BuyerChar)
	admitted := rt.marketAdmitted(division, buyer)
	if admitted && rt.marketBagBusy(division, buyer) {
		return marketRefusal(request.Op, marketRefusedBuyerBusy)
	}
	bagSlot := marketBagSlot(buyer)
	if admitted && bagSlot >= 0 {
		rt.Pending.Clear(grounditem.PendingKey(division, buyer.Name))
	}
	sale, err := rt.Market.GameAccept(c, request.Offer, buyer, bagSlot)
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	frames := []wire.Frame{marketFrame(marketAnswer{Op: request.Op, OK: true, Sale: &sale})}
	var others []RecipientFrames
	if admitted {
		delivery := []wire.Frame{}
		if sale.To == domain.MarketToBag {
			delivery = append(delivery, rt.marketDelivered(buyer, sale.Slot)...)
		}
		if sale.Currency == domain.MarketGold {
			delivery = append(delivery, goldFrame(buyer))
		}
		delivery = append(delivery, marketEventFrame(marketEvent{Kind: marketEventOfferAccepted, Item: sale.Item,
			Currency: sale.Currency, Price: sale.Price}))
		others = append(others, RecipientFrames{CharacterID: buyer.ID, Frames: delivery})
	}
	own, paid := rt.marketPaySeller(division, c, sale, sale.Item)
	return OpResult{Frames: append(frames, own...), Recipients: append(others, paid...)}
}

/*
================
marketAnswerOffer

A seller's reject or a buyer's withdraw; the other party, when here, is
told.
================
*/
func (rt *Runtime) marketAnswerOffer(division string, c *enterworld.Character, request marketRequest) OpResult {
	var offer publicstats.MarketOfferView
	var err error
	kind := marketEventOfferRejected
	if request.Op == marketOpReject {
		offer, err = rt.Market.GameReject(c, request.Offer)
	} else {
		offer, err = rt.Market.GameWithdraw(c, request.Offer)
		kind = marketEventOfferWithdrew
	}
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	other := offer.BuyerChar
	if request.Op == marketOpWithdraw {
		other = offer.SellerChar
	}
	frames := []wire.Frame{marketFrame(marketAnswer{Op: request.Op, OK: true})}
	others := rt.marketTell(division, c, other, marketEventFrame(marketEvent{Kind: kind, Item: offer.Item.Name,
		Currency: offer.Currency, Price: offer.Price, Who: offer.Buyer}))
	return OpResult{Frames: frames, Recipients: others}
}
