/*
===========================================================================

service_test.go - the website market's rules: warehouse-only listing,
offline-only gold buys, and the search's filters, order and pages

===========================================================================
*/
package market

import (
	"errors"
	"testing"

	"opensro.online/server/internal/agent/publicstats"
	"opensro.online/server/internal/domain"
)

/*
================
fakeServiceStore
================
*/
type fakeServiceStore struct {
	open    []domain.MarketListing
	listed  []domain.MarketListRequest
	bought  []domain.MarketBuyRequest
	byID    map[int64]domain.MarketListing
	storage domain.AccountStorage
	// enteredDuringBuy marks characters whose entry lands after the
	// session list was read: the list says offline, the admission gate
	// says online.
	enteredDuringBuy map[int64]bool
}

func (f *fakeServiceStore) MarketList(req domain.MarketListRequest) (domain.MarketListing, error) {
	f.listed = append(f.listed, req)
	for _, row := range f.storage.Rows {
		if row.Slot == req.Slot {
			if err := req.Admit(row); err != nil {
				return domain.MarketListing{}, err
			}
			return domain.MarketListing{ID: 1, Item: row, Currency: req.Currency, Price: req.Price}, nil
		}
	}
	return domain.MarketListing{}, domain.ErrMarketNoItem
}

func (f *fakeServiceStore) MarketBuy(req domain.MarketBuyRequest) (domain.MarketSale, error) {
	f.bought = append(f.bought, req)
	return domain.MarketSale{ListingID: req.ListingID}, nil
}

func (f *fakeServiceStore) MarketCancel(string, int64) (domain.MarketListing, error) {
	return domain.MarketListing{}, nil
}

func (f *fakeServiceStore) MarketTakeBack(string, int64) (domain.MarketListing, error) {
	return domain.MarketListing{}, nil
}

func (f *fakeServiceStore) MarketListingByID(id int64) (domain.MarketListing, error) {
	if listing, ok := f.byID[id]; ok {
		return listing, nil
	}
	return domain.MarketListing{}, domain.ErrMarketNotFound
}

func (f *fakeServiceStore) MarketOpenListings(q domain.MarketQuery) ([]domain.MarketListing, error) {
	out := []domain.MarketListing{}
	for _, listing := range f.open {
		if q.Currency == "" || listing.Currency == q.Currency {
			out = append(out, listing)
		}
	}
	return out, nil
}

func (f *fakeServiceStore) MarketAccountListings(string, int) ([]domain.MarketListing, error) {
	return nil, nil
}

func (f *fakeServiceStore) MarketPurchases(string, int) ([]domain.MarketSale, error) { return nil, nil }

func (f *fakeServiceStore) MarketWarehouse(string) (domain.AccountStorage, error) {
	return f.storage, nil
}

func (f *fakeServiceStore) MarketWallet(string) (domain.MallBalance, error) {
	return domain.MallBalance{Silk: 40}, nil
}

/*
================
testService

Two items: a degree-1 sword (weapon) and an HP potion (consumable). The
account "alpha" has Kekw (online) and Lulu (offline).
================
*/
func testService(store *fakeServiceStore) *Service {
	kekw := &domain.Character{ID: 1, AccountID: "alpha", Name: "Kekw"}
	lulu := &domain.Character{ID: 2, AccountID: "alpha", Name: "Lulu"}
	return &Service{
		Store:    store,
		Division: "global-official",
		Item: func(codename string) (ItemInfo, bool) {
			switch codename {
			case "ITEM_CH_SWORD_01_A":
				return ItemInfo{Name: "Iron Sword", TypeIDs: [4]int64{3, 1, 6, 2}, Degree: 1}, true
			case "ITEM_ETC_HP_POTION_01":
				return ItemInfo{Name: "HP Recovery Herb", TypeIDs: [4]int64{3, 3, 1, 1}}, true
			}
			return ItemInfo{}, false
		},
		Tradeable:  func(row domain.InventoryRow) bool { return !row.ForgeBound },
		Characters: func(string) []*domain.Character { return []*domain.Character{kekw, lulu} },
		Online:     func(id int64) bool { return id == kekw.ID },
		WhileOffline: func(c *domain.Character, write func() error) (bool, error) {
			if c.ID == kekw.ID || store.enteredDuringBuy[c.ID] {
				return true, nil
			}
			return false, write()
		},
	}
}

