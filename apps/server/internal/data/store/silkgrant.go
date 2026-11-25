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
	tx, err := s.beginTxLocked()
	if err != nil {
		return domain.MallBalance{}, err
	}
	defer func() { _ = tx.Rollback() }()
	balance, err := creditSilkTx(tx, accountID, amount, s.now().UnixMilli())
	if err != nil {
		return domain.MallBalance{}, err
	}
	return balance, tx.Commit()
}

/*
================
creditSilkTx

Adds amount to the account's silk inside tx, creating the wallet when it
has none, and records the charge in silk_history. Zero moves nothing and
writes nothing. A total past the wallet's uint32 is refused whole rather
than clamped. GrantSilk, the referral rewards and a market sale's
proceeds share it.
================
*/
func creditSilkTx(tx *sql.Tx, accountID string, amount uint32, atMs int64) (domain.MallBalance, error) {
	// The credit lands in granted silk (#4133): operator and GM grants,
	// referral rewards and a market sale's proceeds are granted, never
	// earned, so what they buy stays bound. readMallBalanceTx reports
	// the earned balance beside it, untouched.
	balance, err := readMallBalanceTx(tx, accountID)
	if err != nil {
		return domain.MallBalance{}, err
	}
	if amount == 0 {
		return balance, nil
	}
	if uint64(balance.Silk)+uint64(amount) > math.MaxUint32 {
		return domain.MallBalance{}, fmt.Errorf("silk credit: wallet would overflow")
	}
	balance.Silk += amount
	_, err = tx.Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET silk = excluded.silk`, accountID, balance.Silk, balance.GiftSilk, balance.Points)
	if err != nil {
		return domain.MallBalance{}, err
	}
	if _, err := insertSilkHistoryTx(tx, accountID, atMs, silkHistoryCharge, domain.MallBalance{Silk: amount}); err != nil {
		return domain.MallBalance{}, err
	}
	return balance, nil
}

/*
================
debitSilkTx

Takes amount from the account's silk (never gift silk, which must not
turn into gold) inside tx, drawing the earned balance first as a mall
purchase does (#4133), and records the use in silk_history. Not enough
silk across both balances is domain.ErrMarketFunds and changes nothing.
================
*/
func debitSilkTx(tx *sql.Tx, accountID string, amount uint32, atMs int64) (domain.MallBalance, error) {
	balance, err := readMallBalanceTx(tx, accountID)
	if err != nil {
		return domain.MallBalance{}, err
	}
	if uint64(amount) > uint64(balance.Silk)+uint64(balance.EarnedSilk) {
		return domain.MallBalance{}, domain.ErrMarketFunds
	}
	earnedTaken := min(amount, balance.EarnedSilk)
	balance.EarnedSilk -= earnedTaken
	balance.Silk -= amount - earnedTaken
	if _, err := tx.Exec("UPDATE mall_accounts SET silk = ? WHERE account_id = ?", balance.Silk, accountID); err != nil {
		return domain.MallBalance{}, err
	}
	if earnedTaken > 0 {
		if err := setEarnedSilkTx(tx, accountID, balance.EarnedSilk); err != nil {
			return domain.MallBalance{}, err
		}
	}
	seq, err := insertSilkHistoryTx(tx, accountID, atMs, silkHistoryUse, domain.MallBalance{Silk: amount})
	if err != nil {
		return domain.MallBalance{}, err
	}
	if err := insertSilkHistoryEarnedTx(tx, seq, earnedTaken); err != nil {
		return domain.MallBalance{}, err
	}
	return balance, nil
}
