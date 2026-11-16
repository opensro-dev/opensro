/*
===========================================================================

market_test.go - the market routes: served only with a source, behind
the token, strict about the search, and every refusal mapped to its code

===========================================================================
*/
package publicstats

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
fakeMarket

A MarketSource with one listing and one purchase; err, when set, is what
every write answers.
================
*/
type fakeMarket struct {
	searched []MarketSearch
	err      error
}

func (f *fakeMarket) Categories() []MarketCategory {
	return []MarketCategory{{Slug: "weapon", Name: "Weapons"}, {Slug: "other", Name: "Other"}}
}

func sampleListing() MarketListingView {
	return MarketListingView{ID: 7, Version: 1, Item: MarketItem{RefObjID: 3117, Codename: "ITEM_EU_SWORD_01_A", Name: "Sword",
		Category: "weapon", Degree: 1, Plus: 2, Quantity: 1}, Currency: domain.MarketGold, Price: 1000, ListedMs: 1, ExpiresMs: 2,
		State: domain.MarketOpen}
}

func (f *fakeMarket) Search(q MarketSearch) (MarketListingsResponse, error) {
	f.searched = append(f.searched, q)
	return MarketListingsResponse{Listings: []MarketListingView{sampleListing()}, Page: q.Page, Pages: 1, Total: 1}, nil
}

func (f *fakeMarket) Account(string) (OwnMarketResponse, error) {
	return OwnMarketResponse{
		Listings:   []MarketListingView{sampleListing()},
		Purchases:  []MarketSaleView{{ListingID: 6, Currency: domain.MarketSilk, Price: 50, AtMs: 3}},
		Warehouse:  []MarketWarehouseItem{{Slot: 0, Item: sampleListing().Item, Tradeable: true}},
		Characters: []MarketCharacter{{Name: "Kekw", Gold: 5, Online: true}},
		Silk:       10,
		Rules:      MarketRulesView{FeePercent: 1, DurationMs: 86400000, OpenCap: 20},
	}, nil
}

func (f *fakeMarket) List(string, MarketListBody) (MarketListingView, error) {
	return sampleListing(), f.err
}

func (f *fakeMarket) Buy(string, MarketBuyBody) (MarketSaleView, error) {
	return MarketSaleView{ListingID: 7, Currency: domain.MarketGold, Price: 1000, Fee: 10}, f.err
}

func (f *fakeMarket) Cancel(string, int64) error   { return f.err }
func (f *fakeMarket) TakeBack(string, int64) error { return f.err }

/*
================
authorizedPost
================
*/
func authorizedPost(s *Service, path, body string) (int, string) {
	rec := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testWriteToken)
	s.Handler().ServeHTTP(rec, request)
	return rec.Code, rec.Body.String()
}

/*
================
TestMarketRoutesAreUnservedWithoutASource
================
*/
func TestMarketRoutesAreUnservedWithoutASource(t *testing.T) {
	s := privacyService(newFixture(), testWriteToken)
	for _, path := range []string{"/v1/market/categories", "/v1/market/listings", "/v1/accounts/alpha/market"} {
		if code, _ := authorizedGet(s, path, testWriteToken); code != http.StatusNotFound {
			t.Errorf("%s answered %d with SRO_CONSIGNMENT off, want 404", path, code)
		}
	}
	if code, _ := authorizedPost(s, "/v1/accounts/alpha/market/buy", `{"listing":1,"version":1}`); code != http.StatusNotFound {
		t.Errorf("buy answered %d with SRO_CONSIGNMENT off, want 404", code)
	}
}

/*
================
TestMarketSearchIsStrict
================
*/
func TestMarketSearchIsStrict(t *testing.T) {
	s := privacyService(newFixture(), testWriteToken)
	market := &fakeMarket{}
	s.src.Market = market
	if code, _ := authorizedGet(s, "/v1/market/listings", ""); code != http.StatusUnauthorized {
		t.Fatalf("no token answered %d, want 401", code)
	}
	for _, query := range []string{"category=nope", "degree=12", "degree=x", "currency=gems", "sort=cheap", "page=0", "page=101",
		"q=" + strings.Repeat("a", 65)} {
		if code, _ := authorizedGet(s, "/v1/market/listings?"+query, testWriteToken); code != http.StatusBadRequest {
			t.Errorf("%s answered %d, want 400", query, code)
		}
	}
	code, body := authorizedGet(s, "/v1/market/listings?q=Sw&category=weapon&degree=1&currency=gold&sort=price-asc&page=2", testWriteToken)
	if code != http.StatusOK {
		t.Fatalf("search answered %d: %s", code, body)
	}
	want := MarketSearch{Q: "Sw", Category: "weapon", Degree: 1, Currency: "gold", Sort: MarketSortPriceAsc, Page: 2}
	if len(market.searched) != 1 || market.searched[0] != want {
		t.Fatalf("searched %+v, want %+v", market.searched, want)
	}
	authorizedGet(s, "/v1/market/listings", testWriteToken)
	if got := market.searched[1]; got.Sort != MarketSortNewest || got.Page != 1 {
		t.Fatalf("defaults %+v, want newest page 1", got)
	}
}

/*
================
TestMarketWritesMapEveryRefusal
================
*/
func TestMarketWritesMapEveryRefusal(t *testing.T) {
	s := privacyService(newFixture(), testWriteToken)
	market := &fakeMarket{}
	s.src.Market = market
	if code, body := authorizedPost(s, "/v1/accounts/alpha/market/list", `{"slot":0,"currency":"silk","price":5}`); code != http.StatusCreated {
		t.Fatalf("list answered %d: %s", code, body)
	}
	if code, _ := authorizedPost(s, "/v1/accounts/alpha/market/list", `{"currency":"silk","price":5}`); code != http.StatusBadRequest {
		t.Fatalf("a list without a slot answered %d, want 400", code)
	}
	if code, _ := authorizedPost(s, "/v1/accounts/alpha/market/buy", `{"listing":7,"version":1,"extra":true}`); code != http.StatusBadRequest {
		t.Fatalf("an unknown field answered %d, want 400", code)
	}
	if code, _ := authorizedPost(s, "/v1/accounts/alpha/market/7/cancel", ""); code != http.StatusNoContent {
		t.Fatalf("cancel answered %d", code)
	}
	if code, _ := authorizedPost(s, "/v1/accounts/alpha/market/x/take-back", ""); code != http.StatusNotFound {
		t.Fatalf("a bad listing id answered %d", code)
	}
	for err, want := range map[error]struct {
		status int
		code   string
	}{
		domain.ErrMarketChanged:       {http.StatusConflict, "changed"},
		ErrMarketOnline:               {http.StatusConflict, "online"},
		domain.ErrMarketWarehouseFull: {http.StatusConflict, "warehouse-full"},
		domain.ErrMarketFunds:         {http.StatusUnprocessableEntity, "funds"},
		domain.ErrMarketOwnListing:    {http.StatusUnprocessableEntity, "own-listing"},
		domain.ErrMarketNotFound:      {http.StatusNotFound, "not-found"},
		ErrNotOwned:                   {http.StatusNotFound, "no-character"},
		domain.ErrMarketUnavailable:   {http.StatusServiceUnavailable, "unavailable"},
	} {
		market.err = err
		code, body := authorizedPost(s, "/v1/accounts/alpha/market/buy", `{"listing":7,"version":1,"character":"Kekw"}`)
		var answer map[string]string
		_ = json.Unmarshal([]byte(body), &answer)
		if code != want.status || answer["error"] != want.code {
			t.Errorf("%v answered %d %q, want %d %q", err, code, answer["error"], want.status, want.code)
		}
	}
}
