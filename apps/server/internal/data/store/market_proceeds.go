/*
===========================================================================

market_proceeds.go - the gold a sale owes the character that listed it
(opensro#3529)

Port-only, not native (SRO_CONSIGNMENT). The owner's rule (2026-11-19):
a gold sale pays the listing character. The sale cannot write that
character's record, which belongs to its action lane while it is online,
so settleSaleLocked marks the sale unpaid and the lane claims it here,
under its division lock, once the character is in the world; an offline
seller is paid on its next entry. A character that no longer exists can
never claim, so the expiry pass pays what it was owed into its account
warehouse instead: owed gold is never lost.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
	"math"

	"opensro.online/server/internal/domain"
)

/*
================
MarketOwedCharacters

The characters some sale still owes gold, for the lane's claim pass to
test against who is online.
================
*/
func (s *Store) MarketOwedCharacters() (map[int64]bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, domain.ErrMarketUnavailable
	}
	rows, err := s.syncDBLocked().Query("SELECT DISTINCT seller_char FROM market_sales WHERE unpaid = 1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	owed := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		owed[id] = true
	}
	return owed, rows.Err()
}

/*
================
MarketClaimProceeds

Pays c every sale that owes it, in one transaction, and returns those
sales (for the seller's notice). The caller is c's action lane under the
division lock, so the live record the store assigns after the commit is
the one the lane owns, as a bag listing's is.
================
*/
func (s *Store) MarketClaimProceeds(c *domain.Character) ([]domain.MarketSale, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil || c == nil {
		return nil, domain.ErrMarketUnavailable
	}
	division, owned := s.ownCharacterLocked(c, c.AccountID)
	if !owned {
		return nil, domain.ErrMarketUnavailable
	}
	rows, err := s.syncDBLocked().Query(`SELECT `+marketSaleColumns+` FROM market_sales
WHERE seller_char = ? AND unpaid = 1 ORDER BY at_ms, listing_id`, c.ID)
	if err != nil {
		return nil, err
	}
	sales, err := scanSales(rows)
	if err != nil || len(sales) == 0 {
		return nil, err
	}
	balance := ledgerGold(c)
	for _, sale := range sales {
		paid := sale.Price - sale.Fee
		if balance > math.MaxInt64-paid {
			return nil, fmt.Errorf("market: character %d gold would overflow", c.ID)
		}
		balance += paid
	}
	tx, err := s.beginCharacterTxLocked()
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	committed := *c
	committed.Gold = &balance
	if err := tx.upsertCharacter(division, &committed); err != nil {
		return nil, err
	}
	settle, err := s.ledgerOwnTxLocked(tx.Tx, "market-proceeds", division, c, balance)
	if err != nil {
		return nil, err
	}
	for _, sale := range sales {
		if err := markSalePaidTx(tx.Tx, sale.ListingID); err != nil {
			return nil, err
		}
	}
	if err := s.commitMarketTxLocked(tx, "market-proceeds"); err != nil {
		return nil, err
	}
	c.Gold = &balance
	settle()
	return sales, nil
}

/*
================
SettleOrphanedMarketProceeds

Pays at most limit owed sales whose listing character no longer exists
into the seller account's warehouse gold, and returns how many it paid.
A character being deleted still exists (its deletion can be cancelled),
so its sales stay owed to it.
================
*/
func (s *Store) SettleOrphanedMarketProceeds(limit int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return 0, domain.ErrMarketUnavailable
	}
	db := s.syncDBLocked()
	rows, err := db.Query(`SELECT sale.listing_id, sale.seller_char, sale.price - sale.fee, listing.seller_account,
listing.division FROM market_sales sale JOIN market_listings listing ON listing.id = sale.listing_id
WHERE sale.unpaid = 1 ORDER BY sale.at_ms, sale.listing_id`)
	if err != nil {
		return 0, err
	}
	type owed struct {
		listing, character, amount int64
		account, division          string
	}
	var orphans []owed
	for rows.Next() {
		var o owed
		if err := rows.Scan(&o.listing, &o.character, &o.amount, &o.account, &o.division); err != nil {
			rows.Close()
			return 0, err
		}
		if s.characterByIDLocked(o.division, o.character) == nil && len(orphans) < limit {
			orphans = append(orphans, o)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()
	paid := 0
	for _, o := range orphans {
		storage, err := readAccountStorage(db, o.account)
		if err != nil {
			return paid, err
		}
		if storage.Gold > math.MaxInt64-o.amount {
			return paid, fmt.Errorf("market: warehouse gold would overflow")
		}
		storage.Gold += o.amount
		tx, err := s.beginTxLocked()
		if err != nil {
			return paid, err
		}
		err = writeAccountStorageTx(tx, o.account, storage)
		if err == nil {
			err = markSalePaidTx(tx, o.listing)
		}
		if err == nil {
			err = s.commitMarketTxLocked(tx, "market-proceeds")
		}
		if err != nil {
			_ = tx.Rollback()
			return paid, err
		}
		paid++
	}
	return paid, nil
}

/*
================
markSalePaidTx

Marks a sale's owed gold paid, once.
================
*/
func markSalePaidTx(tx *sql.Tx, listingID int64) error {
	_, err := tx.Exec("UPDATE market_sales SET unpaid = 0 WHERE listing_id = ? AND unpaid = 1", listingID)
	return err
}
