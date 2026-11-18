/*
===========================================================================

market_rules_test.go - the owner's 2026-11-19 market rules on the store:
gold owed to the listing character, delivery into the bag, the seller's
name, offers, and the migration of an older market file

Every door keeps the escrow invariant of market_test.go: the item is in
exactly one place, and the money moves whole or not at all.

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"testing"

	"opensro.online/server/internal/domain"
)

// testOfferRules adds an offer cap to the listing rules.
var testOfferRules = domain.MarketRules{FeePercent: 1, DurationMs: 24 * 60 * 60 * 1000, OpenCap: 2, OfferCap: 2}

/*
================
bagMarketFixture

A seller whose sword is in its bag (slot 13) and a buyer with an empty
bag, both with 12345 gold.
================
*/
func bagMarketFixture(t *testing.T) (*Store, *domain.Character, *domain.Character) {
	t.Helper()
	s := openTest(t, t.TempDir(), newTestClock())
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
	return s, seller, buyer
}

/*
================
listFromBag
================
*/
func listFromBag(t *testing.T, s *Store, seller *domain.Character, currency string, price int64, anonymous bool) domain.MarketListing {
	t.Helper()
	listing, err := s.MarketList(domain.MarketListRequest{Division: testDivision, Account: marketSeller, Character: seller,
		Slot: 13, Currency: currency, Price: price, Anonymous: anonymous, Admit: admitAll, Rules: testOfferRules})
	if err != nil {
		t.Fatal(err)
	}
	return listing
}

