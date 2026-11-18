/*
===========================================================================

market.go - the website market's routes (opensro-web#148, opensro#3529)

Port-only, not native (SRO_CONSIGNMENT, off by default). Served on the
privacy routes' listener with the same bearer token, and only when the
GameWorld sets Sources.Market (game/market.Service); off, every path
answers 404, which is native.

	GET  /v1/market/categories                    -> 200 the category picker
	GET  /v1/market/listings?q&category&degree&currency&sort&page -> 200
	GET  /v1/accounts/{id}/market                 -> 200 my listings, purchases,
	                                                 warehouse and characters
	POST /v1/accounts/{id}/market/list            {slot, currency, price} -> 201
	POST /v1/accounts/{id}/market/buy             {listing, version, character} -> 201
	POST /v1/accounts/{id}/market/{listing}/cancel     -> 204
	POST /v1/accounts/{id}/market/{listing}/take-back  -> 204
	POST /v1/accounts/{id}/market/offer           {listing, price, character} -> 201
	POST /v1/accounts/{id}/market/offers/{offer}/accept   -> 201 the sale
	POST /v1/accounts/{id}/market/offers/{offer}/reject   -> 204
	POST /v1/accounts/{id}/market/offers/{offer}/withdraw -> 204

A listing names its seller's character unless the seller chose to list
anonymously (owner, 2026-11-19); a warehouse listing has no character
and names none.

A refusal answers {"error": code} with one of the marketError codes, so
the site can say what happened in its own words. None charges anything.
A search with an unknown filter value is 400, never an empty page.

===========================================================================
*/
package publicstats

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"opensro.online/server/internal/domain"
)

// maxMarketBody bounds a market request body.
const maxMarketBody = 512

// The search's bounds (the site's parser keeps to them too).
const (
	MarketPageSize  = 15
	MarketMaxPage   = 100
	MarketMaxDegree = 11
	maxMarketQuery  = 64
)

// The search orders.
const (
	MarketSortNewest    = "newest"
	MarketSortEnding    = "ending"
	MarketSortPriceAsc  = "price-asc"
	MarketSortPriceDesc = "price-desc"
)

// The race and rarity filters. An item either race wears (country 3, and
// every item that is no equipment) matches both races.
const (
	MarketRaceChinese  = "ch"
	MarketRaceEuropean = "eu"
	MarketRaritySealed = "sealed"
	MarketRarityNormal = "normal"
)

/*
================
MarketFilterValid

Whether a race and a rarity filter are known values ("" is any): one
check for the site's query and the in-game window.
================
*/
func MarketFilterValid(race, rarity string) bool {
	switch race {
	case "", MarketRaceChinese, MarketRaceEuropean:
	default:
		return false
	}
	switch rarity {
	case "", MarketRaritySealed, MarketRarityNormal:
		return true
	}
	return false
}

/*
================
MarketSource

The market behind the routes (game/market.Service). Account is always the
signed-in player's, from the path; the source checks every listing and
character against it.
================
*/
type MarketSource interface {
	Categories() []MarketCategory
	Search(q MarketSearch) (MarketListingsResponse, error)
	Account(account string) (OwnMarketResponse, error)
	List(account string, body MarketListBody) (MarketListingView, error)
	Buy(account string, body MarketBuyBody) (MarketSaleView, error)
	Cancel(account string, listing int64) error
	TakeBack(account string, listing int64) error
	MakeOffer(account string, body MarketOfferBody) (MarketOfferView, error)
	AcceptOffer(account string, offer int64) (MarketSaleView, error)
	RejectOffer(account string, offer int64) error
	WithdrawOffer(account string, offer int64) error
}

/*
================
MarketSearch

A parsed search: Degree 0 and the empty strings mean "any". Race is
MarketRaceChinese or MarketRaceEuropean; Rarity is MarketRaritySealed or
MarketRarityNormal.
================
*/
type MarketSearch struct {
	Q        string
	Category string
	Degree   int
	Race     string
	Rarity   string
	Currency string
	Sort     string
	Page     int
}

