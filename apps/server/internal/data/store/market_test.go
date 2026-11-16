/*
===========================================================================

market_test.go - the market escrow: every door moves the item and the
money whole or not at all

The invariant under test is that a listed item is in exactly one place
(the seller's bag or warehouse, an open or held listing, or the buyer's
warehouse) after every door, including a door whose commit fails.

===========================================================================
*/
package store

import (
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
)

var testMarketRules = domain.MarketRules{FeePercent: 1, DurationMs: 24 * 60 * 60 * 1000, OpenCap: 2}

const (
	marketSeller = "market-seller"
	marketBuyer  = "market-buyer"
	marketSword  = "ITEM_EU_SWORD_01_A"
)

func admitAll(domain.InventoryRow) error { return nil }

/*
================
marketFixture

A store with a seller and a buyer character, the seller's sword moved
from the bag into warehouse slot 0.
================
*/
func marketFixture(t *testing.T) (*Store, *testClock, *domain.Character, *domain.Character) {
	t.Helper()
	clock := newTestClock()
	s := openTest(t, t.TempDir(), clock)
	seller := seededCharacter()
	buyer := seededCharacter()
	buyer.Name = "MarketBuyer"
	buyer.MissionInventory = nil
	if err := s.CreateCharacter(testDivision, marketSeller, seller); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateCharacter(testDivision, marketBuyer, buyer); err != nil {
		t.Fatal(err)
	}
	if _, err := s.TransactStorage(seller, func(next *domain.Character, storage *domain.AccountStorage) error {
		row := next.MissionInventory[0]
		row.Slot = 0
		storage.Rows = append(storage.Rows, row)
		next.MissionInventory = next.MissionInventory[1:]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return s, clock, seller, buyer
}

/*
================
swordCount

Where the sword is: every warehouse, bag and live listing that holds it.
================
*/
func swordCount(t *testing.T, s *Store, characters ...*domain.Character) int {
	t.Helper()
	count := 0
	for _, account := range []string{marketSeller, marketBuyer} {
		storage, err := readAccountStorage(s.syncDBLocked(), account)
		if err != nil {
			t.Fatal(err)
		}
		for _, row := range storage.Rows {
			if row.Codename == marketSword {
				count++
			}
		}
	}
	for _, c := range characters {
		for _, row := range c.MissionInventory {
			if row.Codename == marketSword {
				count++
			}
		}
	}
	var live int
	if err := s.syncDBLocked().QueryRow("SELECT COUNT(*) FROM market_listings WHERE state IN ('open', 'held')").Scan(&live); err != nil {
		t.Fatal(err)
	}
	return count + live
}

/*
================
listSword
================
*/
func listSword(t *testing.T, s *Store, currency string, price int64) domain.MarketListing {
	t.Helper()
	listing, err := s.MarketList(domain.MarketListRequest{Division: testDivision, Account: marketSeller, Slot: 0,
		Currency: currency, Price: price, Admit: admitAll, Rules: testMarketRules})
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

/*
================
TestMarketListTakesTheItemOutOfTheWarehouse
================
*/
func TestMarketListTakesTheItemOutOfTheWarehouse(t *testing.T) {
	t.Parallel()
	s, _, seller, buyer := marketFixture(t)
	request := domain.MarketListRequest{Division: testDivision, Account: marketSeller, Slot: 0, Currency: domain.MarketSilk,
		Price: 500, Rules: testMarketRules}

	request.Admit = func(domain.InventoryRow) error { return errors.New("bound") }
	if _, err := s.MarketList(request); !errors.Is(err, domain.ErrMarketNotTradeable) {
		t.Fatalf("an untradeable item listed: %v", err)
	}
	request.Admit = admitAll
	request.Price = 0
	if _, err := s.MarketList(request); !errors.Is(err, domain.ErrMarketPrice) {
		t.Fatalf("a zero price listed: %v", err)
	}
	request.Price = 500
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.MarketList(request); err == nil {
		t.Fatal("a failed commit listed")
	}
	s.FailCommits(nil)
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after refusals the sword is in %d places", n)
	}

	listing, err := s.MarketList(request)
	if err != nil || listing.State != domain.MarketOpen || listing.Item.Codename != marketSword || listing.Version != 1 {
		t.Fatalf("list = %+v, %v", listing, err)
	}
	storage, _ := readAccountStorage(s.syncDBLocked(), marketSeller)
	if len(storage.Rows) != 0 {
		t.Fatalf("the warehouse still holds %d rows", len(storage.Rows))
	}
	if _, err := s.MarketList(request); !errors.Is(err, domain.ErrMarketNoItem) {
		t.Fatalf("the same slot listed twice: %v", err)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after listing the sword is in %d places", n)
	}
}

/*
================
TestMarketListFromTheBag
================
*/
func TestMarketListFromTheBag(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	seller := seededCharacter()
	if err := s.CreateCharacter(testDivision, marketSeller, seller); err != nil {
		t.Fatal(err)
	}
	request := domain.MarketListRequest{Division: testDivision, Account: marketSeller, Character: seller, Slot: 13,
		Currency: domain.MarketGold, Price: 1000, Admit: admitAll, Rules: testMarketRules}
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.MarketList(request); err == nil || len(seller.MissionInventory) != 1 {
		t.Fatalf("a failed commit took the item: %v, %d rows", err, len(seller.MissionInventory))
	}
	s.FailCommits(nil)
	listing, err := s.MarketList(request)
	if err != nil || listing.SellerChar != seller.ID || len(seller.MissionInventory) != 0 {
		t.Fatalf("bag list = %+v, %v, %d rows left", listing, err, len(seller.MissionInventory))
	}
	if n := swordCount(t, s, seller); n != 1 {
		t.Fatalf("after a bag listing the sword is in %d places", n)
	}
}

/*
================
TestMarketListingCap
================
*/
func TestMarketListingCap(t *testing.T) {
	t.Parallel()
	s := openTest(t, t.TempDir(), newTestClock())
	seller := seededCharacter()
	seller.MissionInventory = append(seller.MissionInventory,
		domain.InventoryRow{Slot: 14, RefObjID: 3117, Codename: marketSword, VarianceBits: "0", StackCount: 1},
		domain.InventoryRow{Slot: 15, RefObjID: 3117, Codename: marketSword, VarianceBits: "0", StackCount: 1})
	if err := s.CreateCharacter(testDivision, marketSeller, seller); err != nil {
		t.Fatal(err)
	}
	for i, slot := range []int64{13, 14, 15} {
		_, err := s.MarketList(domain.MarketListRequest{Division: testDivision, Account: marketSeller, Character: seller, Slot: slot,
			Currency: domain.MarketSilk, Price: 10, Admit: admitAll, Rules: testMarketRules})
		if i < testMarketRules.OpenCap && err != nil {
			t.Fatalf("listing %d refused: %v", i, err)
		}
		if i == testMarketRules.OpenCap && !errors.Is(err, domain.ErrMarketCap) {
			t.Fatalf("listing past the cap: %v", err)
		}
	}
}

/*
================
TestMarketBuyWithSilk
================
*/
func TestMarketBuyWithSilk(t *testing.T) {
	t.Parallel()
	s, _, seller, buyer := marketFixture(t)
	listing := listSword(t, s, domain.MarketSilk, 500)
	buy := domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version, BuyerAccount: marketBuyer, Rules: testMarketRules}

	if _, err := s.MarketBuy(buy); !errors.Is(err, domain.ErrMarketFunds) {
		t.Fatalf("a buyer with no Silk bought: %v", err)
	}
	if _, err := s.GrantSilk(marketBuyer, 800); err != nil {
		t.Fatal(err)
	}
	own := buy
	own.BuyerAccount = marketSeller
	if _, err := s.MarketBuy(own); !errors.Is(err, domain.ErrMarketOwnListing) {
		t.Fatalf("a seller bought their own listing: %v", err)
	}
	stale := buy
	stale.Version = listing.Version + 1
	if _, err := s.MarketBuy(stale); !errors.Is(err, domain.ErrMarketChanged) {
		t.Fatalf("a stale version bought: %v", err)
	}
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.MarketBuy(buy); err == nil {
		t.Fatal("a failed commit bought")
	}
	s.FailCommits(nil)
	if wallet, _ := readMallBalance(s.syncDBLocked(), marketBuyer); wallet.Silk != 800 {
		t.Fatalf("a refused buy charged the buyer: %d left", wallet.Silk)
	}

	sale, err := s.MarketBuy(buy)
	if err != nil || sale.Fee != 5 || sale.Price != 500 {
		t.Fatalf("buy = %+v, %v", sale, err)
	}
	buyerWallet, _ := readMallBalance(s.syncDBLocked(), marketBuyer)
	sellerWallet, _ := readMallBalance(s.syncDBLocked(), marketSeller)
	if buyerWallet.Silk != 300 || sellerWallet.Silk != 495 {
		t.Fatalf("wallets after the sale: buyer %d, seller %d", buyerWallet.Silk, sellerWallet.Silk)
	}
	storage, _ := readAccountStorage(s.syncDBLocked(), marketBuyer)
	if len(storage.Rows) != 1 || storage.Rows[0].Codename != marketSword {
		t.Fatalf("the buyer's warehouse = %+v", storage.Rows)
	}
	if _, err := s.MarketBuy(buy); !errors.Is(err, domain.ErrMarketChanged) {
		t.Fatalf("a sold listing sold again: %v", err)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after the sale the sword is in %d places", n)
	}
	purchases, err := s.MarketPurchases(marketBuyer, 10)
	if err != nil || len(purchases) != 1 || purchases[0].ListingID != listing.ID {
		t.Fatalf("purchases = %+v, %v", purchases, err)
	}
}

/*
================
TestMarketBuyWithGold

The buyer's character pays; the seller's warehouse gets the price less
the fee, and the ledger names the move.
================
*/
func TestMarketBuyWithGold(t *testing.T) {
	t.Parallel()
	s, _, seller, buyer := marketFixture(t)
	listing := listSword(t, s, domain.MarketGold, 1000)
	buy := domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version, BuyerAccount: marketBuyer, Rules: testMarketRules}

	if _, err := s.MarketBuy(buy); !errors.Is(err, domain.ErrMarketUnavailable) {
		t.Fatalf("a gold buy with no paying character: %v", err)
	}
	buy.Payer = seller
	if _, err := s.MarketBuy(buy); !errors.Is(err, domain.ErrMarketUnavailable) {
		t.Fatalf("another account's character paid: %v", err)
	}
	buy.Payer = buyer
	sale, err := s.MarketBuy(buy)
	if err != nil || sale.BuyerChar != buyer.ID || sale.Fee != 10 {
		t.Fatalf("gold buy = %+v, %v", sale, err)
	}
	if *buyer.Gold != 12345-1000 {
		t.Fatalf("the buyer kept %d gold", *buyer.Gold)
	}
	storage, _ := readAccountStorage(s.syncDBLocked(), marketSeller)
	if storage.Gold != 990 {
		t.Fatalf("the seller's warehouse gold = %d, want 990", storage.Gold)
	}
	var reason string
	if err := s.syncDBLocked().QueryRow("SELECT reason FROM economy_ledger WHERE door = 'market-buy'").Scan(&reason); err != nil || reason != "market_sale" {
		t.Fatalf("ledger reason = %q, %v", reason, err)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after the gold sale the sword is in %d places", n)
	}
}

