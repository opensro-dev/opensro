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
	buy       listing -> buyer's warehouse; buyer's Silk or a character's
	          gold -> seller's Silk or warehouse gold, less the fee
	return    listing -> seller's warehouse (cancel or expiry), or held in
	          the listing while that warehouse is full
	take back held listing -> seller's warehouse

Gold proceeds land in the seller's account warehouse, never on a
character: a character may be online, and its record belongs to the
action lane then. The warehouse is the account's and is only written
under the store lock, so a sale can never race a player's own move.

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
  version        INTEGER NOT NULL
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
  at_ms         INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS market_sales_buyer ON market_sales (buyer_account, at_ms);
`

// marketMaxGoldPrice keeps price * fee percent inside int64; no real
// price comes near it (a character carries at most a few billion).
const marketMaxGoldPrice = math.MaxInt64 / 100

// marketListingColumns is every column a listing read selects, in
// scanListing's order.
const marketListingColumns = `id, division, seller_account, seller_char, item, currency, price,
listed_ms, expires_ms, state, version`

/*
================
ensureMarket

Creates the market tables when the store opens: additive, so no stamp
moves and an older binary on the same file never notices them.
================
*/
func ensureMarket(db *sql.DB) error {
	if _, err := db.Exec(marketSchema); err != nil {
		return fmt.Errorf("market: %w", err)
	}
	return nil
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
	open, err := countOpenListings(s.syncDBLocked(), req.Account)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if open >= req.Rules.OpenCap {
		return domain.MarketListing{}, domain.ErrMarketCap
	}
	now := s.now().UnixMilli()
	listing := domain.MarketListing{
		Division: req.Division, SellerAccount: req.Account, Currency: req.Currency, Price: req.Price,
		ListedMs: now, ExpiresMs: now + req.Rules.DurationMs, State: domain.MarketOpen, Version: 1,
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
	division, known := s.charDivision[character]
	if !known || character.AccountID != req.Account || character.DeletePending {
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

The whole sale is one transaction: the buyer pays, the seller is paid
less the fee, the item lands in the buyer's warehouse, the listing turns
sold and the sale row is written. Any refusal changes nothing.
================
*/
func (s *Store) MarketBuy(req domain.MarketBuyRequest) (domain.MarketSale, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || req.BuyerAccount == "" {
		return domain.MarketSale{}, domain.ErrMarketUnavailable
	}
	db := s.syncDBLocked()
	listing, err := readListing(db, req.ListingID)
	if err != nil {
		return domain.MarketSale{}, err
	}
	now := s.now().UnixMilli()
	if listing.State != domain.MarketOpen || listing.Version != req.Version || listing.ExpiresMs <= now {
		return domain.MarketSale{}, domain.ErrMarketChanged
	}
	if listing.SellerAccount == req.BuyerAccount {
		return domain.MarketSale{}, domain.ErrMarketOwnListing
	}
	buyerStorage, err := readAccountStorage(db, req.BuyerAccount)
	if err != nil {
		return domain.MarketSale{}, err
	}
	slot, free := freeStorageSlot(buyerStorage)
	if !free {
		return domain.MarketSale{}, domain.ErrMarketWarehouseFull
	}
	item := listing.Item
	item.Slot = slot
	buyerStorage.Rows = append(buyerStorage.Rows, item)
	fee := req.Rules.MarketFee(listing.Price)
	sale := domain.MarketSale{ListingID: listing.ID, BuyerAccount: req.BuyerAccount, Currency: listing.Currency,
		Price: listing.Price, Fee: fee, AtMs: now}

	var payer *domain.Character
	var payerDivision string
	var paidGold int64
	var sellerStorage domain.AccountStorage
	if listing.Currency == domain.MarketGold {
		// Read before the transaction: the store has one connection, so a
		// read outside an open transaction would wait on it forever.
		if sellerStorage, err = readAccountStorage(db, listing.SellerAccount); err != nil {
			return domain.MarketSale{}, err
		}
		if sellerStorage.Gold > math.MaxInt64-(listing.Price-fee) {
			return domain.MarketSale{}, fmt.Errorf("market: warehouse gold would overflow")
		}
		sellerStorage.Gold += listing.Price - fee
		payer = req.Payer
		division, known := s.charDivision[payer]
		if payer == nil || !known || payer.AccountID != req.BuyerAccount || payer.DeletePending || division != listing.Division {
			return domain.MarketSale{}, domain.ErrMarketUnavailable
		}
		payerDivision = division
		// The live record, never the row on disk: a deferred write
		// (MutateCharacterDeferred, queued before the logout) has already run
		// on it and only waits for the tick batch, which beginTxLocked flushes
		// before this transaction opens. The buy then neither spends gold the
		// record no longer holds nor writes back a balance that loses it.
		paidGold = ledgerGold(payer) - listing.Price
		if paidGold < 0 {
			return domain.MarketSale{}, domain.ErrMarketFunds
		}
		sale.BuyerChar = payer.ID
	}
	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		return domain.MarketSale{}, err
	}
	defer func() { _ = tx.Rollback() }()
	if err := writeAccountStorageTx(tx.Tx, req.BuyerAccount, buyerStorage); err != nil {
		return domain.MarketSale{}, err
	}
	settle := func() {}
	if listing.Currency == domain.MarketSilk {
		if _, err := debitSilkTx(tx.Tx, req.BuyerAccount, uint32(listing.Price), now); err != nil {
			return domain.MarketSale{}, err
		}
		if _, err := creditSilkTx(tx.Tx, listing.SellerAccount, uint32(listing.Price-fee), now); err != nil {
			return domain.MarketSale{}, err
		}
	} else {
		committed := *payer
		committed.Gold = &paidGold
		if err := tx.upsertCharacter(payerDivision, &committed); err != nil {
			return domain.MarketSale{}, err
		}
		if settle, err = s.ledgerOwnTxLocked(tx.Tx, "market-buy", payerDivision, payer, paidGold); err != nil {
			return domain.MarketSale{}, err
		}
		if err := writeAccountStorageTx(tx.Tx, listing.SellerAccount, sellerStorage); err != nil {
			return domain.MarketSale{}, err
		}
	}
	if err := setListingStateTx(tx.Tx, listing, domain.MarketSold); err != nil {
		return domain.MarketSale{}, err
	}
	if _, err := tx.Exec(`INSERT INTO market_sales (listing_id, buyer_account, buyer_char, currency, price, fee, at_ms)
