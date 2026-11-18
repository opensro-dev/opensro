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
  - A gold buy pays from the character's own gold; the lane then sends
    the new balance, and a listing sends the native drop result that
    empties the bag slot (the PK death's silent removal).

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

// marketAnswerVersion is the only answer layout today.
const marketAnswerVersion = 1

// The ops a request names.
const (
	marketOpSearch   = "search"
	marketOpMine     = "mine"
	marketOpList     = "list"
	marketOpBuy      = "buy"
	marketOpCancel   = "cancel"
	marketOpTakeBack = "takeback"
)

// errMarketPreview is an item the window cannot preview (an invalid
// body); the answer refuses rather than send a row it cannot draw.
var errMarketPreview = errors.New("market: item preview failed")

// The lane's own refusal codes, beside the store's (publicstats
// MarketErrorCode): a market that is off, a malformed request, a bag
// held by a stall or an exchange, and a slot that is no bag slot.
const (
	marketRefusedUnavailable = "unavailable"
	marketRefusedRequest     = "request"
	marketRefusedBusy        = "busy"
	marketRefusedSlot        = "no-item"
)

/*
================
MarketDesk

The market the lane serves (game/market.Service). Account names the
session character's own account; GameList and GameBuy act for the
session character itself, under the division lock.
================
*/
type MarketDesk interface {
	Categories() []publicstats.MarketCategory
	Search(q publicstats.MarketSearch) (publicstats.MarketListingsResponse, error)
	Account(account string) (publicstats.OwnMarketResponse, error)
	GameList(c *domain.Character, slot int64, currency string, price int64) (publicstats.MarketListingView, error)
	GameBuy(c *domain.Character, listing, version int64) (publicstats.MarketSaleView, error)
	Cancel(account string, listing int64) error
	TakeBack(account string, listing int64) error
}

/*
================
marketRequest

One window request. Which fields count depends on Op: a search reads
the filters and the page, a listing the slot, currency and price, a buy
the listing and the version it saw, a cancel or take-back the listing.
================
*/
type marketRequest struct {
	Op       string `json:"op"`
	Q        string `json:"q,omitempty"`
	Category string `json:"category,omitempty"`
	Degree   int    `json:"degree,omitempty"`
	Currency string `json:"currency,omitempty"`
	Sort     string `json:"sort,omitempty"`
	Page     int    `json:"page,omitempty"`
	Slot     int64  `json:"slot,omitempty"`
	Price    int64  `json:"price,omitempty"`
	Listing  int64  `json:"listing,omitempty"`
	Version  int64  `json:"version,omitempty"`
}

/*
================
marketRow

One listing as the window shows it: the listing's own numbers, the
item's category and degree for the filters, and the item itself as a
commerce preview (the shop's body), so the window draws the native icon
and tooltip. The seller is never named.
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
	Category  string           `json:"category"`
	Degree    int              `json:"degree"`
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

The account's own side: its listings (open, sold, returned or held),
its purchases, the Silk it can pay with, the warehouse gold its sales
are paid into, and the rules.
================
*/
type marketMine struct {
	Listings      []marketRow                  `json:"listings"`
	Purchases     []publicstats.MarketSaleView `json:"purchases"`
	Silk          uint32                       `json:"silk"`
	WarehouseGold int64                        `json:"warehouseGold"`
	Rules         publicstats.MarketRulesView  `json:"rules"`
}

/*
================
marketAnswer

The lane's one answer. OK false carries the refusal's code; a search
answers Search and the categories, "mine" the account's side, a listing
the new listing, a buy the sale.
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
}

/*
================
marketRows

The listings as window rows, with the reference delta their items need
(published before the answer, as a shop's delivery publishes its own).
A preview's slot is the row's index; it names no bag slot.
================
*/
func (rt *Runtime) marketRows(views []publicstats.MarketListingView) ([]marketRow, wire.Frame, error) {
	rows := make([]enterworld.InventoryRow, len(views))
	for i, view := range views {
		rows[i] = view.Row
		rows[i].Slot = int64(i)
	}
	items := invItemsFromRowsWithin(rows, int64(len(rows)))
	previews, err := rt.shopInventoryRows(items, nil)
	if err != nil || len(previews) != len(views) {
		return nil, wire.Frame{}, errMarketPreview
	}
	out := make([]marketRow, len(views))
	for i, view := range views {
		out[i] = marketRow{ID: view.ID, Version: view.Version, Currency: view.Currency, Price: view.Price,
			ListedMs: view.ListedMs, ExpiresMs: view.ExpiresMs, State: view.State, Category: view.Item.Category,
			Degree: view.Item.Degree, Item: previews[i]}
	}
	return out, rt.commerceReferences(items, nil), nil
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
		payload = []byte(`{"version":1,"ok":false,"error":"unavailable"}`)
	}
	return wire.Frame{Opcode: opMarket, Payload: payload}
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

Strict: unknown fields, trailing data and an unknown op all refuse.
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
	case marketOpSearch, marketOpMine, marketOpList, marketOpBuy, marketOpCancel, marketOpTakeBack:
		return request, true
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
		Currency: request.Currency, Sort: request.Sort, Page: request.Page}
	if q.Page == 0 {
		q.Page = 1
	}
	if q.Page < 1 || q.Page > publicstats.MarketMaxPage || q.Degree < 0 || q.Degree > publicstats.MarketMaxDegree ||
		len(q.Q) > maxMarketText {
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
Reads (search, mine) need only the session; the writes hold the
division lock for the whole door, so the record the store changes is
the one the action lane owns.
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
		account, err := rt.Market.Account(c.AccountID)
		if err != nil {
			return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
		}
		rows, references, err := rt.marketRows(account.Listings)
		if err != nil {
			return marketRefusal(request.Op, marketRefusedUnavailable)
		}
		mine := marketMine{Listings: rows, Purchases: account.Purchases, Silk: account.Silk,
			WarehouseGold: account.WarehouseGold, Rules: account.Rules}
		return OpResult{Frames: []wire.Frame{references, marketFrame(marketAnswer{Op: request.Op, OK: true, Mine: &mine})}}
	case marketOpList:
		return rt.marketList(division, c, request)
	case marketOpBuy:
		return rt.marketBuy(division, c, request)
	}
	var err error
	if request.Op == marketOpCancel {
		err = rt.Market.Cancel(c.AccountID, request.Listing)
	} else {
		err = rt.Market.TakeBack(c.AccountID, request.Listing)
	}
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	return OpResult{Frames: []wire.Frame{marketFrame(marketAnswer{Op: request.Op, OK: true})}}
}

/*
================
marketBagBusy

A stall's owner keeps its bag as the stall shows it, and an open
exchange locks the bag (itemmove.go); neither may list or pay.
================
*/
func (rt *Runtime) marketBagBusy(division string, c *enterworld.Character) bool {
	return rt.Stalls.Keeping(division, c.Name) || rt.Exchanges.Trading(division, c.Name)
}

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
	listing, err := rt.Market.GameList(c, request.Slot, request.Currency, request.Price)
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

A buy for the session character. A gold buy changed its balance, so
the new balance follows the answer; the item is in the warehouse.
================
*/
func (rt *Runtime) marketBuy(division string, c *enterworld.Character, request marketRequest) OpResult {
	if rt.marketBagBusy(division, c) {
		return marketRefusal(request.Op, marketRefusedBusy)
	}
	sale, err := rt.Market.GameBuy(c, request.Listing, request.Version)
	if err != nil {
		return marketRefusal(request.Op, publicstats.MarketErrorCode(err))
	}
	frames := []wire.Frame{marketFrame(marketAnswer{Op: request.Op, OK: true, Sale: &sale})}
	if sale.Currency == domain.MarketGold {
		frames = append(frames, goldFrame(c))
	}
	return OpResult{Frames: frames}
}
