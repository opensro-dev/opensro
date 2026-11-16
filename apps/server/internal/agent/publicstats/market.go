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
}

/*
================
MarketSearch

A parsed search: Degree 0 and the empty strings mean "any".
================
*/
type MarketSearch struct {
	Q        string
	Category string
	Degree   int
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
	Plus     int64  `json:"plus"`
	Quantity int64  `json:"quantity"`
}

// MarketListingView is one listing. The seller is never named.
type MarketListingView struct {
	ID        int64      `json:"id"`
	Version   int64      `json:"version"`
	Item      MarketItem `json:"item"`
	Currency  string     `json:"currency"`
	Price     int64      `json:"price"`
	ListedMs  int64      `json:"listedMs"`
	ExpiresMs int64      `json:"expiresMs"`
	State     string     `json:"state"`
}

// MarketListingsResponse is one search page.
type MarketListingsResponse struct {
	Listings []MarketListingView `json:"listings"`
	Page     int                 `json:"page"`
	Pages    int                 `json:"pages"`
	Total    int                 `json:"total"`
}

// MarketSaleView is one purchase.
type MarketSaleView struct {
	ListingID int64  `json:"listingId"`
	Currency  string `json:"currency"`
	Price     int64  `json:"price"`
	Fee       int64  `json:"fee"`
	AtMs      int64  `json:"atMs"`
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
	}
	return http.StatusServiceUnavailable, "unavailable"
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
		Sort: query.Get("sort"), Page: 1}
	if len(search.Q) > maxMarketQuery {
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