// MarketCategory is one picker entry.
type MarketCategory struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// MarketCategoriesResponse is GET /v1/market/categories.
type MarketCategoriesResponse struct {
	Categories []MarketCategory `json:"categories"`
}

// MarketItem is a listed (or warehouse) item as the site shows it. Name
// is the item's English name, "" when the references do not carry one.
type MarketItem struct {
	RefObjID uint32 `json:"refObjId"`
	Codename string `json:"codename"`
	Name     string `json:"name"`
	Category string `json:"category"`
	Degree   int    `json:"degree"`
	// Race is the race that wears it ("ch", "eu"), "" for either.
	Race string `json:"race,omitempty"`
	// Sealed is a sealed (rare) item.
	Sealed   bool  `json:"sealed,omitempty"`
	Plus     int64 `json:"plus"`
	Quantity int64 `json:"quantity"`
}

// MarketListingView is one listing. Seller is the listing character's
// name, "" when the seller listed anonymously or from the warehouse.
type MarketListingView struct {
	ID        int64      `json:"id"`
	Version   int64      `json:"version"`
	Item      MarketItem `json:"item"`
	Currency  string     `json:"currency"`
	Price     int64      `json:"price"`
	ListedMs  int64      `json:"listedMs"`
	ExpiresMs int64      `json:"expiresMs"`
	State     string     `json:"state"`
	Seller    string     `json:"seller,omitempty"`
	// Anonymous is shown to the seller alone, on its own listings.
	Anonymous bool `json:"anonymous,omitempty"`
	// Row is the listed item itself, for the in-game window's item
	// preview (game/action market.go); the site never receives it.
	Row domain.InventoryRow `json:"-"`
	// SellerChar is the listing character, 0 for a warehouse listing: the
	// game shows a character only its own (game/market GameAccount).
	SellerChar int64 `json:"-"`
}

// MarketListingsResponse is one search page.
type MarketListingsResponse struct {
	Listings []MarketListingView `json:"listings"`
	Page     int                 `json:"page"`
	Pages    int                 `json:"pages"`
	Total    int                 `json:"total"`
}

// MarketSaleView is one purchase. To is where the item went (bag or
// warehouse) and Slot the slot it took there.
type MarketSaleView struct {
	ListingID int64  `json:"listingId"`
	Currency  string `json:"currency"`
	Price     int64  `json:"price"`
	Fee       int64  `json:"fee"`
	AtMs      int64  `json:"atMs"`
	To        string `json:"to"`
	Slot      int64  `json:"slot"`
	// Item names the item; SellerChar is the listing character, for the
	// game's seller notice, and never leaves the server.
	Item       string `json:"item,omitempty"`
	SellerChar int64  `json:"-"`
	// BuyerChar is the buying character, 0 for a website buy.
	BuyerChar int64 `json:"-"`
}

// MarketOfferView is one offer, with the listing's item and asked price.
// Buyer names the offering character to the seller.
type MarketOfferView struct {
	ID        int64      `json:"id"`
	ListingID int64      `json:"listingId"`
	Item      MarketItem `json:"item"`
	Currency  string     `json:"currency"`
	Asked     int64      `json:"asked"`
	Price     int64      `json:"price"`
	Buyer     string     `json:"buyer"`
	To        string     `json:"to"`
	State     string     `json:"state"`
	AtMs      int64      `json:"atMs"`
	// Row is the listed item, for the in-game preview; the two parties'
	// characters route the game's notices. None is sent to the site.
	Row        domain.InventoryRow `json:"-"`
	SellerChar int64               `json:"-"`
	BuyerChar  int64               `json:"-"`
}

// MarketWarehouseItem is one warehouse row the player may list.
type MarketWarehouseItem struct {
	Slot      int64      `json:"slot"`
	Item      MarketItem `json:"item"`
	Tradeable bool       `json:"tradeable"`
}

