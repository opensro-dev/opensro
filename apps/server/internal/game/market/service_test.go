/*
===========================================================================

service_test.go - the website market's rules: warehouse-only listing,
offline-only gold buys, and the search's filters, order and pages

===========================================================================
*/
package market

import (
	"errors"
	"slices"
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
	reclaimed        []domain.MarketReclaimRequest
	offered          []domain.MarketOfferRequest
	accepted         []domain.MarketAcceptRequest
	offers           map[int64]domain.MarketOffer
}

func (f *fakeServiceStore) MarketReclaim(req domain.MarketReclaimRequest) (domain.MarketListing, error) {
	f.reclaimed = append(f.reclaimed, req)
	return domain.MarketListing{}, nil
}

func (f *fakeServiceStore) MarketMakeOffer(req domain.MarketOfferRequest) (domain.MarketOffer, error) {
	f.offered = append(f.offered, req)
	return domain.MarketOffer{ID: 9, ListingID: req.ListingID, Price: req.Price, To: req.To, BuyerName: req.Buyer.Name}, nil
}

func (f *fakeServiceStore) MarketAcceptOffer(req domain.MarketAcceptRequest) (domain.MarketSale, domain.MarketOffer, error) {
	f.accepted = append(f.accepted, req)
	return domain.MarketSale{ListingID: 1}, domain.MarketOffer{}, nil
}

func (f *fakeServiceStore) MarketRejectOffer(string, int64) (domain.MarketOffer, error) {
	return domain.MarketOffer{ListingID: 1}, nil
}

func (f *fakeServiceStore) MarketWithdrawOffer(string, int64) (domain.MarketOffer, error) {
	return domain.MarketOffer{ListingID: 1}, nil
}

func (f *fakeServiceStore) MarketOffers(string, int) ([]domain.MarketOffer, []domain.MarketOffer, error) {
	return nil, nil, nil
}

func (f *fakeServiceStore) MarketOfferByID(id int64) (domain.MarketOffer, error) {
	if offer, ok := f.offers[id]; ok {
		return offer, nil
	}
	return domain.MarketOffer{}, domain.ErrMarketOfferGone
}

func (f *fakeServiceStore) MarketOwedCharacters() (map[int64]bool, error) { return nil, nil }

