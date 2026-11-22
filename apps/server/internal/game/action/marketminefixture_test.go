/*
===========================================================================

marketminefixture_test.go - the largest "mine" answer, pinned for both ports

The wire's row limit (game/market WireRowLimit) has one owner on each
side; this fixture is how the two are pinned together (#4111). The
answer below is built through the real market.Service from an account
with more rows than the limit in every list, driven through
HandleMarket like a player's open of the window, and the client's
market suite decodes the pinned bytes whole: raise the server's limit
and the regenerated fixture stops decoding on the client until its
limit follows.

Regenerate:

	UPDATE_MARKET_MINE_FIXTURE=1 go test ./internal/game/action -run TestMarketMineFixturePinned

===========================================================================
*/
package action

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/market"
)

/*
================
fixtureMarketStore

The account behind the fixture: 20 open and 60 sold listings of one
character, 60 purchases, 5 open and 58 finished offers out - more rows
than the wire limit in every list the mine answer carries.
================
*/
type fixtureMarketStore struct {
	listings  []domain.MarketListing
	purchases []domain.MarketSale
	offersIn  []domain.MarketOffer
	offersOut []domain.MarketOffer
	byID      map[int64]domain.MarketListing
}

func (f *fixtureMarketStore) MarketList(domain.MarketListRequest) (domain.MarketListing, error) {
	return domain.MarketListing{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketBuy(domain.MarketBuyRequest) (domain.MarketSale, error) {
	return domain.MarketSale{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketCancel(string, int64) (domain.MarketListing, error) {
	return domain.MarketListing{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketTakeBack(string, int64) (domain.MarketListing, error) {
	return domain.MarketListing{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketListingByID(id int64) (domain.MarketListing, error) {
	if listing, ok := f.byID[id]; ok {
		return listing, nil
	}
	return domain.MarketListing{}, domain.ErrMarketNotFound
}

func (f *fixtureMarketStore) MarketOpenListings(domain.MarketQuery) ([]domain.MarketListing, error) {
	return nil, nil
}

func (f *fixtureMarketStore) MarketAccountListings(string, int) ([]domain.MarketListing, error) {
	return f.listings, nil
}

func (f *fixtureMarketStore) MarketPurchases(string, int) ([]domain.MarketSale, error) {
	return f.purchases, nil
}

func (f *fixtureMarketStore) MarketWarehouse(string) (domain.AccountStorage, error) {
	return domain.AccountStorage{}, nil
}

func (f *fixtureMarketStore) MarketWallet(string) (domain.MallBalance, error) {
	return domain.MallBalance{Silk: 40}, nil
}

func (f *fixtureMarketStore) MarketReclaim(domain.MarketReclaimRequest) (domain.MarketListing, error) {
	return domain.MarketListing{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketMakeOffer(domain.MarketOfferRequest) (domain.MarketOffer, error) {
	return domain.MarketOffer{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketAcceptOffer(domain.MarketAcceptRequest) (domain.MarketSale, domain.MarketOffer, error) {
	return domain.MarketSale{}, domain.MarketOffer{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketRejectOffer(string, int64) (domain.MarketOffer, error) {
	return domain.MarketOffer{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketWithdrawOffer(string, int64) (domain.MarketOffer, error) {
	return domain.MarketOffer{}, domain.ErrMarketUnavailable
}

func (f *fixtureMarketStore) MarketOffers(string, int) ([]domain.MarketOffer, []domain.MarketOffer, error) {
	return f.offersIn, f.offersOut, nil
}

func (f *fixtureMarketStore) MarketOfferByID(int64) (domain.MarketOffer, error) {
	return domain.MarketOffer{}, domain.ErrMarketOfferGone
}

func (f *fixtureMarketStore) MarketOwedCharacters() (map[int64]bool, error) { return nil, nil }

func (f *fixtureMarketStore) MarketClaimProceeds(*domain.Character) ([]domain.MarketSale, error) {
	return nil, nil
}

/*
================
TestMarketMineFixturePinned

The largest mine answer the server can send, byte-pinned: the account
above, through the real service and lane, bounded to the wire limit
in every list.
================
*/
func TestMarketMineFixturePinned(t *testing.T) {
	rt, c, _ := marketFixture()
	row := testCharacter().MissionInventory[0]
	listing := func(id int64, state string) domain.MarketListing {
		return domain.MarketListing{ID: id, Division: testDivision, SellerAccount: "alpha", SellerChar: c.ID,
			SellerName: "asd2", Currency: domain.MarketGold, Price: 900, State: state, ListedMs: id, Item: row}
	}
	store := &fixtureMarketStore{
		byID: map[int64]domain.MarketListing{
			1: listing(1, domain.MarketOpen),
			30: {ID: 30, Division: testDivision, SellerAccount: "beta", SellerChar: 5,
				Currency: domain.MarketGold, Price: 900, State: domain.MarketOpen, Item: row},
		},
	}
	for id := int64(160); id >= 101; id-- {
		store.offersIn = append(store.offersIn, domain.MarketOffer{ID: id, ListingID: 1, BuyerChar: 7,
			BuyerName: "Buyer", Price: 700, To: domain.MarketToBag, State: domain.MarketOfferOpen, AtMs: id})
	}
	// The store's shape, newest first (see the service test of the bound).
	for id := int64(80); id >= 71; id-- {
		store.listings = append(store.listings, listing(id, domain.MarketSold))
	}
	for id := int64(20); id >= 1; id-- {
		store.listings = append(store.listings, listing(id, domain.MarketOpen))
	}
	for id := int64(70); id >= 21; id-- {
		store.listings = append(store.listings, listing(id, domain.MarketSold))
	}
	for id := int64(63); id >= 59; id-- {
		store.offersOut = append(store.offersOut, domain.MarketOffer{ID: id, ListingID: 30, BuyerChar: c.ID,
			BuyerName: "asd2", Price: 700, To: domain.MarketToBag, State: domain.MarketOfferOpen, AtMs: id})
	}
	for id := int64(58); id >= 1; id-- {
		store.offersOut = append(store.offersOut, domain.MarketOffer{ID: id, ListingID: 30, BuyerChar: c.ID,
			BuyerName: "asd2", Price: 700, To: domain.MarketToBag, State: domain.MarketOfferRejected, AtMs: id})
	}
	for id := int64(60); id >= 1; id-- {
		store.purchases = append(store.purchases, domain.MarketSale{ListingID: id, BuyerChar: c.ID,
			Currency: domain.MarketSilk, Price: 50, AtMs: id, To: domain.MarketToBag})
	}
	rt.Market = &market.Service{
		Store:    store,
		Division: testDivision,
		Item: func(codename string) (market.ItemInfo, bool) {
			if codename == row.Codename {
				return market.ItemInfo{Name: "Iron Sword", TypeIDs: [4]int64{3, 1, 6, 2}, Degree: 1}, true
			}
			return market.ItemInfo{}, false
		},
		Tradeable: func(domain.InventoryRow) bool { return true },
		Characters: func(string) []*domain.Character {
			return []*domain.Character{{ID: c.ID, AccountID: "alpha", Name: "asd2"}}
		},
		Online: func(id int64) bool { return id == c.ID },
	}

	result := rt.HandleMarket(testDivision, c, []byte(`{"op":"mine"}`))
	var payload []byte
	for _, frame := range result.Frames {
		if frame.Opcode == opMarket {
			payload = frame.Payload
		}
	}
	if payload == nil {
		t.Fatalf("no mine answer in %v", result)
	}
	var answer marketAnswer
	if err := json.Unmarshal(payload, &answer); err != nil {
		t.Fatal(err)
	}
	if answer.Mine == nil {
		t.Fatalf("answer carries no mine page: %s", payload)
	}
	for name, count := range map[string]int{
		"listings": len(answer.Mine.Listings), "offersIn": len(answer.Mine.OffersIn),
		"offersOut": len(answer.Mine.OffersOut), "purchases": len(answer.Mine.Purchases),
	} {
		if count != market.WireRowLimit {
			t.Fatalf("%s = %d rows, want the wire limit %d", name, count, market.WireRowLimit)
		}
	}

	path := filepath.Join("testdata", "market_mine_max_fixture.json")
	if os.Getenv("UPDATE_MARKET_MINE_FIXTURE") == "1" {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatalf("mkdir testdata: %v", err)
		}
		if err := os.WriteFile(path, append(payload, '\n'), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
		return
	}
	pinned, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("fixture missing (%v) - regenerate with UPDATE_MARKET_MINE_FIXTURE=1", err)
	}
	if string(pinned) != string(append(payload, '\n')) {
		t.Fatalf("market mine fixture drifted:\n got %s\nwant %s\nregenerate with UPDATE_MARKET_MINE_FIXTURE=1",
			payload, pinned)
	}
}
