/*
===========================================================================

betasilk.go - beta silk credits into the account's real mall wallet

The closed beta's earned silk (action/betasilk.go, port-only, not native) is
credited into mall_accounts.silk, the same persisted wallet the Item Mall
debits. It therefore survives restarts and deploys, and switching the beta
off stops new credits but leaves what was earned: credited silk is real.

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
GrantBetaSilkStarter

Create the account's wallet with the starter silk when it has none yet. The
row's existence is the persisted marker, so the grant happens once per
account however often the process restarts. Reports whether it granted.
================
*/
func (s *Store) GrantBetaSilkStarter(accountID string, starter uint32) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accountID == "" || s.db == nil {
		return false, fmt.Errorf("beta silk: account unavailable")
	}
	// A direct write lands only after the tick's deferred rows (tickbatch.go).
	if err := s.flushDeferredLocked(); err != nil {
		return false, err
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, ?, 0, 0)
ON CONFLICT(account_id) DO NOTHING`, accountID, starter)
	if err != nil {
		return false, err
	}
	added, err := result.RowsAffected()
	if err != nil || added != 1 {
		return added == 1, err
	}
	// The starter silk is a charge like any other credit (silkhistory.go):
	// it lands in the history with the wallet row that creates it.
	if err := insertSilkHistoryTx(tx, accountID, s.now().UnixMilli(), silkHistoryCharge, domain.MallBalance{Silk: starter}); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

/*
================
CreditBetaSilk

Add amount to the account's silk while it is below bankCap, never past it.
At or above the cap nothing changes and credited is false. One transaction,
so a purchase can never interleave between the read and the write.
================
*/
func (s *Store) CreditBetaSilk(accountID string, amount, bankCap uint32) (domain.MallBalance, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accountID == "" || s.db == nil {
		return domain.MallBalance{}, false, fmt.Errorf("beta silk: account unavailable")
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return domain.MallBalance{}, false, err
	}
	defer func() { _ = tx.Rollback() }()
	var balance domain.MallBalance
	err = tx.QueryRow("SELECT silk, gift_silk, points FROM mall_accounts WHERE account_id = ?", accountID).
		Scan(&balance.Silk, &balance.GiftSilk, &balance.Points)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return domain.MallBalance{}, false, err
	}
	if balance.Silk >= bankCap || amount == 0 {
		return balance, false, nil
	}
	before := balance.Silk
	balance.Silk = uint32(min(uint64(balance.Silk)+uint64(amount), uint64(bankCap)))
	_, err = tx.Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET silk = excluded.silk`, accountID, balance.Silk, balance.GiftSilk, balance.Points)
	if err != nil {
		return domain.MallBalance{}, false, err
	}
	// Only what actually landed is a charge (silkhistory.go): the cap can
	// trim the credit below the amount asked for.
	if err := insertSilkHistoryTx(tx, accountID, s.now().UnixMilli(), silkHistoryCharge, domain.MallBalance{Silk: balance.Silk - before}); err != nil {
		return domain.MallBalance{}, false, err
	}
	return balance, true, tx.Commit()
}