/*
================
fillWarehouse
================
*/
func fillWarehouse(t *testing.T, s *Store, c *domain.Character) {
	t.Helper()
	if _, err := s.TransactStorage(c, func(_ *domain.Character, storage *domain.AccountStorage) error {
		for slot := int64(0); slot < storage.Capacity; slot++ {
			if !slotTaken(storage.Rows, slot) {
				storage.Rows = append(storage.Rows, domain.InventoryRow{Slot: slot, RefObjID: 62, Codename: "ITEM_ETC_GOLD_01",
					VarianceBits: "0", StackCount: 1})
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestMarketGoldSaleOwesTheListingCharacter

A gold sale of a bag listing writes no warehouse gold: the sale is owed
to the listing character until its lane claims it, once, with a ledger
row; the listing names its seller and keeps the anonymous choice.
================
*/
func TestMarketGoldSaleOwesTheListingCharacter(t *testing.T) {
	t.Parallel()
	s, seller, buyer := bagMarketFixture(t)
	listing := listFromBag(t, s, seller, domain.MarketGold, 1000, true)
	if listing.SellerName != seller.Name || !listing.Anonymous {
		t.Fatalf("listing seller = %q anonymous %v", listing.SellerName, listing.Anonymous)
	}
	stored, err := s.MarketListingByID(listing.ID)
	if err != nil || stored.SellerName != seller.Name || !stored.Anonymous {
		t.Fatalf("stored listing = %+v, %v", stored, err)
	}
	sale, err := s.MarketBuy(domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version,
		BuyerAccount: marketBuyer, Payer: buyer, Rules: testOfferRules})
	if err != nil || !sale.Unpaid || sale.SellerChar != seller.ID || sale.To != domain.MarketToWarehouse {
		t.Fatalf("sale = %+v, %v", sale, err)
	}
	if storage, _ := readAccountStorage(s.syncDBLocked(), marketSeller); storage.Gold != 0 {
		t.Fatalf("the seller's warehouse got %d gold", storage.Gold)
	}
	owed, err := s.MarketOwedCharacters()
	if err != nil || !owed[seller.ID] || len(owed) != 1 {
		t.Fatalf("owed = %v, %v", owed, err)
	}

	s.FailCommits(errors.New("disk failure"))
	if _, err := s.MarketClaimProceeds(seller); err == nil || *seller.Gold != 12345 {
		t.Fatalf("a failed claim paid: %v, gold %d", err, *seller.Gold)
	}
	s.FailCommits(nil)
	paid, err := s.MarketClaimProceeds(seller)
	if err != nil || len(paid) != 1 || *seller.Gold != 12345+990 {
		t.Fatalf("claim = %+v, %v, gold %d", paid, err, *seller.Gold)
	}
	if again, err := s.MarketClaimProceeds(seller); err != nil || len(again) != 0 || *seller.Gold != 12345+990 {
		t.Fatalf("a second claim paid again: %+v, %v", again, err)
	}
	if owed, _ := s.MarketOwedCharacters(); len(owed) != 0 {
		t.Fatalf("still owed after the claim: %v", owed)
	}
	var reason string
	if err := s.syncDBLocked().QueryRow("SELECT reason FROM economy_ledger WHERE door = 'market-proceeds'").Scan(&reason); err != nil ||
		reason != "market_sale" {
		t.Fatalf("proceeds ledger reason = %q, %v", reason, err)
	}
	if _, err := s.MarketClaimProceeds(buyer); err != nil {
		t.Fatalf("a claim with nothing owed: %v", err)
	}
}

/*
================
TestMarketBuyIntoTheBag

A bag buy puts the item in the slot the lane chose, on the record and on
disk, and one transaction pays and delivers; a taken slot refuses with
nothing moved.
================
*/
func TestMarketBuyIntoTheBag(t *testing.T) {
	t.Parallel()
	s, seller, buyer := bagMarketFixture(t)
	listing := listFromBag(t, s, seller, domain.MarketGold, 1000, false)
	buy := domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version, BuyerAccount: marketBuyer, Payer: buyer,
		To: domain.MarketToBag, Receiver: buyer, BagSlot: 20, Rules: testOfferRules}

	buyer.MissionInventory = []domain.InventoryRow{{Slot: 20, RefObjID: 62, Codename: "ITEM_ETC_GOLD_01", VarianceBits: "0", StackCount: 1}}
	if _, err := s.MarketBuy(buy); !errors.Is(err, domain.ErrMarketBagFull) || *buyer.Gold != 12345 {
		t.Fatalf("a taken slot: %v, gold %d", err, *buyer.Gold)
	}
	buyer.MissionInventory = nil
	other := buy
	other.Receiver = seller
	if _, err := s.MarketBuy(other); !errors.Is(err, domain.ErrMarketUnavailable) {
		t.Fatalf("another account's bag received: %v", err)
	}

	sale, err := s.MarketBuy(buy)
	if err != nil || sale.To != domain.MarketToBag || sale.Slot != 20 {
		t.Fatalf("bag buy = %+v, %v", sale, err)
	}
	if len(buyer.MissionInventory) != 1 || buyer.MissionInventory[0].Slot != 20 || *buyer.Gold != 12345-1000 {
		t.Fatalf("the buyer's record = %+v, gold %d", buyer.MissionInventory, *buyer.Gold)
	}
	if storage, _ := readAccountStorage(s.syncDBLocked(), marketBuyer); len(storage.Rows) != 0 {
		t.Fatalf("the bag buy also filled the warehouse: %+v", storage.Rows)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after the bag buy the sword is in %d places", n)
	}
	reopened := s.characterByIDLocked(testDivision, buyer.ID)
	if reopened == nil || len(reopened.MissionInventory) != 1 {
		t.Fatalf("the store's record = %+v", reopened)
	}
}

/*
================
TestMarketOffers

An offer must be below the price, payable now, the account's only open
one on the listing and inside the cap. An accept sells at the offer's
price, closes the listing's other offers, and falls back to the other
place when the buyer's pick is full; both full leaves the offer open.
================
*/
func TestMarketOffers(t *testing.T) {
	t.Parallel()
	s, seller, buyer := bagMarketFixture(t)
	listing := listFromBag(t, s, seller, domain.MarketGold, 5000, false)
	offer := domain.MarketOfferRequest{ListingID: listing.ID, BuyerAccount: marketBuyer, Buyer: buyer, Price: 5000,
		To: domain.MarketToBag, Rules: testOfferRules}

	if _, err := s.MarketMakeOffer(offer); !errors.Is(err, domain.ErrMarketOfferPrice) {
		t.Fatalf("an offer at the price: %v", err)
	}
	offer.Price = 4000
	own := offer
	own.BuyerAccount, own.Buyer = marketSeller, seller
	if _, err := s.MarketMakeOffer(own); !errors.Is(err, domain.ErrMarketOwnListing) {
		t.Fatalf("an offer on one's own listing: %v", err)
	}
	poor := offer
	poor.Price = 4999
	*buyer.Gold = 100
	if _, err := s.MarketMakeOffer(poor); !errors.Is(err, domain.ErrMarketFunds) {
		t.Fatalf("an offer the buyer cannot pay: %v", err)
	}
	*buyer.Gold = 12345
	made, err := s.MarketMakeOffer(offer)
	if err != nil || made.State != domain.MarketOfferOpen || made.BuyerName != buyer.Name {
		t.Fatalf("offer = %+v, %v", made, err)
	}
	if _, err := s.MarketMakeOffer(offer); !errors.Is(err, domain.ErrMarketOfferOpen) {
		t.Fatalf("a second open offer on one listing: %v", err)
	}
	if _, err := s.MarketWithdrawOffer(marketSeller, made.ID); !errors.Is(err, domain.ErrMarketNotFound) {
		t.Fatalf("the seller withdrew the buyer's offer: %v", err)
	}
	if _, err := s.MarketRejectOffer(marketBuyer, made.ID); !errors.Is(err, domain.ErrMarketNotFound) {
		t.Fatalf("the buyer rejected its own offer: %v", err)
	}
	incoming, outgoing, err := s.MarketOffers(marketSeller, 10)
	if err != nil || len(incoming) != 1 || len(outgoing) != 0 {
		t.Fatalf("seller offers = %+v / %+v, %v", incoming, outgoing, err)
	}

	accept := domain.MarketAcceptRequest{OfferID: made.ID, SellerAccount: marketSeller, Buyer: buyer, BagSlot: -1,
		Rules: testOfferRules}
	fillWarehouse(t, s, buyer)
	if _, _, err := s.MarketAcceptOffer(accept); !errors.Is(err, domain.ErrMarketBuyerFull) {
		t.Fatalf("an accept with nowhere to deliver: %v", err)
	}
	if still, _ := s.MarketOfferByID(made.ID); still.State != domain.MarketOfferOpen {
		t.Fatalf("a full buyer closed the offer: %s", still.State)
	}
	// The bag was the pick but is full: the warehouse takes it once it has
	// room.
	if _, err := s.TransactStorage(buyer, func(_ *domain.Character, storage *domain.AccountStorage) error {
		storage.Rows = storage.Rows[1:]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	sale, accepted, err := s.MarketAcceptOffer(accept)
	if err != nil || sale.Price != 4000 || sale.Fee != 40 || sale.To != domain.MarketToWarehouse ||
		accepted.State != domain.MarketOfferAccepted {
		t.Fatalf("accept = %+v / %+v, %v", sale, accepted, err)
	}
	if *buyer.Gold != 12345-4000 {
		t.Fatalf("the buyer paid %d", 12345-*buyer.Gold)
	}
	if _, _, err := s.MarketAcceptOffer(accept); !errors.Is(err, domain.ErrMarketOfferGone) {
		t.Fatalf("an accepted offer accepted again: %v", err)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after the accepted offer the sword is in %d places", n)
	}
}

/*
================
TestMarketOfferClosesWhenTheListingGoes

A sale or a cancel closes every open offer on the listing, and an accept
whose buyer can no longer pay closes the offer with that reason.
================
*/
func TestMarketOfferClosesWhenTheListingGoes(t *testing.T) {
	t.Parallel()
	s, seller, buyer := bagMarketFixture(t)
	listing := listFromBag(t, s, seller, domain.MarketGold, 5000, false)
	made, err := s.MarketMakeOffer(domain.MarketOfferRequest{ListingID: listing.ID, BuyerAccount: marketBuyer, Buyer: buyer,
		Price: 3000, To: domain.MarketToWarehouse, Rules: testOfferRules})
	if err != nil {
		t.Fatal(err)
	}
	*buyer.Gold = 10
	if _, _, err := s.MarketAcceptOffer(domain.MarketAcceptRequest{OfferID: made.ID, SellerAccount: marketSeller, Buyer: buyer,
		BagSlot: -1, Rules: testOfferRules}); !errors.Is(err, domain.ErrMarketBuyerFunds) {
		t.Fatalf("an accept the buyer cannot pay: %v", err)
	}
	if closed, _ := s.MarketOfferByID(made.ID); closed.State != domain.MarketOfferClosed {
		t.Fatalf("the unpayable offer is %s", closed.State)
	}
	*buyer.Gold = 12345
	second, err := s.MarketMakeOffer(domain.MarketOfferRequest{ListingID: listing.ID, BuyerAccount: marketBuyer, Buyer: buyer,
		Price: 3000, To: domain.MarketToWarehouse, Rules: testOfferRules})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.MarketCancel(marketSeller, listing.ID); err != nil {
		t.Fatal(err)
	}
	if closed, _ := s.MarketOfferByID(second.ID); closed.State != domain.MarketOfferClosed {
		t.Fatalf("a cancel left the offer %s", closed.State)
	}
}

/*
================
TestMarketReclaimIntoTheBag

A held item whose warehouse is still full comes back into a free bag
slot; the warehouse path still refuses while it is full.
================
*/
func TestMarketReclaimIntoTheBag(t *testing.T) {
	t.Parallel()
	s, seller, buyer, listing := expiredHeldListing(t)
	fillWarehouse(t, s, seller)
	toWarehouse := domain.MarketReclaimRequest{Account: marketSeller, ListingID: listing.ID, To: domain.MarketToWarehouse}
	if _, err := s.MarketReclaim(toWarehouse); !errors.Is(err, domain.ErrMarketWarehouseFull) {
		t.Fatalf("a take-back into a full warehouse: %v", err)
	}
	toBag := domain.MarketReclaimRequest{Account: marketSeller, ListingID: listing.ID, To: domain.MarketToBag,
		Receiver: seller, BagSlot: 30}
	returned, err := s.MarketReclaim(toBag)
	if err != nil || returned.State != domain.MarketReturned {
		t.Fatalf("bag take-back = %+v, %v", returned, err)
	}
	found := false
	for _, row := range seller.MissionInventory {
		found = found || (row.Slot == 30 && row.Codename == marketSword)
	}
	if !found {
		t.Fatalf("the bag = %+v", seller.MissionInventory)
	}
	if n := swordCount(t, s, seller, buyer); n != 1 {
		t.Fatalf("after the bag take-back the sword is in %d places", n)
	}
}

/*
================
TestMarketOrphanedProceedsGoToTheWarehouse

Gold owed to a character that no longer exists is paid into its
account's warehouse by the expiry pass, once.
================
*/
func TestMarketOrphanedProceedsGoToTheWarehouse(t *testing.T) {
	t.Parallel()
	s, seller, buyer := bagMarketFixture(t)
	listing := listFromBag(t, s, seller, domain.MarketGold, 1000, false)
	if _, err := s.MarketBuy(domain.MarketBuyRequest{ListingID: listing.ID, Version: listing.Version, BuyerAccount: marketBuyer,
		Payer: buyer, Rules: testOfferRules}); err != nil {
		t.Fatal(err)
	}
	if paid, err := s.SettleOrphanedMarketProceeds(10); err != nil || paid != 0 {
		t.Fatalf("a living seller's proceeds settled: %d, %v", paid, err)
	}
	// The listing character is gone: no record has its id any more.
	if _, err := s.syncDBLocked().Exec("UPDATE market_sales SET seller_char = 999999"); err != nil {
		t.Fatal(err)
	}
	if paid, err := s.SettleOrphanedMarketProceeds(10); err != nil || paid != 1 {
		t.Fatalf("orphan pass = %d, %v", paid, err)
	}
	if storage, _ := readAccountStorage(s.syncDBLocked(), marketSeller); storage.Gold != 990 {
		t.Fatalf("the seller's warehouse gold = %d", storage.Gold)
	}
	if paid, _ := s.SettleOrphanedMarketProceeds(10); paid != 0 {
		t.Fatalf("the orphan was paid twice")
	}
}

/*
================
TestEnsureMarketAddsTheLaterColumns

A file made before the 2026-11-19 columns gains them with defaults that
keep its old sales paid, and opening it twice changes nothing more.
================
*/
func TestEnsureMarketAddsTheLaterColumns(t *testing.T) {
	t.Parallel()
	db, err := sql.Open("sqlite", t.TempDir()+"/old.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`
CREATE TABLE market_listings (id INTEGER PRIMARY KEY, division TEXT NOT NULL, seller_account TEXT NOT NULL,
  seller_char INTEGER NOT NULL, item TEXT NOT NULL, codename TEXT NOT NULL, currency TEXT NOT NULL, price INTEGER NOT NULL,
  listed_ms INTEGER NOT NULL, expires_ms INTEGER NOT NULL, state TEXT NOT NULL, version INTEGER NOT NULL);
CREATE TABLE market_sales (listing_id INTEGER PRIMARY KEY, buyer_account TEXT NOT NULL, buyer_char INTEGER NOT NULL,
  currency TEXT NOT NULL, price INTEGER NOT NULL, fee INTEGER NOT NULL, at_ms INTEGER NOT NULL);
INSERT INTO market_sales VALUES (1, 'a', 2, 'gold', 100, 1, 5);`); err != nil {
		t.Fatal(err)
	}
	for pass := 0; pass < 2; pass++ {
		if err := ensureMarket(db); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}
	for _, c := range marketColumns {
		if present, err := tableHasColumn(db, c.table, c.column); err != nil || !present {
			t.Fatalf("%s.%s missing: %v", c.table, c.column, err)
		}
	}
	var unpaid int
	var to string
	if err := db.QueryRow("SELECT unpaid, dest FROM market_sales WHERE listing_id = 1").Scan(&unpaid, &to); err != nil ||
		unpaid != 0 || to != domain.MarketToWarehouse {
		t.Fatalf("the old sale = unpaid %d, dest %q, %v", unpaid, to, err)
	}
}

/*
================
TestMarketHeldListingsCountTowardTheCap

A held item still sits in the market, so it takes a place under the
cap: a full warehouse is no way to keep any number of items there.
================
*/
func TestMarketHeldListingsCountTowardTheCap(t *testing.T) {
	t.Parallel()
	s, _, _, held := expiredHeldListing(t)
	if held.ID == 0 {
		t.Fatal("no held listing")
	}
	storage, err := s.MarketWarehouse(marketSeller)
	if err != nil || len(storage.Rows) < 2 {
		t.Fatalf("warehouse = %+v, %v", storage, err)
	}
	list := func(slot int64) error {
		_, err := s.MarketList(domain.MarketListRequest{Division: testDivision, Account: marketSeller, Slot: slot,
			Currency: domain.MarketSilk, Price: 10, Admit: admitAll, Rules: testMarketRules})
		return err
	}
	if err := list(storage.Rows[0].Slot); err != nil {
		t.Fatalf("the listing beside the held one: %v", err)
	}
	if err := list(storage.Rows[1].Slot); !errors.Is(err, domain.ErrMarketCap) {
		t.Fatalf("one held and one open with a cap of %d: %v", testMarketRules.OpenCap, err)
	}
}

/*
================
TestMarketReclaimIntoAFullWarehouseChangesNothing

The player picked the warehouse: when it is full the cancel refuses and
the listing stays on sale, never turned held behind the refusal.
================
*/
func TestMarketReclaimIntoAFullWarehouseChangesNothing(t *testing.T) {
	t.Parallel()
	s, seller, _ := bagMarketFixture(t)
	listing := listFromBag(t, s, seller, domain.MarketGold, 5000, false)
	fillWarehouse(t, s, seller)
	_, err := s.MarketReclaim(domain.MarketReclaimRequest{Account: marketSeller, ListingID: listing.ID,
		To: domain.MarketToWarehouse})
	if !errors.Is(err, domain.ErrMarketWarehouseFull) {
		t.Fatalf("a cancel into a full warehouse: %v", err)
	}
	after, err := s.MarketListingByID(listing.ID)
	if err != nil || after.State != domain.MarketOpen || after.Version != listing.Version {
		t.Fatalf("after the refusal the listing is %+v, %v", after, err)
	}
}

/*
================
TestMarketIncomingOffersAreBounded

Other accounts fill the seller's incoming list, so it is cut to the
newest limit open offers.
================
*/
func TestMarketIncomingOffersAreBounded(t *testing.T) {
	t.Parallel()
	s, seller, _ := bagMarketFixture(t)
	listing := listFromBag(t, s, seller, domain.MarketGold, 5000, false)
	for i := 0; i < 5; i++ {
		if _, err := s.syncDBLocked().Exec(`INSERT INTO market_offers (listing_id, division, buyer_account, buyer_char,
buyer_name, price, dest, state, at_ms) VALUES (?, ?, ?, ?, ?, ?, 'bag', 'open', ?)`, listing.ID, testDivision,
			fmt.Sprintf("flood%d", i), 100+i, fmt.Sprintf("Flood%d", i), 10, i); err != nil {
			t.Fatal(err)
		}
	}
	incoming, _, err := s.MarketOffers(marketSeller, 3)
	if err != nil || len(incoming) != 3 || incoming[0].BuyerName != "Flood4" {
		t.Fatalf("incoming = %+v, %v", incoming, err)
	}
}
