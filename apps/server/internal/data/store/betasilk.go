/*
===========================================================================

betasilk.go - beta silk credits into the account's real mall wallet

The closed beta's earned silk (action/betasilk.go, port-only, not native) is
credited into the wallet's earned balance (mall_earned_silk, #4133), the
same persisted wallet the Item Mall debits - earned first. It therefore
survives restarts and deploys, and switching the beta off stops new credits
but leaves what was earned: credited silk is real.

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
	atMs := s.now().UnixMilli()
	if _, err := insertSilkHistoryTx(tx, accountID, atMs, silkHistoryCharge, domain.MallBalance{Silk: starter}); err != nil {
		return false, err
	}
	if err := addGrantDayTx(tx, atMs, accountID, domain.GrantStarterSilk, grantItemSilk, int64(starter)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

/*
================
CreditBetaSilk

Add amount to the account's earned silk (#4133: playtime credits are
the earned balance) while the wallet's whole silk - earned and granted
together - is below bankCap, never past it. At or above the cap nothing
changes and credited is false. One transaction, so a purchase can never
interleave between the read and the write. progressMs is the hour
clock's remainder after this payment (#4429): it is written in the same
transaction, so a restart can neither pay the hour twice nor lose the
time past it.
================
*/
func (s *Store) CreditBetaSilk(accountID string, amount, bankCap uint32, progressMs int64) (domain.MallBalance, bool, error) {
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
	balance, err := readMallBalanceTx(tx, accountID)
	if err != nil {
		return domain.MallBalance{}, false, err
	}
	if uint64(balance.Silk)+uint64(balance.EarnedSilk) >= uint64(bankCap) || amount == 0 {
		return balance, false, nil
	}
	credited := uint32(min(uint64(amount), uint64(bankCap)-uint64(balance.Silk)-uint64(balance.EarnedSilk)))
	balance.EarnedSilk += credited
	// The mall_accounts upsert only anchors the wallet row for an
	// account that earned before it ever held one; its granted columns
	// are written back unchanged.
	_, err = tx.Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET silk = excluded.silk`, accountID, balance.Silk, balance.GiftSilk, balance.Points)
	if err != nil {
		return domain.MallBalance{}, false, err
	}
	if err := setEarnedSilkTx(tx, accountID, balance.EarnedSilk); err != nil {
		return domain.MallBalance{}, false, err
	}
	// Only what actually landed is a charge (silkhistory.go): the cap can
	// trim the credit below the amount asked for.
	atMs := s.now().UnixMilli()
	seq, err := insertSilkHistoryTx(tx, accountID, atMs, silkHistoryCharge, domain.MallBalance{Silk: credited})
	if err != nil {
		return domain.MallBalance{}, false, err
	}
	if err := insertSilkHistoryEarnedTx(tx, seq, credited); err != nil {
		return domain.MallBalance{}, false, err
	}
	if err := addGrantDayTx(tx, atMs, accountID, domain.GrantBetaSilk, grantItemSilk, int64(credited)); err != nil {
		return domain.MallBalance{}, false, err
	}
	if err := setBetaSilkProgressTx(tx, accountID, progressMs); err != nil {
		return domain.MallBalance{}, false, err
	}
	return balance, true, tx.Commit()
}

/*
================
BetaSilkProgress

The account's persisted uncredited in-world time toward its next hourly
credit (layout 16, #4429). An account without a row has none.
================
*/
func (s *Store) BetaSilkProgress(accountID string) (int64, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if accountID == "" || s.db == nil {
		return 0, fmt.Errorf("beta silk: account unavailable")
	}
	var earnedMs int64
	err := s.syncDBLocked().QueryRow("SELECT earned_ms FROM beta_silk_progress WHERE account_id = ?", accountID).Scan(&earnedMs)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return earnedMs, err
}

/*
================
SaveBetaSilkProgress

Record the account's uncredited in-world time. The clock calls it every
betaSilkSaveStepMs of accrual and when the time falls (a cap), so a
restart loses at most one step (action/betasilk.go).
================
*/
func (s *Store) SaveBetaSilkProgress(accountID string, earnedMs int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if accountID == "" || s.db == nil {
		return fmt.Errorf("beta silk: account unavailable")
	}
	tx, err := s.beginTxLocked()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := setBetaSilkProgressTx(tx, accountID, earnedMs); err != nil {
		return err
	}
	return tx.Commit()
}

/*
================
setBetaSilkProgressTx
================
*/
func setBetaSilkProgressTx(tx *sql.Tx, accountID string, earnedMs int64) error {
	_, err := tx.Exec(`INSERT INTO beta_silk_progress (account_id, earned_ms) VALUES (?, ?)
ON CONFLICT(account_id) DO UPDATE SET earned_ms = excluded.earned_ms`, accountID, max(earnedMs, 0))
	return err
}