// MarketCharacter is one of the account's characters for the gold-buy
// picker; an online one cannot pay from the website.
type MarketCharacter struct {
	Name   string `json:"name"`
	Gold   int64  `json:"gold"`
	Online bool   `json:"online"`
}

// MarketRulesView carries the server's numbers; the site writes none.
type MarketRulesView struct {
	FeePercent int64 `json:"feePercent"`
	DurationMs int64 `json:"durationMs"`
	OpenCap    int   `json:"openCap"`
	OfferCap   int   `json:"offerCap"`
}

// OwnMarketResponse is GET /v1/accounts/{id}/market.
type OwnMarketResponse struct {
	Listings      []MarketListingView   `json:"listings"`
	Purchases     []MarketSaleView      `json:"purchases"`
	Warehouse     []MarketWarehouseItem `json:"warehouse"`
	WarehouseGold int64                 `json:"warehouseGold"`
	Characters    []MarketCharacter     `json:"characters"`
	Silk          uint32                `json:"silk"`
	Rules         MarketRulesView       `json:"rules"`
	// OffersIn are the open offers on the account's listings, OffersOut
	// the account's own.
	OffersIn  []MarketOfferView `json:"offersIn"`
	OffersOut []MarketOfferView `json:"offersOut"`
}

// MarketListBody is POST .../market/list.
type MarketListBody struct {
	Slot     *int64 `json:"slot"`
	Currency string `json:"currency"`
	Price    int64  `json:"price"`
}

// MarketBuyBody is POST .../market/buy. Character names the paying
// character for a gold listing and is ignored for a Silk one.
type MarketBuyBody struct {
	Listing   int64  `json:"listing"`
	Version   int64  `json:"version"`
	Character string `json:"character,omitempty"`
}

// MarketOfferBody is POST .../market/offer. Character names the offering
// character: the gold payer of a gold listing, and the one an accepted
// offer delivers to. A website offer's item goes to the warehouse.
type MarketOfferBody struct {
	Listing   int64  `json:"listing"`
	Price     int64  `json:"price"`
	Character string `json:"character"`
}

// ErrMarketOnline refuses a website gold buy from an online character.
var ErrMarketOnline = errors.New("market: the paying character is online")

/*
================
marketError

The status and code a refusal answers.
================
*/
func marketError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrMarketNotFound):
		return http.StatusNotFound, "not-found"
	case errors.Is(err, domain.ErrMarketNoItem):
		return http.StatusNotFound, "no-item"
	case errors.Is(err, ErrNotOwned):
		return http.StatusNotFound, "no-character"
	case errors.Is(err, domain.ErrMarketChanged):
		return http.StatusConflict, "changed"
	case errors.Is(err, ErrMarketOnline):
		return http.StatusConflict, "online"
	case errors.Is(err, domain.ErrMarketWarehouseFull):
		return http.StatusConflict, "warehouse-full"
	case errors.Is(err, domain.ErrMarketNotHeld):
		return http.StatusConflict, "not-held"
	case errors.Is(err, domain.ErrMarketCap):
		return http.StatusConflict, "cap"
	case errors.Is(err, domain.ErrMarketOwnListing):
		return http.StatusUnprocessableEntity, "own-listing"
	case errors.Is(err, domain.ErrMarketNotTradeable):
		return http.StatusUnprocessableEntity, "not-tradeable"
	case errors.Is(err, domain.ErrMarketFunds):
		return http.StatusUnprocessableEntity, "funds"
	case errors.Is(err, domain.ErrMarketPrice):
		return http.StatusBadRequest, "price"
	case errors.Is(err, domain.ErrMarketBagFull):
		return http.StatusConflict, "bag-full"
	case errors.Is(err, domain.ErrMarketOfferPrice):
		return http.StatusUnprocessableEntity, "offer-price"
	case errors.Is(err, domain.ErrMarketOfferOpen):
		return http.StatusConflict, "offer-open"
	case errors.Is(err, domain.ErrMarketOfferCap):
		return http.StatusConflict, "offer-cap"
	case errors.Is(err, domain.ErrMarketOfferGone):
		return http.StatusConflict, "offer-gone"
	case errors.Is(err, domain.ErrMarketBuyerFunds):
		return http.StatusConflict, "buyer-funds"
	case errors.Is(err, domain.ErrMarketBuyerFull):
		return http.StatusConflict, "buyer-full"
	}
	return http.StatusServiceUnavailable, "unavailable"
}