VALUES (?, ?, ?, ?, ?, ?, ?)`, sale.ListingID, sale.BuyerAccount, sale.BuyerChar, sale.Currency, sale.Price, sale.Fee, sale.AtMs); err != nil {
		return domain.MarketSale{}, err
	}
	if err := s.commitMarketTxLocked(tx, "market-buy"); err != nil {
		return domain.MarketSale{}, err
	}
	if payer != nil {
		payer.Gold = &paidGold
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
	if err := s.commitMarketTxLocked(tx, "market-return"); err != nil {
		return domain.MarketListing{}, err
	}
	listing.State = state
	listing.Version++
	return listing, nil
}

/*
================
MarketWarehouse

The account's warehouse, for the website's list picker: the site lists
only from it.
================
*/
func (s *Store) MarketWarehouse(account string) (domain.AccountStorage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil || account == "" {
		return domain.AccountStorage{}, domain.ErrMarketUnavailable
	}
	return readAccountStorage(s.syncDBLocked(), account)
}

/*
================
MarketWallet

The account's Silk wallet, for the website's buy page.
================
*/
func (s *Store) MarketWallet(account string) (domain.MallBalance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil || account == "" {
		return domain.MallBalance{}, domain.ErrMarketUnavailable
	}
	return readMallBalance(s.syncDBLocked(), account)
}

/*
================
MarketListingByID
================
*/
func (s *Store) MarketListingByID(id int64) (domain.MarketListing, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return domain.MarketListing{}, domain.ErrMarketUnavailable
	}
	return readListing(s.syncDBLocked(), id)
}

/*
================
MarketOpenListings

Open, unexpired listings, newest first.
================
*/
func (s *Store) MarketOpenListings(q domain.MarketQuery) ([]domain.MarketListing, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, domain.ErrMarketUnavailable
	}
	query := `SELECT ` + marketListingColumns + ` FROM market_listings
