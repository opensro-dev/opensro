/*
===========================================================================

market.go - the market escrow's tables and doors (opensro#3529,
opensro-web#148)

Port-only, not native: v1.150 has no consignment. The GameWorld serves it
only with SRO_CONSIGNMENT (game/market); off, nothing calls these doors.

A listed item lives in exactly one market_listings row and nowhere else.
Every door is one store transaction that takes the item (and any money)
out of its source and writes it to its destination, or changes nothing:

	list      bag or account warehouse -> listing (open)
	buy       listing -> the buyer's bag or warehouse (the buyer picks);
	          buyer's Silk or a character's gold -> seller's Silk, or gold
	          owed to the listing character, less the fee
	offer     a price below the listing's; accept runs the buy at it
	          (market_offers.go)
	return    listing -> seller's bag or warehouse (cancel or expiry), or
	          held in the listing while that warehouse is full
	take back held listing -> seller's bag or warehouse
	proceeds  owed gold -> the listing character (market_proceeds.go)

The read-only doors (a listing, the open page, an account's listings,
purchases, warehouse and wallet) are in market_reads.go.

A gold sale of a bag listing pays the character that listed it (owner,
2026-11-19), but never by writing that record here: the character may be
online, and its record then belongs to its action lane. The sale stays
owed (market_sales.unpaid) until the lane claims it under its division
lock. A website listing has no character, so its gold goes to the
account warehouse, which is only written under the store lock.

Every door holds the store lock for its whole read-check-write, the lock
TransactStorage holds, so a warehouse move in the game and a market door
on the site are serialized. The tables are additive and created at open
with no schema stamp (ensureMarket), like the economy buckets.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"math"

	"opensro.online/server/internal/domain"
)

// marketSchema holds the listings and the sales; dbSchema carries it for a
// fresh file, ensureMarket for an existing one.
const marketSchema = `
CREATE TABLE IF NOT EXISTS market_listings (
  id             INTEGER PRIMARY KEY,
  division       TEXT    NOT NULL,
  seller_account TEXT    NOT NULL,
  seller_char    INTEGER NOT NULL,
  item           TEXT    NOT NULL,
  codename       TEXT    NOT NULL,
  currency       TEXT    NOT NULL CHECK (currency IN ('gold', 'silk')),
  price          INTEGER NOT NULL CHECK (price > 0),
  listed_ms      INTEGER NOT NULL,
  expires_ms     INTEGER NOT NULL,
  state          TEXT    NOT NULL CHECK (state IN ('open', 'sold', 'returned', 'held')),
  version        INTEGER NOT NULL,
  seller_name    TEXT    NOT NULL DEFAULT '',
  anonymous      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS market_listings_expiry ON market_listings (state, expires_ms);
CREATE INDEX IF NOT EXISTS market_listings_seller ON market_listings (seller_account, state);
CREATE INDEX IF NOT EXISTS market_listings_search ON market_listings (division, state, codename);
CREATE TABLE IF NOT EXISTS market_sales (
  listing_id    INTEGER PRIMARY KEY,
  buyer_account TEXT    NOT NULL,
  buyer_char    INTEGER NOT NULL,
  currency      TEXT    NOT NULL,
  price         INTEGER NOT NULL,
  fee           INTEGER NOT NULL,
  at_ms         INTEGER NOT NULL,
  seller_char   INTEGER NOT NULL DEFAULT 0,
  unpaid        INTEGER NOT NULL DEFAULT 0,
  codename      TEXT    NOT NULL DEFAULT '',
  dest          TEXT    NOT NULL DEFAULT 'warehouse',
  slot          INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS market_sales_buyer ON market_sales (buyer_account, at_ms);
CREATE TABLE IF NOT EXISTS market_offers (
  id            INTEGER PRIMARY KEY,
  listing_id    INTEGER NOT NULL,
  division      TEXT    NOT NULL,
  buyer_account TEXT    NOT NULL,
  buyer_char    INTEGER NOT NULL,
  buyer_name    TEXT    NOT NULL,
  price         INTEGER NOT NULL CHECK (price > 0),
  dest          TEXT    NOT NULL CHECK (dest IN ('bag', 'warehouse')),
  state         TEXT    NOT NULL CHECK (state IN ('open', 'accepted', 'rejected', 'withdrawn', 'closed')),
  at_ms         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS market_offers_listing ON market_offers (listing_id, state);
CREATE INDEX IF NOT EXISTS market_offers_buyer ON market_offers (buyer_account, state);
`

// marketColumns are the columns later than the first market tables, by
// table, with their definitions: ensureMarket adds each one a file lacks.
var marketColumns = []struct{ table, column, definition string }{
	{"market_listings", "seller_name", "TEXT NOT NULL DEFAULT ''"},
	{"market_listings", "anonymous", "INTEGER NOT NULL DEFAULT 0"},
	{"market_sales", "seller_char", "INTEGER NOT NULL DEFAULT 0"},
	{"market_sales", "unpaid", "INTEGER NOT NULL DEFAULT 0"},
	{"market_sales", "codename", "TEXT NOT NULL DEFAULT ''"},
	{"market_sales", "dest", "TEXT NOT NULL DEFAULT 'warehouse'"},
	{"market_sales", "slot", "INTEGER NOT NULL DEFAULT 0"},
}

// marketLateIndexes index the later columns, so they are created only
// once ensureMarket has added them (dbSchema may meet an older file).
const marketLateIndexes = `
CREATE INDEX IF NOT EXISTS market_sales_owed ON market_sales (seller_char) WHERE unpaid = 1;
`

// marketMaxGoldPrice keeps price * fee percent inside int64; no real
// price comes near it (a character carries at most a few billion).
const marketMaxGoldPrice = math.MaxInt64 / 100

// marketListingColumns is every column a listing read selects, in
// scanListing's order.
const marketListingColumns = `id, division, seller_account, seller_char, item, currency, price,
listed_ms, expires_ms, state, version, seller_name, anonymous`

// marketSaleColumns is every column a sale read selects, in scanSales'
// order.
const marketSaleColumns = `listing_id, buyer_account, buyer_char, currency, price, fee, at_ms, seller_char,
unpaid, codename, dest, slot`

/*
================
ensureMarket

Creates the market tables when the store opens, and adds the later
columns a file lacks: additive, so no stamp moves and an older binary on
the same file never notices them (its inserts name their columns, and
every later column has a default).
================
*/
func ensureMarket(db *sql.DB) error {
	if _, err := db.Exec(marketSchema); err != nil {
		return fmt.Errorf("market: %w", err)
	}
	for _, c := range marketColumns {
		present, err := tableHasColumn(db, c.table, c.column)
		if err != nil {
			return fmt.Errorf("market: %w", err)
		}
		if present {
			continue
		}
		if _, err := db.Exec("ALTER TABLE " + c.table + " ADD COLUMN " + c.column + " " + c.definition); err != nil {
			return fmt.Errorf("market: add %s.%s: %w", c.table, c.column, err)
		}
	}
	if _, err := db.Exec(marketLateIndexes); err != nil {
		return fmt.Errorf("market: %w", err)
	}
	return nil
}

