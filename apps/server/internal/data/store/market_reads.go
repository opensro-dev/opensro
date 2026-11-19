/*
===========================================================================

market_reads.go - the market escrow's read-only doors and row scanners

Port-only, not native (SRO_CONSIGNMENT, opensro#3529). The queries the
game and the site page through: a listing, the open page, an account's
listings and purchases, its warehouse and wallet. None of them writes;
the doors that move items and money stay in market.go.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/domain"
)

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
	rows, err := s.syncDBLocked().Query(`SELECT `+marketSaleColumns+`
FROM market_sales WHERE buyer_account = ? ORDER BY at_ms DESC, listing_id DESC LIMIT ?`, account, limit)
	if err != nil {
		return nil, err
	}
	return scanSales(rows)
}

/*
================
scanSales

Every row of a sale query; closes rows.
================
*/
func scanSales(rows *sql.Rows) ([]domain.MarketSale, error) {
	defer rows.Close()
	out := []domain.MarketSale{}
	for rows.Next() {
		var sale domain.MarketSale
		if err := rows.Scan(&sale.ListingID, &sale.BuyerAccount, &sale.BuyerChar, &sale.Currency, &sale.Price, &sale.Fee,
			&sale.AtMs, &sale.SellerChar, &sale.Unpaid, &sale.Codename, &sale.To, &sale.Slot); err != nil {
			return nil, err
		}
		out = append(out, sale)
	}
	return out, rows.Err()
}

/*
================
insertSaleTx
================
*/
func insertSaleTx(tx *sql.Tx, sale domain.MarketSale) error {
	_, err := tx.Exec(`INSERT INTO market_sales (`+marketSaleColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		sale.ListingID, sale.BuyerAccount, sale.BuyerChar, sale.Currency, sale.Price, sale.Fee, sale.AtMs, sale.SellerChar,
		sale.Unpaid, sale.Codename, sale.To, sale.Slot)
	return err
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
			&listing.Currency, &listing.Price, &listing.ListedMs, &listing.ExpiresMs, &listing.State, &listing.Version,
			&listing.SellerName, &listing.Anonymous); err != nil {
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