WHERE division = ? AND state = 'open' AND expires_ms > ?`
	args := []any{q.Division, s.now().UnixMilli()}
	if q.Currency != "" {
		query += ` AND currency = ?`
		args = append(args, q.Currency)
	}
	if q.Codenames != nil {
		if len(q.Codenames) == 0 {
			return []domain.MarketListing{}, nil
		}
		query += ` AND codename IN (?` + repeatPlaceholders(len(q.Codenames)-1) + `)`
		for _, codename := range q.Codenames {
			args = append(args, codename)
		}
	}
	query += ` ORDER BY listed_ms DESC, id DESC LIMIT ? OFFSET ?`
	args = append(args, q.Limit, q.Offset)
	rows, err := s.syncDBLocked().Query(query, args...)
	if err != nil {
		return nil, err
	}
	return scanListings(rows)
}

/*
================
MarketAccountListings

The account's listings that still concern it: open and held ones, and
the last limit sold or returned, newest first.
================
*/
func (s *Store) MarketAccountListings(account string, limit int) ([]domain.MarketListing, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, domain.ErrMarketUnavailable
	}
	rows, err := s.syncDBLocked().Query(`SELECT `+marketListingColumns+` FROM market_listings
WHERE seller_account = ? AND state IN ('open', 'held')
UNION ALL
SELECT * FROM (SELECT `+marketListingColumns+` FROM market_listings
WHERE seller_account = ? AND state IN ('sold', 'returned') ORDER BY listed_ms DESC, id DESC LIMIT ?)
ORDER BY listed_ms DESC, id DESC`, account, account, limit)
	if err != nil {
		return nil, err
	}
	return scanListings(rows)
}

/*
================
MarketPurchases

The account's last limit purchases, newest first.
================
*/
func (s *Store) MarketPurchases(account string, limit int) ([]domain.MarketSale, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, domain.ErrMarketUnavailable
	}
	rows, err := s.syncDBLocked().Query(`SELECT listing_id, buyer_account, buyer_char, currency, price, fee, at_ms
FROM market_sales WHERE buyer_account = ? ORDER BY at_ms DESC, listing_id DESC LIMIT ?`, account, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []domain.MarketSale{}
	for rows.Next() {
		var sale domain.MarketSale
		if err := rows.Scan(&sale.ListingID, &sale.BuyerAccount, &sale.BuyerChar, &sale.Currency, &sale.Price, &sale.Fee, &sale.AtMs); err != nil {
			return nil, err
		}
		out = append(out, sale)
	}
	return out, rows.Err()
}

//============================================================================

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
countOpenListings
================
*/
func countOpenListings(db *sql.DB, account string) (int, error) {
	var open int
	err := db.QueryRow("SELECT COUNT(*) FROM market_listings WHERE seller_account = ? AND state = 'open'", account).Scan(&open)
	return open, err
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
price, listed_ms, expires_ms, state, version) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		listing.Division, listing.SellerAccount, listing.SellerChar, string(item), listing.Item.Codename,
		listing.Currency, listing.Price, listing.ListedMs, listing.ExpiresMs, listing.State, listing.Version)
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

/*
================
readListing
================
*/
func readListing(db *sql.DB, id int64) (domain.MarketListing, error) {
	rows, err := db.Query(`SELECT `+marketListingColumns+` FROM market_listings WHERE id = ?`, id)
	if err != nil {
		return domain.MarketListing{}, err
	}
	listings, err := scanListings(rows)
	if err != nil {
		return domain.MarketListing{}, err
	}
	if len(listings) == 0 {
		return domain.MarketListing{}, domain.ErrMarketNotFound
	}
	return listings[0], nil
}

/*
================
scanListings

Every row of a listing query; closes rows.
================
*/
func scanListings(rows *sql.Rows) ([]domain.MarketListing, error) {
	defer rows.Close()
	out := []domain.MarketListing{}
	for rows.Next() {
		var listing domain.MarketListing
		var item string
		if err := rows.Scan(&listing.ID, &listing.Division, &listing.SellerAccount, &listing.SellerChar, &item,
			&listing.Currency, &listing.Price, &listing.ListedMs, &listing.ExpiresMs, &listing.State, &listing.Version); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(item), &listing.Item); err != nil {
			return nil, fmt.Errorf("market: listing %d item: %w", listing.ID, err)
		}
		out = append(out, listing)
	}
	return out, rows.Err()
}

/*
================
repeatPlaceholders
================
*/
func repeatPlaceholders(n int) string {
	out := make([]byte, 0, n*3)
	for i := 0; i < n; i++ {
		out = append(out, ", ?"...)
	}
	return string(out)
}