func (f *fakeServiceStore) MarketClaimProceeds(*domain.Character) ([]domain.MarketSale, error) {
	return nil, nil
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
TestSearchFiltersRaceAndRarity

A Chinese sword, a sealed European sword and a herb either race uses:
the race filter keeps the race's items and the shared herb; the rarity
filter splits sealed from normal.
================
*/
func TestSearchFiltersRaceAndRarity(t *testing.T) {
	store := &fakeServiceStore{}
	for i, codename := range []string{"ITEM_CH_SWORD_01_A", "ITEM_EU_SWORD_01_A_RARE", "ITEM_ETC_HP_POTION_01"} {
		store.open = append(store.open, domain.MarketListing{ID: int64(i + 1), Item: domain.InventoryRow{Codename: codename},
			Currency: domain.MarketGold, Price: 10, ListedMs: int64(i)})
	}
	s := testService(store)
	item := s.Item
	s.Item = func(codename string) (ItemInfo, bool) {
		switch codename {
		case "ITEM_CH_SWORD_01_A":
			return ItemInfo{Name: "Iron Sword", TypeIDs: [4]int64{3, 1, 6, 2}, Degree: 1, Race: publicstats.MarketRaceChinese}, true
		case "ITEM_EU_SWORD_01_A_RARE":
			return ItemInfo{Name: "Sword (Seal of Star)", TypeIDs: [4]int64{3, 1, 6, 7}, Degree: 1,
				Race: publicstats.MarketRaceEuropean, Sealed: true}, true
		}
		return item(codename)
	}
	ids := func(q publicstats.MarketSearch) []int64 {
		q.Page = 1
		page, err := s.Search(q)
		if err != nil {
			t.Fatal(err)
		}
		out := []int64{}
		for _, listing := range page.Listings {
			out = append(out, listing.ID)
		}
		return out
	}
	for _, c := range []struct {
		q    publicstats.MarketSearch
		want []int64
	}{
		{publicstats.MarketSearch{Race: publicstats.MarketRaceChinese}, []int64{3, 1}},
		{publicstats.MarketSearch{Race: publicstats.MarketRaceEuropean}, []int64{3, 2}},
		{publicstats.MarketSearch{Rarity: publicstats.MarketRaritySealed}, []int64{2}},
		{publicstats.MarketSearch{Rarity: publicstats.MarketRarityNormal}, []int64{3, 1}},
		{publicstats.MarketSearch{Race: publicstats.MarketRaceChinese, Rarity: publicstats.MarketRaritySealed}, []int64{}},
	} {
		if got := ids(c.q); !slices.Equal(got, c.want) {
			t.Errorf("search %+v = %v, want %v", c.q, got, c.want)
		}
	}
	for _, bad := range [][2]string{{"kr", ""}, {"", "unique"}} {
		if publicstats.MarketFilterValid(bad[0], bad[1]) {
			t.Errorf("filter %v accepted", bad)
		}
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

/*
================
TestGameDoorsActForTheOnlineCharacter

The in-game doors are the website's mirror: a listing names the
character and so comes from its bag, and a gold buy is paid by that
character although it is online (the action lane owns it), never
through the offline gate. A Silk buy names no payer; another shard's
listing is not found.
================
*/
func TestGameDoorsActForTheOnlineCharacter(t *testing.T) {
	store := &fakeServiceStore{byID: map[int64]domain.MarketListing{
		1: {ID: 1, Division: "global-official", Currency: domain.MarketGold},
		2: {ID: 2, Division: "global-official", Currency: domain.MarketSilk},
		3: {ID: 3, Division: "another-shard", Currency: domain.MarketGold},
	}, storage: domain.AccountStorage{Rows: []domain.InventoryRow{
		{Slot: 13, Codename: "ITEM_CH_SWORD_01_A"},
		{Slot: 14, Codename: "ITEM_CH_SWORD_01_A", ForgeBound: true},
	}}}
	s := testService(store)
	kekw := &domain.Character{ID: 1, AccountID: "alpha", Name: "Kekw"}

	if _, err := s.GameList(kekw, 13, domain.MarketGold, 500, true); err != nil {
		t.Fatal(err)
	}
	if req := store.listed[0]; req.Character != kekw || req.Account != "alpha" || req.Slot != 13 || req.Price != 500 ||
		!req.Anonymous {
		t.Fatalf("a game listing = %+v", req)
	}
	if _, err := s.GameList(kekw, 14, domain.MarketGold, 500, false); !errors.Is(err, domain.ErrMarketNotTradeable) {
		t.Fatalf("a forge-bound listing: %v", err)
	}

	if _, err := s.GameBuy(kekw, 1, 4, domain.MarketToBag, 20); err != nil {
		t.Fatal(err)
	}
	if req := store.bought[0]; req.Payer != kekw || req.BuyerAccount != "alpha" || req.Version != 4 ||
		req.To != domain.MarketToBag || req.Receiver != kekw || req.BagSlot != 20 {
		t.Fatalf("a game gold buy = %+v", req)
	}
	if _, err := s.GameBuy(kekw, 2, 1, domain.MarketToWarehouse, -1); err != nil || store.bought[1].Payer != nil ||
		store.bought[1].Receiver != nil {
		t.Fatalf("a game Silk buy = %v, %+v", err, store.bought[1])
	}
	if _, err := s.GameBuy(kekw, 3, 1, domain.MarketToWarehouse, -1); !errors.Is(err, domain.ErrMarketNotFound) || len(store.bought) != 2 {
		t.Fatalf("another shard's listing: %v", err)
	}
}

/*
================
TestSellerIsNamedUnlessAnonymous

Every viewer sees a named seller's name and no anonymous one's; the
seller's own page shows its name with the choice.
================
*/
func TestSellerIsNamedUnlessAnonymous(t *testing.T) {
	store := &fakeServiceStore{open: []domain.MarketListing{
		{ID: 1, Division: "global-official", Currency: domain.MarketGold, SellerName: "Kekw",
			Item: domain.InventoryRow{Codename: "ITEM_CH_SWORD_01_A"}},
		{ID: 2, Division: "global-official", Currency: domain.MarketGold, SellerName: "Lulu", Anonymous: true,
			Item: domain.InventoryRow{Codename: "ITEM_CH_SWORD_01_A"}},
	}}
	s := testService(store)
	page, err := s.Search(publicstats.MarketSearch{Page: 1})
	if err != nil {
		t.Fatal(err)
	}
	sellers := map[int64]string{}
	for _, view := range page.Listings {
		sellers[view.ID] = view.Seller
		if view.Anonymous {
			t.Fatalf("a search shows the anonymous choice: %+v", view)
		}
	}
	if sellers[1] != "Kekw" || sellers[2] != "" {
		t.Fatalf("sellers = %v", sellers)
	}
	own := s.ownListingView(store.open[1])
	name, anonymous := own.Seller, own.Anonymous
	if name != "Lulu" || !anonymous {
		t.Fatalf("own seller = %q %v", name, anonymous)
	}
}

/*
================
TestWebsiteOffersNeedAnOfflineCharacter

A website offer and a website accept write a character's record only
through the offline gate: an online offering character refuses both,
and an offline one is judged with its free bag slot.
================
*/
func TestWebsiteOffersNeedAnOfflineCharacter(t *testing.T) {
	store := &fakeServiceStore{byID: map[int64]domain.MarketListing{
		1: {ID: 1, Division: "global-official", Currency: domain.MarketGold, Price: 900},
	}, offers: map[int64]domain.MarketOffer{
		5: {ID: 5, ListingID: 1, BuyerAccount: "alpha", BuyerChar: 1},
		6: {ID: 6, ListingID: 1, BuyerAccount: "alpha", BuyerChar: 2},
	}}
	s := testService(store)
	s.FreeBagSlot = func(c *domain.Character) int64 { return 40 + c.ID }

	if _, err := s.MakeOffer("alpha", publicstats.MarketOfferBody{Listing: 1, Price: 500, Character: "Kekw"}); !errors.Is(err, publicstats.ErrMarketOnline) {
		t.Fatalf("an online character's website offer: %v", err)
	}
	if _, err := s.MakeOffer("alpha", publicstats.MarketOfferBody{Listing: 1, Price: 500, Character: "Nobody"}); !errors.Is(err, publicstats.ErrNotOwned) {
		t.Fatalf("an offer for another account's character: %v", err)
	}
	if _, err := s.MakeOffer("alpha", publicstats.MarketOfferBody{Listing: 1, Price: 500, Character: "Lulu"}); err != nil ||
		len(store.offered) != 1 || store.offered[0].To != domain.MarketToWarehouse || store.offered[0].Buyer.Name != "Lulu" {
		t.Fatalf("an offline offer = %v, %+v", err, store.offered)
	}
	if _, err := s.AcceptOffer("beta", 5); !errors.Is(err, publicstats.ErrMarketOnline) || len(store.accepted) != 0 {
		t.Fatalf("accepting an online buyer's offer from the website: %v", err)
	}
	if _, err := s.AcceptOffer("beta", 6); err != nil || len(store.accepted) != 1 || store.accepted[0].BagSlot != 42 ||
		store.accepted[0].Buyer.Name != "Lulu" || store.accepted[0].SellerAccount != "beta" {
		t.Fatalf("an offline buyer's accept = %v, %+v", err, store.accepted)
	}
}