/*
================
TestMarketBuyRefusesBeyondTheBuyersGold
================
*/
func TestMarketBuyRefusesBeyondTheBuyersGold(t *testing.T) {
	t.Parallel()
	s, _, _, buyer := marketFixture(t)
	listing := listSword(t, s, domain.MarketGold, 20000)
	_, err := s.MarketBuy(domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version, BuyerAccount: marketBuyer,
		Payer: buyer, Rules: testMarketRules})
	if !errors.Is(err, domain.ErrMarketFunds) || *buyer.Gold != 12345 {
		t.Fatalf("buy past the purse = %v, gold %d", err, *buyer.Gold)
	}
}

/*
================
TestMarketExpiryReturnsHoldsAndTakesBack
================
*/
func TestMarketExpiryReturnsHoldsAndTakesBack(t *testing.T) {
	t.Parallel()
	s, clock, seller, buyer := marketFixture(t)
	listing := listSword(t, s, domain.MarketSilk, 50)

	if n, err := s.ExpireMarketListings(10); err != nil || n != 0 {
		t.Fatalf("an unexpired listing returned: %d, %v", n, err)
	}
	// Fill the seller's warehouse, so the expiry has nowhere to put it.
	if _, err := s.TransactStorage(seller, func(_ *domain.Character, storage *domain.AccountStorage) error {
		for slot := int64(0); slot < storage.Capacity; slot++ {
			storage.Rows = append(storage.Rows, domain.InventoryRow{Slot: slot, RefObjID: 62, Codename: "ITEM_ETC_GOLD_01", VarianceBits: "0", StackCount: 1})
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(25 * time.Hour)
	if _, err := s.MarketBuy(domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version, BuyerAccount: marketBuyer, Rules: testMarketRules}); !errors.Is(err, domain.ErrMarketChanged) {
		t.Fatalf("an expired listing sold: %v", err)
	}
	if n, err := s.ExpireMarketListings(10); err != nil || n != 1 {
		t.Fatalf("expiry = %d, %v", n, err)
	}
	held, _ := s.MarketListingByID(listing.ID)
	if held.State != domain.MarketHeld {
		t.Fatalf("a full warehouse left the listing %s, want held", held.State)
	}
	if _, err := s.MarketTakeBack(marketSeller, listing.ID); !errors.Is(err, domain.ErrMarketWarehouseFull) {
		t.Fatalf("take back into a full warehouse: %v", err)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("while held the sword is in %d places", n)
	}
	if _, err := s.TransactStorage(seller, func(_ *domain.Character, storage *domain.AccountStorage) error {
		storage.Rows = storage.Rows[1:]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarketTakeBack(marketBuyer, listing.ID); !errors.Is(err, domain.ErrMarketNotFound) {
		t.Fatalf("another account took it back: %v", err)
	}
	back, err := s.MarketTakeBack(marketSeller, listing.ID)
	if err != nil || back.State != domain.MarketReturned {
		t.Fatalf("take back = %+v, %v", back, err)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after taking it back the sword is in %d places", n)
	}
}

/*
================
TestMarketCancel
================
*/
func TestMarketCancel(t *testing.T) {
	t.Parallel()
	s, _, seller, buyer := marketFixture(t)
	listing := listSword(t, s, domain.MarketSilk, 50)
	if _, err := s.MarketCancel(marketBuyer, listing.ID); !errors.Is(err, domain.ErrMarketNotFound) {
		t.Fatalf("another account cancelled: %v", err)
	}
	s.FailCommits(errors.New("disk failure"))
	if _, err := s.MarketCancel(marketSeller, listing.ID); err == nil {
		t.Fatal("a failed commit cancelled")
	}
	s.FailCommits(nil)
	back, err := s.MarketCancel(marketSeller, listing.ID)
	if err != nil || back.State != domain.MarketReturned {
		t.Fatalf("cancel = %+v, %v", back, err)
	}
	if _, err := s.MarketCancel(marketSeller, listing.ID); !errors.Is(err, domain.ErrMarketChanged) {
		t.Fatalf("a returned listing cancelled again: %v", err)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after cancelling the sword is in %d places", n)
	}
	mine, err := s.MarketAccountListings(marketSeller, 10)
	if err != nil || len(mine) != 1 || mine[0].State != domain.MarketReturned {
		t.Fatalf("own listings = %+v, %v", mine, err)
	}
}

/*
================
TestMarketSearch
================
*/
func TestMarketSearch(t *testing.T) {
	t.Parallel()
	s, _, _, _ := marketFixture(t)
	listing := listSword(t, s, domain.MarketGold, 75)
	all, err := s.MarketOpenListings(domain.MarketQuery{Division: testDivision, Limit: 15})
	if err != nil || len(all) != 1 || all[0].ID != listing.ID {
		t.Fatalf("search = %+v, %v", all, err)
	}
	none, _ := s.MarketOpenListings(domain.MarketQuery{Division: testDivision, Currency: domain.MarketSilk, Limit: 15})
	named, _ := s.MarketOpenListings(domain.MarketQuery{Division: testDivision, Codenames: []string{marketSword}, Limit: 15})
	other, _ := s.MarketOpenListings(domain.MarketQuery{Division: testDivision, Codenames: []string{}, Limit: 15})
	if len(none) != 0 || len(named) != 1 || len(other) != 0 {
		t.Fatalf("filters: silk %d, named %d, empty %d", len(none), len(named), len(other))
	}
}

/*
================
TestMarketBuyNeverSpendsGiftSilk

A Silk listing is paid from purchased Silk only: gift Silk (the referral
reward, #3671) never buys a listing and so never reaches a seller. Written
by opensro-web2's reading of eae40ca29, which mutated debitSilkTx to fall
back to gift Silk and found no test failed.
================
*/
func TestMarketBuyNeverSpendsGiftSilk(t *testing.T) {
	t.Parallel()
	s, _, _, _ := marketFixture(t)
	listing := listSword(t, s, domain.MarketSilk, 500)
	if _, err := s.syncDBLocked().Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, 0, 800, 0)`, marketBuyer); err != nil {
		t.Fatal(err)
	}
	buy := domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version, BuyerAccount: marketBuyer, Rules: testMarketRules}
	if _, err := s.MarketBuy(buy); !errors.Is(err, domain.ErrMarketFunds) {
		t.Fatalf("gift Silk paid a listing: %v", err)
	}
	buyerWallet, _ := readMallBalance(s.syncDBLocked(), marketBuyer)
	sellerWallet, _ := readMallBalance(s.syncDBLocked(), marketSeller)
	if buyerWallet.GiftSilk != 800 || buyerWallet.Silk != 0 || sellerWallet.Silk != 0 {
		t.Fatalf("wallets moved: buyer %+v, seller %+v", buyerWallet, sellerWallet)
	}
}