/*
================
MarketErrorCode

The refusal's code alone, for a front end that is not HTTP: the in-game
window (game/action market.go) names refusals with the site's codes.
================
*/
func MarketErrorCode(err error) string {
	_, code := marketError(err)
	return code
}

/*
================
installMarket
================
*/
func (s *Service) installMarket(mux *http.ServeMux, digest [sha256.Size]byte) {
	market := s.src.Market
	if market == nil {
		return
	}
	mux.HandleFunc("GET /v1/market/categories", func(w http.ResponseWriter, r *http.Request) {
		if authorized(w, r, digest) {
			writeMarketJSON(w, http.StatusOK, MarketCategoriesResponse{Categories: market.Categories()})
		}
	})
	mux.HandleFunc("GET /v1/market/listings", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		search, ok := parseMarketSearch(r, market.Categories())
		if !ok {
			writeStatus(w, http.StatusBadRequest, "search")
			return
		}
		answer, err := market.Search(search)
		writeMarketAnswer(w, http.StatusOK, answer, err)
	})
	mux.HandleFunc("GET /v1/accounts/{id}/market", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		answer, err := market.Account(r.PathValue("id"))
		writeMarketAnswer(w, http.StatusOK, answer, err)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/list", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		var body MarketListBody
		if !decodeMarketBody(w, r, &body) || body.Slot == nil {
			writeStatus(w, http.StatusBadRequest, "body")
			return
		}
		answer, err := market.List(r.PathValue("id"), body)
		writeMarketAnswer(w, http.StatusCreated, answer, err)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/buy", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		var body MarketBuyBody
		if !decodeMarketBody(w, r, &body) || body.Listing < 1 || body.Version < 1 || len(body.Character) > maxNameLength {
			writeStatus(w, http.StatusBadRequest, "body")
			return
		}
		answer, err := market.Buy(r.PathValue("id"), body)
		writeMarketAnswer(w, http.StatusCreated, answer, err)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/{listing}/cancel", func(w http.ResponseWriter, r *http.Request) {
		s.marketListingAction(w, r, digest, market.Cancel)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/{listing}/take-back", func(w http.ResponseWriter, r *http.Request) {
		s.marketListingAction(w, r, digest, market.TakeBack)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/offer", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		var body MarketOfferBody
		if !decodeMarketBody(w, r, &body) || body.Listing < 1 || body.Price < 1 || body.Character == "" ||
			len(body.Character) > maxNameLength {
			writeStatus(w, http.StatusBadRequest, "body")
			return
		}
		answer, err := market.MakeOffer(r.PathValue("id"), body)
		writeMarketAnswer(w, http.StatusCreated, answer, err)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/offers/{offer}/accept", func(w http.ResponseWriter, r *http.Request) {
		if !authorized(w, r, digest) {
			return
		}
		offer, err := strconv.ParseInt(r.PathValue("offer"), 10, 64)
		if err != nil || offer < 1 {
			writeStatus(w, http.StatusNotFound, "offer-gone")
			return
		}
		answer, err := market.AcceptOffer(r.PathValue("id"), offer)
		writeMarketAnswer(w, http.StatusCreated, answer, err)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/offers/{offer}/reject", func(w http.ResponseWriter, r *http.Request) {
		s.marketOfferAction(w, r, digest, market.RejectOffer)
	})
	mux.HandleFunc("POST /v1/accounts/{id}/market/offers/{offer}/withdraw", func(w http.ResponseWriter, r *http.Request) {
		s.marketOfferAction(w, r, digest, market.WithdrawOffer)
	})
}