/*
================
TestCategoryFollowsTheTypeIDs
================
*/
func TestCategoryFollowsTheTypeIDs(t *testing.T) {
	for types, want := range map[[4]int64]string{
		{3, 1, 6, 2}:  "weapon",
		{3, 1, 4, 1}:  "shield",
		{3, 1, 10, 1}: "armor",
		{3, 1, 12, 1}: "accessory",
		{3, 1, 13, 1}: "avatar",
		{3, 2, 1, 1}:  "pet",
		{3, 3, 1, 1}:  "consumable",
		{3, 1, 7, 1}:  "other",
		{1, 1, 1, 1}:  "other",
	} {
		if got := Category(types); got != want {
			t.Errorf("Category(%v) = %q, want %q", types, got, want)
		}
	}
	for _, category := range (&Service{}).Categories() {
		if category.Slug == "" || category.Name == "" {
			t.Fatalf("a blank category %+v", category)
		}
	}
}

/*
================
TestSearchFiltersOrdersAndPages
================
*/
func TestSearchFiltersOrdersAndPages(t *testing.T) {
	store := &fakeServiceStore{}
	for i := int64(1); i <= 20; i++ {
		codename := "ITEM_CH_SWORD_01_A"
		if i%2 == 0 {
			codename = "ITEM_ETC_HP_POTION_01"
		}
		store.open = append(store.open, domain.MarketListing{ID: i, Item: domain.InventoryRow{Codename: codename},
			Currency: domain.MarketGold, Price: 100 - i, ListedMs: i, ExpiresMs: 1000 - i})
	}
	s := testService(store)

	page, _ := s.Search(publicstats.MarketSearch{Sort: publicstats.MarketSortNewest, Page: 1})
	if page.Total != 20 || page.Pages != 2 || len(page.Listings) != 15 || page.Listings[0].ID != 20 {
		t.Fatalf("first page = %d of %d, %d rows, first %d", page.Page, page.Pages, len(page.Listings), page.Listings[0].ID)
	}
	second, _ := s.Search(publicstats.MarketSearch{Sort: publicstats.MarketSortNewest, Page: 2})
	if len(second.Listings) != 5 {
		t.Fatalf("second page has %d rows, want 5", len(second.Listings))
	}
	beyond, _ := s.Search(publicstats.MarketSearch{Sort: publicstats.MarketSortNewest, Page: 9})
	if len(beyond.Listings) != 0 {
		t.Fatalf("a page past the end has %d rows", len(beyond.Listings))
	}
	weapons, _ := s.Search(publicstats.MarketSearch{Category: "weapon", Degree: 1, Sort: publicstats.MarketSortPriceAsc, Page: 1})
	if weapons.Total != 10 || weapons.Listings[0].Item.Name != "Iron Sword" || weapons.Listings[0].ID != 19 {
		t.Fatalf("weapons = %d, first %+v", weapons.Total, weapons.Listings[0])
	}
	herbs, _ := s.Search(publicstats.MarketSearch{Q: "herb", Sort: publicstats.MarketSortEnding, Page: 1})
	if herbs.Total != 10 || herbs.Listings[0].ID != 20 {
		t.Fatalf("herb search = %d, first %d", herbs.Total, herbs.Listings[0].ID)
	}
	none, _ := s.Search(publicstats.MarketSearch{Category: "weapon", Degree: 2, Sort: publicstats.MarketSortNewest, Page: 1})
	if none.Total != 0 || none.Pages != 0 {
		t.Fatalf("no match = %+v", none)
	}
}