/*
================
tableHasColumn
================
*/
func tableHasColumn(db *sql.DB, table, column string) (bool, error) {
	rows, err := db.Query("SELECT name FROM pragma_table_info(?)", table)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

/*
================
MarketList
================
*/
func (s *Store) MarketList(req domain.MarketListRequest) (domain.MarketListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || req.Account == "" || req.Admit == nil {
		return domain.MarketListing{}, domain.ErrMarketUnavailable
	}
	if err := checkMarketPrice(req.Currency, req.Price); err != nil {
		return domain.MarketListing{}, err
	}
	active, err := countActiveListings(s.syncDBLocked(), req.Account)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if active >= req.Rules.OpenCap {
		return domain.MarketListing{}, domain.ErrMarketCap
	}
	now := s.now().UnixMilli()
	listing := domain.MarketListing{
		Division: req.Division, SellerAccount: req.Account, Currency: req.Currency, Price: req.Price,
		ListedMs: now, ExpiresMs: now + req.Rules.DurationMs, State: domain.MarketOpen, Version: 1,
		Anonymous: req.Anonymous,
	}
	if req.Character == nil {
		return s.listFromStorageLocked(req, listing)
	}
	return s.listFromBagLocked(req, listing)
}

/*
================
listFromStorageLocked
================
*/
func (s *Store) listFromStorageLocked(req domain.MarketListRequest, listing domain.MarketListing) (domain.MarketListing, error) {
	storage, err := readAccountStorage(s.syncDBLocked(), req.Account)
	if err != nil {
		return domain.MarketListing{}, err
	}
	row, rest, found := takeRow(storage.Rows, req.Slot)
	if !found {
		return domain.MarketListing{}, domain.ErrMarketNoItem
	}
	if err := req.Admit(row); err != nil {
		return domain.MarketListing{}, domain.ErrMarketNotTradeable
	}
	listing.Item = row
	storage.Rows = rest
	tx, err := s.beginTxLocked()
	if err != nil {
		return domain.MarketListing{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeAccountStorageTx(tx, req.Account, storage); err != nil {
		return domain.MarketListing{}, err
	}
	if listing.ID, err = insertListingTx(tx, listing); err != nil {
		return domain.MarketListing{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-list"); err != nil {
		return domain.MarketListing{}, err
	}
	return listing, nil
}

/*
================
listFromBagLocked

The item leaves the character's bag and the character row is written in
the listing's transaction; the live record changes only after commit.
================
*/
func (s *Store) listFromBagLocked(req domain.MarketListRequest, listing domain.MarketListing) (domain.MarketListing, error) {
	character := req.Character
	division, owned := s.ownCharacterLocked(character, req.Account)
	if !owned {
		return domain.MarketListing{}, domain.ErrMarketUnavailable
	}
	row, rest, found := takeRow(character.MissionInventory, req.Slot)
	if !found {
		return domain.MarketListing{}, domain.ErrMarketNoItem
	}
	if err := req.Admit(row); err != nil {
		return domain.MarketListing{}, domain.ErrMarketNotTradeable
	}
	listing.Division = division
	listing.SellerChar = character.ID
	listing.SellerName = character.Name
	listing.Item = row
	committed := *character
	committed.MissionInventory = rest
	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		return domain.MarketListing{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := tx.upsertCharacter(division, &committed); err != nil {
		return domain.MarketListing{}, err
	}
	if listing.ID, err = insertListingTx(tx.Tx, listing); err != nil {
		return domain.MarketListing{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-list"); err != nil {
		return domain.MarketListing{}, err
	}
	character.MissionInventory = rest
	return listing, nil
}

/*
================
MarketBuy

A buy at the listing's price, against the version the buyer saw
(settleSaleLocked makes the sale).
================
*/
func (s *Store) MarketBuy(req domain.MarketBuyRequest) (domain.MarketSale, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || req.BuyerAccount == "" {
		return domain.MarketSale{}, domain.ErrMarketUnavailable
	}
	listing, err := readListing(s.syncDBLocked(), req.ListingID)
	if err != nil {
		return domain.MarketSale{}, err
	}
	if listing.Version != req.Version {
		return domain.MarketSale{}, domain.ErrMarketChanged
	}
	return s.settleSaleLocked(saleTerms{listing: listing, buyer: req.BuyerAccount, payer: req.Payer, price: listing.Price,
		to: req.To, receiver: req.Receiver, bagSlot: req.BagSlot, rules: req.Rules})
}

/*
================
saleTerms

One sale settleSaleLocked makes: a buy at the listing's price, or an
accepted offer at its own. within runs inside the sale's transaction
(the offer's state change), nil for a buy.
================
*/
type saleTerms struct {
	listing  domain.MarketListing
	buyer    string
	payer    *domain.Character
	price    int64
	to       string
	receiver *domain.Character
	bagSlot  int64
	rules    domain.MarketRules
	within   func(tx *sql.Tx) error
}

/*
================
settleSaleLocked

The whole sale is one transaction: the buyer pays, the seller is paid
less the fee, the item lands in the picked bag slot or the first free
warehouse slot, the listing turns sold, its open offers close and the
sale row is written. Any refusal changes nothing.

The seller is paid by currency and listing: Silk to the account's
wallet; gold of a bag listing is owed to the listing character (claimed
by its lane, market_proceeds.go); gold of a website listing to the
account warehouse.
================
*/
func (s *Store) settleSaleLocked(t saleTerms) (domain.MarketSale, error) {
	db := s.syncDBLocked()
	listing := t.listing
	now := s.now().UnixMilli()
	if listing.State != domain.MarketOpen || listing.ExpiresMs <= now {
		return domain.MarketSale{}, domain.ErrMarketChanged
	}
	if listing.SellerAccount == t.buyer {
		return domain.MarketSale{}, domain.ErrMarketOwnListing
	}
	if t.price < 1 || t.price > listing.Price {
		return domain.MarketSale{}, domain.ErrMarketPrice
	}
	fee := t.rules.MarketFee(t.price)
	sale := domain.MarketSale{ListingID: listing.ID, BuyerAccount: t.buyer, Currency: listing.Currency,
		Price: t.price, Fee: fee, AtMs: now, Codename: listing.Item.Codename, To: t.to}
	item := listing.Item

	var buyerStorage domain.AccountStorage
	var receiverDivision string
	switch t.to {
	case domain.MarketToWarehouse, "":
		// The warehouse unless the bag is named: the website has no bag.
		sale.To = domain.MarketToWarehouse
		var err error
		if buyerStorage, err = readAccountStorage(db, t.buyer); err != nil {
			return domain.MarketSale{}, err
		}
		slot, free := freeStorageSlot(buyerStorage)
		if !free {
			return domain.MarketSale{}, domain.ErrMarketWarehouseFull
		}
		item.Slot = slot
		buyerStorage.Rows = append(buyerStorage.Rows, item)
	case domain.MarketToBag:
		division, owned := s.ownCharacterLocked(t.receiver, t.buyer)
		if !owned || division != listing.Division {
			return domain.MarketSale{}, domain.ErrMarketUnavailable
		}
		// The lane chose the slot inside the bag; the store only refuses
		// one a row already holds.
		if t.bagSlot < 0 || slotTaken(t.receiver.MissionInventory, t.bagSlot) {
			return domain.MarketSale{}, domain.ErrMarketBagFull
		}
		item.Slot = t.bagSlot
		receiverDivision = division
		sale.BuyerChar = t.receiver.ID
	default:
		return domain.MarketSale{}, domain.ErrMarketUnavailable
	}
	sale.Slot = item.Slot

	gold := listing.Currency == domain.MarketGold
	var payerDivision string
	var paidGold int64
	var sellerStorage domain.AccountStorage
	creditWarehouse := false
	if gold {
		division, owned := s.ownCharacterLocked(t.payer, t.buyer)
		if !owned || division != listing.Division {
			return domain.MarketSale{}, domain.ErrMarketUnavailable
		}
		payerDivision = division
		// The live record, never the row on disk: a deferred write
		// (MutateCharacterDeferred, queued before the logout) has already run
		// on it and only waits for the tick batch, which beginTxLocked flushes
		// before this transaction opens. The buy then neither spends gold the
		// record no longer holds nor writes back a balance that loses it.
		paidGold = ledgerGold(t.payer) - t.price
		if paidGold < 0 {
			return domain.MarketSale{}, domain.ErrMarketFunds
		}
		sale.BuyerChar = t.payer.ID
		if listing.SellerChar != 0 {
			sale.SellerChar = listing.SellerChar
			sale.Unpaid = true
		} else {
			// Read before the transaction: the store has one connection, so a
			// read outside an open transaction would wait on it forever.
			var err error
			if sellerStorage, err = readAccountStorage(db, listing.SellerAccount); err != nil {
				return domain.MarketSale{}, err
			}
			if sellerStorage.Gold > math.MaxInt64-(t.price-fee) {
				return domain.MarketSale{}, fmt.Errorf("market: warehouse gold would overflow")
			}
			sellerStorage.Gold += t.price - fee
			creditWarehouse = true
		}
	}
	toBag := sale.To == domain.MarketToBag
	// One character row when the payer's bag receives the item.
	sameRecord := gold && toBag && t.receiver == t.payer

	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		return domain.MarketSale{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if !toBag {
		if err := writeAccountStorageTx(tx.Tx, t.buyer, buyerStorage); err != nil {
			return domain.MarketSale{}, err
		}
	}
	settle := func() {}
	if gold {
		committed := *t.payer
		committed.Gold = &paidGold
		if sameRecord {
			committed.MissionInventory = withRow(t.payer.MissionInventory, item)
		}
		if err := tx.upsertCharacter(payerDivision, &committed); err != nil {
			return domain.MarketSale{}, err
		}
		if settle, err = s.ledgerOwnTxLocked(tx.Tx, "market-buy", payerDivision, t.payer, paidGold); err != nil {
			return domain.MarketSale{}, err
		}
		if creditWarehouse {
			if err := writeAccountStorageTx(tx.Tx, listing.SellerAccount, sellerStorage); err != nil {
				return domain.MarketSale{}, err
			}
		}
	} else {
		if _, err := debitSilkTx(tx.Tx, t.buyer, uint32(t.price), now); err != nil {
			return domain.MarketSale{}, err
		}
		if _, err := creditSilkTx(tx.Tx, listing.SellerAccount, uint32(t.price-fee), now); err != nil {
			return domain.MarketSale{}, err
		}
	}
	if toBag && !sameRecord {
		committed := *t.receiver
		committed.MissionInventory = withRow(t.receiver.MissionInventory, item)
		if err := tx.upsertCharacter(receiverDivision, &committed); err != nil {
			return domain.MarketSale{}, err
		}
	}
	if err := setListingStateTx(tx.Tx, listing, domain.MarketSold); err != nil {
		return domain.MarketSale{}, err
	}
	if t.within != nil {
		if err := t.within(tx.Tx); err != nil {
			return domain.MarketSale{}, err
		}
	}
	if err := closeOffersTx(tx.Tx, listing.ID); err != nil {
		return domain.MarketSale{}, err
	}
	if err := insertSaleTx(tx.Tx, sale); err != nil {
		return domain.MarketSale{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-buy"); err != nil {
		return domain.MarketSale{}, err
	}
	if gold {
		t.payer.Gold = &paidGold
	}
	if toBag {
		t.receiver.MissionInventory = withRow(t.receiver.MissionInventory, item)
	}
	settle()
	return sale, nil
}

/*
================
MarketCancel

The seller takes an open listing back; it follows the expiry path.
================
*/
func (s *Store) MarketCancel(account string, id int64) (domain.MarketListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, err := s.sellerListingLocked(account, id)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if listing.State != domain.MarketOpen {
		return domain.MarketListing{}, domain.ErrMarketChanged
	}
	return s.returnListingLocked(listing, true)
}

/*
================
MarketTakeBack

A held item moves to the seller's warehouse once it has room.
================
*/
func (s *Store) MarketTakeBack(account string, id int64) (domain.MarketListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, err := s.sellerListingLocked(account, id)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if listing.State != domain.MarketHeld {
		return domain.MarketListing{}, domain.ErrMarketNotHeld
	}
	returned, err := s.returnListingLocked(listing, false)
	if err == nil && returned.State == domain.MarketHeld {
		return listing, domain.ErrMarketWarehouseFull
	}
	return returned, err
}

/*
================
MarketReclaim

The game window's cancel and take-back: an open listing or a held item
goes back to the bag slot the lane chose, or to the warehouse. The player
picked the place, so a full warehouse refuses and changes nothing (an
open listing stays on sale) rather than turn the listing held behind a
refusal; a free bag slot always takes it.
================
*/
func (s *Store) MarketReclaim(req domain.MarketReclaimRequest) (domain.MarketListing, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	listing, err := s.sellerListingLocked(req.Account, req.ListingID)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if listing.State != domain.MarketOpen && listing.State != domain.MarketHeld {
		return domain.MarketListing{}, domain.ErrMarketNotHeld
	}
	if req.To != domain.MarketToBag {
		returned, err := s.returnListingLocked(listing, false)
		if err == nil && returned.State != domain.MarketReturned {
			return listing, domain.ErrMarketWarehouseFull
		}
		return returned, err
	}
	division, owned := s.ownCharacterLocked(req.Receiver, req.Account)
	if !owned || division != listing.Division {
		return domain.MarketListing{}, domain.ErrMarketUnavailable
	}
	if req.BagSlot < 0 || slotTaken(req.Receiver.MissionInventory, req.BagSlot) {
		return domain.MarketListing{}, domain.ErrMarketBagFull
	}
	item := listing.Item
	item.Slot = req.BagSlot
	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		return domain.MarketListing{}, err
	}
	defer func() { _ = tx.Rollback() }()
	committed := *req.Receiver
	committed.MissionInventory = withRow(req.Receiver.MissionInventory, item)
	if err := tx.upsertCharacter(division, &committed); err != nil {
		return domain.MarketListing{}, err
	}
	if err := setListingStateTx(tx.Tx, listing, domain.MarketReturned); err != nil {
		return domain.MarketListing{}, err
	}
	if err := closeOffersTx(tx.Tx, listing.ID); err != nil {
		return domain.MarketListing{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-return"); err != nil {
		return domain.MarketListing{}, err
	}
	req.Receiver.MissionInventory = withRow(req.Receiver.MissionInventory, item)
	listing.State = domain.MarketReturned
	listing.Version++
	return listing, nil
}

/*
================
ExpireMarketListings

Returns at most limit open listings whose time ran out, oldest first, and
how many it returned. A bounded batch per pass, so a backlog never holds
the store lock for long.
================
*/
func (s *Store) ExpireMarketListings(limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, domain.ErrMarketUnavailable
	}
	db := s.syncDBLocked()
	rows, err := db.Query(`SELECT `+marketListingColumns+` FROM market_listings
WHERE state = 'open' AND expires_ms <= ? ORDER BY expires_ms, id LIMIT ?`, s.now().UnixMilli(), limit)
	if err != nil {
		return 0, err
	}
	expired, err := scanListings(rows)
	if err != nil {
		return 0, err
	}
	done := 0
	for _, listing := range expired {
		if _, err := s.returnListingLocked(listing, true); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

/*
================
returnListingLocked

Moves the listed item to the seller's warehouse (state returned), or,
with fallBackToHeld and no free slot, marks it held so it waits in My
listings. A held item is never dropped.
================
*/
func (s *Store) returnListingLocked(listing domain.MarketListing, fallBackToHeld bool) (domain.MarketListing, error) {
	storage, err := readAccountStorage(s.syncDBLocked(), listing.SellerAccount)
	if err != nil {
		return domain.MarketListing{}, err
	}
	slot, free := freeStorageSlot(storage)
	if !free && !fallBackToHeld {
		return listing, nil
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return domain.MarketListing{}, err
	}
	defer func() { _ = tx.Rollback() }()
	state := domain.MarketHeld
	if free {
		item := listing.Item
		item.Slot = slot
		storage.Rows = append(storage.Rows, item)
		if err := writeAccountStorageTx(tx, listing.SellerAccount, storage); err != nil {
			return domain.MarketListing{}, err
		}
		state = domain.MarketReturned
	}
	if err := setListingStateTx(tx, listing, state); err != nil {
		return domain.MarketListing{}, err
	}
	if err := closeOffersTx(tx, listing.ID); err != nil {
		return domain.MarketListing{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-return"); err != nil {
		return domain.MarketListing{}, err
	}
	listing.State = state
	listing.Version++
	return listing, nil
}

//============================================================================

/*
================
sellerListingLocked

The listing id when it is account's: anyone else finds no such listing.
================
*/
func (s *Store) sellerListingLocked(account string, id int64) (domain.MarketListing, error) {
	if s.db == nil {
		return domain.MarketListing{}, domain.ErrMarketUnavailable
	}
	listing, err := readListing(s.syncDBLocked(), id)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if listing.SellerAccount != account {
		return domain.MarketListing{}, domain.ErrMarketNotFound
	}
	return listing, nil
}

/*
================
commitMarketTxLocked

Commits one door's transaction, or fails it with the store's forced
commit failure (FailCommits) after every write in it was made, so the
tests prove the rollback rather than an early refusal. A failure counts
in the store's write health either way. The caller's deferred Rollback
undoes the writes, and no door touches a live record before this returns
nil.
================
*/
func (s *Store) commitMarketTxLocked(tx interface{ Commit() error }, label string) error {
	err := s.commitFail
	if err == nil {
		err = tx.Commit()
	}
	if err != nil {
		s.recordWriteFailureLocked(label, err)
	}
	return err
}

/*
================
checkMarketPrice

Silk fits the wallet's uint32; gold stays clear of the fee arithmetic's
overflow.
================
*/
func checkMarketPrice(currency string, price int64) error {
	switch currency {
	case domain.MarketSilk:
		if price < 1 || price > math.MaxUint32 {
			return domain.ErrMarketPrice
		}
	case domain.MarketGold:
		if price < 1 || price > marketMaxGoldPrice {
			return domain.ErrMarketPrice
		}
	default:
		return domain.ErrMarketPrice
	}
	return nil
}

/*
================
takeRow

The row at slot and the rows without it.
================
*/
func takeRow(rows []domain.InventoryRow, slot int64) (domain.InventoryRow, []domain.InventoryRow, bool) {
	for i, row := range rows {
		if row.Slot == slot {
			rest := make([]domain.InventoryRow, 0, len(rows)-1)
			rest = append(rest, rows[:i]...)
			return row, append(rest, rows[i+1:]...), true
		}
	}
	return domain.InventoryRow{}, rows, false
}

/*
================
withRow

A copy of rows with row appended: the record a door commits, and the
live record it assigns after the commit, never share a backing array.
================
*/
func withRow(rows []domain.InventoryRow, row domain.InventoryRow) []domain.InventoryRow {
	out := make([]domain.InventoryRow, 0, len(rows)+1)
	return append(append(out, rows...), row)
}

/*
================
slotTaken
================
*/
func slotTaken(rows []domain.InventoryRow, slot int64) bool {
	for _, row := range rows {
		if row.Slot == slot {
			return true
		}
	}
	return false
}

/*
================
ownCharacterLocked

The division of c when the store owns its record and it is the
account's and not being deleted.
================
*/
func (s *Store) ownCharacterLocked(c *domain.Character, account string) (string, bool) {
	if c == nil {
		return "", false
	}
	division, known := s.charDivision[c]
	if !known || c.AccountID != account || c.DeletePending {
		return "", false
	}
	return division, true
}

/*
================
closeOffersTx

Closes every open offer on a listing that leaves the open state.
================
*/
func closeOffersTx(tx *sql.Tx, listingID int64) error {
	_, err := tx.Exec("UPDATE market_offers SET state = 'closed' WHERE listing_id = ? AND state = 'open'", listingID)
	return err
}

/*
================
freeStorageSlot

The lowest warehouse slot no row holds.
================
*/
func freeStorageSlot(storage domain.AccountStorage) (int64, bool) {
	used := make(map[int64]bool, len(storage.Rows))
	for _, row := range storage.Rows {
		used[row.Slot] = true
	}
	for slot := int64(0); slot < storage.Capacity; slot++ {
		if !used[slot] {
			return slot, true
		}
	}
	return 0, false
}

/*
================
countActiveListings

The account's listings the market still holds: open ones and held ones.
A held item counts, or a seller with a full warehouse could keep any
number of items in the market for free, listing more as each one's time
runs out.
================
*/
func countActiveListings(db *sql.DB, account string) (int, error) {
	var active int
	err := db.QueryRow("SELECT COUNT(*) FROM market_listings WHERE seller_account = ? AND state IN ('open', 'held')",
		account).Scan(&active)
	return active, err
}

/*
================
insertListingTx
================
*/
func insertListingTx(tx *sql.Tx, listing domain.MarketListing) (int64, error) {
	item, err := json.Marshal(listing.Item)
	if err != nil {
		return 0, err
	}
	result, err := tx.Exec(`INSERT INTO market_listings (division, seller_account, seller_char, item, codename, currency,
price, listed_ms, expires_ms, state, version, seller_name, anonymous) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		listing.Division, listing.SellerAccount, listing.SellerChar, string(item), listing.Item.Codename,
		listing.Currency, listing.Price, listing.ListedMs, listing.ExpiresMs, listing.State, listing.Version,
		listing.SellerName, listing.Anonymous)
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}

/*
================
setListingStateTx

Moves the listing to state, guarded by the version it was read at: a
row that changed meanwhile is ErrMarketChanged and the door rolls back.
================
*/
func setListingStateTx(tx *sql.Tx, listing domain.MarketListing, state string) error {
	result, err := tx.Exec("UPDATE market_listings SET state = ?, version = version + 1 WHERE id = ? AND version = ?",
		state, listing.ID, listing.Version)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed != 1 {
		return domain.ErrMarketChanged
	}
	return nil
}