/*
================
marketOfferAction

A reject or withdraw: 204, or the refusal's code.
================
*/
func (s *Service) marketOfferAction(w http.ResponseWriter, r *http.Request, digest [sha256.Size]byte, act func(string, int64) error) {
	if !authorized(w, r, digest) {
		return
	}
	offer, err := strconv.ParseInt(r.PathValue("offer"), 10, 64)
	if err != nil || offer < 1 {
		writeStatus(w, http.StatusNotFound, "offer-gone")
		return
	}
	if err := act(r.PathValue("id"), offer); err != nil {
		status, code := marketError(err)
		writeStatus(w, status, code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
================
marketListingAction

A cancel or take-back: 204, or the refusal's code.
================
*/
func (s *Service) marketListingAction(w http.ResponseWriter, r *http.Request, digest [sha256.Size]byte, act func(string, int64) error) {
	if !authorized(w, r, digest) {
		return
	}
	listing, err := strconv.ParseInt(r.PathValue("listing"), 10, 64)
	if err != nil || listing < 1 {
		writeStatus(w, http.StatusNotFound, "not-found")
		return
	}
	if err := act(r.PathValue("id"), listing); err != nil {
		status, code := marketError(err)
		writeStatus(w, status, code)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

/*
================
parseMarketSearch

The search from the query; false for any value outside its list.
================
*/
func parseMarketSearch(r *http.Request, categories []MarketCategory) (MarketSearch, bool) {
	query := r.URL.Query()
	search := MarketSearch{Q: query.Get("q"), Category: query.Get("category"), Currency: query.Get("currency"),
		Race: query.Get("race"), Rarity: query.Get("rarity"), Sort: query.Get("sort"), Page: 1}
	if len(search.Q) > maxMarketQuery || !MarketFilterValid(search.Race, search.Rarity) {
		return search, false
	}
	if search.Category != "" && !knownCategory(categories, search.Category) {
		return search, false
	}
	if search.Currency != "" && search.Currency != domain.MarketGold && search.Currency != domain.MarketSilk {
		return search, false
	}
	switch search.Sort {
	case "":
		search.Sort = MarketSortNewest
	case MarketSortNewest, MarketSortEnding, MarketSortPriceAsc, MarketSortPriceDesc:
	default:
		return search, false
	}
	if value := query.Get("degree"); value != "" {
		degree, err := strconv.Atoi(value)
		if err != nil || degree < 1 || degree > MarketMaxDegree {
			return search, false
		}
		search.Degree = degree
	}
	if value := query.Get("page"); value != "" {
		page, err := strconv.Atoi(value)
		if err != nil || page < 1 || page > MarketMaxPage {
			return search, false
		}
		search.Page = page
	}
	return search, true
}

/*
================
knownCategory
================
*/
func knownCategory(categories []MarketCategory, slug string) bool {
	for _, category := range categories {
		if category.Slug == slug {
			return true
		}
	}
	return false
}

/*
================
decodeMarketBody
================
*/
func decodeMarketBody(w http.ResponseWriter, r *http.Request, into any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxMarketBody))
	decoder.DisallowUnknownFields()
	return decoder.Decode(into) == nil
}

/*
================
writeMarketAnswer
================
*/
func writeMarketAnswer(w http.ResponseWriter, status int, answer any, err error) {
	if err != nil {
		code, message := marketError(err)
		writeStatus(w, code, message)
		return
	}
	writeMarketJSON(w, status, answer)
}

/*
================
writeMarketJSON
================
*/
func writeMarketJSON(w http.ResponseWriter, status int, answer any) {
	body, err := json.Marshal(answer)
	if err != nil {
		writeStatus(w, http.StatusInternalServerError, "encode")
		return
	}
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