/*
================
TestListIsFromTheWarehouseAndAppliesTheTradeRule
================
*/
func TestListIsFromTheWarehouseAndAppliesTheTradeRule(t *testing.T) {
	store := &fakeServiceStore{storage: domain.AccountStorage{Capacity: 150, Rows: []domain.InventoryRow{
		{Slot: 0, Codename: "ITEM_CH_SWORD_01_A"}, {Slot: 1, Codename: "ITEM_CH_SWORD_01_A", ForgeBound: true}}}}
	s := testService(store)
	slot := int64(0)
	if _, err := s.List("alpha", publicstats.MarketListBody{Slot: &slot, Currency: domain.MarketGold, Price: 10}); err != nil {
		t.Fatal(err)
	}
	if request := store.listed[0]; request.Character != nil || request.Account != "alpha" || request.Rules != Rules() {
		t.Fatalf("list request = %+v", request)
	}
	bound := int64(1)
	if _, err := s.List("alpha", publicstats.MarketListBody{Slot: &bound, Currency: domain.MarketGold, Price: 10}); !errors.Is(err, domain.ErrMarketNotTradeable) {
		t.Fatalf("a bound item listed: %v", err)
	}
	account, err := s.Account("alpha")
	if err != nil || len(account.Warehouse) != 2 || !account.Warehouse[0].Tradeable || account.Warehouse[1].Tradeable {
		t.Fatalf("account warehouse = %+v, %v", account.Warehouse, err)
	}
	if len(account.Characters) != 2 || !account.Characters[0].Online || account.Characters[1].Online || account.Silk != 40 {
		t.Fatalf("account characters = %+v, silk %d", account.Characters, account.Silk)
	}
}

/*
================
TestGoldBuyNeedsAnOfflineCharacterOfTheAccount
================
*/
func TestGoldBuyNeedsAnOfflineCharacterOfTheAccount(t *testing.T) {
	store := &fakeServiceStore{byID: map[int64]domain.MarketListing{
		1: {ID: 1, Division: "global-official", Currency: domain.MarketGold},
		2: {ID: 2, Division: "global-official", Currency: domain.MarketSilk},
		3: {ID: 3, Division: "another-shard", Currency: domain.MarketSilk},
	}}
	s := testService(store)
	for name, want := range map[string]error{"Kekw": publicstats.ErrMarketOnline, "Nobody": publicstats.ErrNotOwned, "": publicstats.ErrNotOwned} {
		if _, err := s.Buy("alpha", publicstats.MarketBuyBody{Listing: 1, Version: 1, Character: name}); !errors.Is(err, want) {
			t.Errorf("gold buy paid by %q: %v, want %v", name, err, want)
		}
	}
	if len(store.bought) != 0 {
		t.Fatalf("a refused buy reached the store: %+v", store.bought)
	}
	if _, err := s.Buy("alpha", publicstats.MarketBuyBody{Listing: 1, Version: 1, Character: "lulu"}); err != nil {
		t.Fatal(err)
	}
	if payer := store.bought[0].Payer; payer == nil || payer.Name != "Lulu" {
		t.Fatalf("payer = %+v", payer)
	}
	if _, err := s.Buy("alpha", publicstats.MarketBuyBody{Listing: 2, Version: 1}); err != nil || store.bought[1].Payer != nil {
		t.Fatalf("a Silk buy = %v, payer %+v", err, store.bought[1].Payer)
	}
	if _, err := s.Buy("alpha", publicstats.MarketBuyBody{Listing: 3, Version: 1}); !errors.Is(err, domain.ErrMarketNotFound) {
		t.Fatalf("another shard's listing: %v", err)
	}

	// Lulu enters between the session list and the buy: the list still
	// says offline, but the admission gate refuses, and nothing is paid.
	store.enteredDuringBuy = map[int64]bool{2: true}
	bought := len(store.bought)
	if _, err := s.Buy("alpha", publicstats.MarketBuyBody{Listing: 1, Version: 1, Character: "Lulu"}); !errors.Is(err, publicstats.ErrMarketOnline) {
		t.Fatalf("a buy paid by a character entering the world: %v", err)
	}
	if len(store.bought) != bought {
		t.Fatal("a buy refused at the gate reached the store")
	}
}
