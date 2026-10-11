/*
===========================================================================

silkgrant.go - operator silk credits into the account's mall wallet

GMs and operators grant silk (action/operator_silk.go, operator tooling,
not a gameplay rule). The credit lands in mall_accounts.silk, the wallet
the Item Mall debits, so it is real silk and survives restarts.

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"math"

	"opensro.online/server/internal/domain"
)

/*
================
GrantSilk

Add amount to the account's silk, creating the wallet when it has none.
One transaction; a total that would pass the wallet's uint32 is refused
whole rather than clamped.
================
*/
func (s *Store) GrantSilk(accountID string, amount uint32) (domain.MallBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accountID == "" || s.db == nil {
		return domain.MallBalance{}, fmt.Errorf("silk grant: account unavailable")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return domain.MallBalance{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var balance domain.MallBalance
	err = tx.QueryRow("SELECT silk, gift_silk, points FROM mall_accounts WHERE account_id = ?", accountID).
		Scan(&balance.Silk, &balance.GiftSilk, &balance.Points)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.MallBalance{}, err
	}
	if uint64(balance.Silk)+uint64(amount) > math.MaxUint32 {
		return domain.MallBalance{}, fmt.Errorf("silk grant: wallet would overflow")
	}
	balance.Silk += amount
	_, err = tx.Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET silk = excluded.silk`, accountID, balance.Silk, balance.GiftSilk, balance.Points)
	if err != nil {
		return domain.MallBalance{}, err
	}
	return balance, tx.Commit()
}
