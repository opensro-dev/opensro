/*
===========================================================================

itemmall.go - atomic account currency debits and mall item delivery

The character pointer remains stable. SQL commits the account debit and new
inventory together before live state is changed or a success can be sent.

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
MallBalance
================
*/
func (s *Store) MallBalance(character *domain.Character) (domain.MallBalance, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if character == nil || character.AccountID == "" || s.db == nil {
		return domain.MallBalance{}, fmt.Errorf("mall: account unavailable")
	}
	if _, known := s.charDivision[character]; !known {
		return domain.MallBalance{}, fmt.Errorf("mall: unknown character")
	}
	return readMallBalance(s.syncDBLocked(), character.AccountID)
}

// mallBalanceQuery is the one wallet read: granted silk, gift silk and
// points from the account row, the earned half joined from its own
// table - an absent earned row reads as zero, the pre-split migration.
const mallBalanceQuery = `SELECT a.silk, a.gift_silk, a.points, COALESCE(e.earned_silk, 0) FROM mall_accounts a
LEFT JOIN mall_earned_silk e ON e.account_id = a.account_id WHERE a.account_id = ?`

/*
================
scanMallBalance

Read one account's wallet through whichever handle the caller holds
(a database or its transaction). An account without a wallet row has
never held game currency. Read failures must remain errors rather
than being presented as a zero balance.
================
*/
func scanMallBalance(handle interface {
	QueryRow(query string, args ...any) *sql.Row
}, account string) (domain.MallBalance, error) {
	var balance domain.MallBalance
	err := handle.QueryRow(mallBalanceQuery, account).
		Scan(&balance.Silk, &balance.GiftSilk, &balance.Points, &balance.EarnedSilk)
	if errors.Is(err, sql.ErrNoRows) {
		return balance, nil
	}
	return balance, err
}

/*
================
readMallBalance
================
*/
func readMallBalance(db *sql.DB, account string) (domain.MallBalance, error) {
	return scanMallBalance(db, account)
}

/*
================
readMallBalanceTx

scanMallBalance inside a transaction: the live balances a debit splits
against, so a concurrent credit or purchase cannot interleave.
================
*/
func readMallBalanceTx(tx *sql.Tx, account string) (domain.MallBalance, error) {
	return scanMallBalance(tx, account)
}

/*
================
setEarnedSilkTx

Write the account's earned balance inside tx, creating its row on the
first earned movement. Only callers that moved earned silk call this:
a wallet that never earned keeps no row and reads as zero.
================
*/
func setEarnedSilkTx(tx *sql.Tx, account string, earned uint32) error {
	_, err := tx.Exec(`INSERT INTO mall_earned_silk (account_id, earned_silk) VALUES (?, ?)
ON CONFLICT(account_id) DO UPDATE SET earned_silk = excluded.earned_silk`, account, earned)
	return err
}

/*
================
validateMallAccounts

Validate every stored balance at boot, including accounts without characters.
================
*/
func validateMallAccounts(db *sql.DB) error {
	earned, err := db.Query("SELECT account_id, earned_silk FROM mall_earned_silk LIMIT 0")
	if err != nil {
		return err
	}
	// The probe only proves the table's shape; close before the real
	// query, as the store's single connection cannot serve both.
	if err := earned.Err(); err != nil {
		earned.Close()
		return err
	}
	earned.Close()
	rows, err := db.Query("SELECT account_id, silk, gift_silk, points FROM mall_accounts")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var account string
		var balance domain.MallBalance
		if err := rows.Scan(&account, &balance.Silk, &balance.GiftSilk, &balance.Points); err != nil {
			return err
		}
		if account == "" {
			return fmt.Errorf("mall: wallet without account identity")
		}
	}
	return rows.Err()
}

/*
================
PurchaseMall

The silk charge draws the earned balance first (#4133): earnedTaken is
what earned silk pays, the rest of the silk charge falls to granted
silk. earnedOnly - a silk-only price the earned balance covers whole -
is the one fact the grant needs, because it sets the delivered items'
bound flag. Gift silk and points never make a purchase earned.
================
*/
func (s *Store) PurchaseMall(character *domain.Character, cost domain.MallBalance, grant func(rows []domain.InventoryRow, earnedOnly bool) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if character == nil || character.AccountID == "" || character.DeletePending || grant == nil || s.db == nil {
		return domain.MallBalance{}, fmt.Errorf("mall: purchase unavailable")
	}
	division, known := s.charDivision[character]
	if !known {
		return domain.MallBalance{}, fmt.Errorf("mall: unknown character")
	}
	balance, err := readMallBalance(s.syncDBLocked(), character.AccountID)
	if err != nil {
		return balance, err
	}
	if uint64(cost.Silk) > uint64(balance.Silk)+uint64(balance.EarnedSilk) || cost.GiftSilk > balance.GiftSilk || cost.Points > balance.Points {
		return balance, domain.MallInsufficientCurrency{}
	}
	earnedTaken := min(cost.Silk, balance.EarnedSilk)
	earnedOnly := cost.Silk > 0 && cost.GiftSilk == 0 && cost.Points == 0 && earnedTaken == cost.Silk
	snapshot := character.Snapshot()
	nextInventory, err := grant(snapshot.MissionInventory, earnedOnly)
	if err != nil {
		return balance, err
	}
	next := *character
	next.MissionInventory = nextInventory
	remaining := domain.MallBalance{
		Silk: balance.Silk - (cost.Silk - earnedTaken), EarnedSilk: balance.EarnedSilk - earnedTaken,
		GiftSilk: balance.GiftSilk - cost.GiftSilk, Points: balance.Points - cost.Points,
	}
	if err := s.commitMallPurchaseLocked(division, &next, remaining, cost, earnedTaken); err != nil {
		s.recordWriteFailureLocked("mall-purchase", err)
		return balance, err
	}
	character.MissionInventory = nextInventory
	if s.changes.empty() {
		s.recordWriteSuccessLocked()
	}
	return remaining, nil
}

/*
================
commitMallPurchaseLocked

Keep failed ordinary gameplay writes pending. This transaction changes only
the mall's account row and the current complete character record. A purchase
that charged silk also appends its silk-history row here (silkhistory.go),
so the debit and its record are durable together or not at all.
================
*/
func (s *Store) commitMallPurchaseLocked(division string, character *domain.Character, balance domain.MallBalance, cost domain.MallBalance, earnedTaken uint32) error {
	if s.commitFail != nil {
		return s.commitFail
	}
	tx, err := s.beginCharacterTxLocked("mall-purchase")
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`INSERT INTO mall_accounts (account_id, silk, gift_silk, points) VALUES (?, ?, ?, ?)
ON CONFLICT(account_id) DO UPDATE SET silk = excluded.silk, gift_silk = excluded.gift_silk, points = excluded.points`,
		character.AccountID, balance.Silk, balance.GiftSilk, balance.Points)
	if err != nil {
		return err
	}
	if earnedTaken > 0 {
		if err := setEarnedSilkTx(tx.Tx, character.AccountID, balance.EarnedSilk); err != nil {
			return err
		}
	}
	if uint64(cost.Silk)+uint64(cost.GiftSilk) > 0 {
		seq, err := insertSilkHistoryTx(tx.Tx, character.AccountID, s.now().UnixMilli(), silkHistoryUse, cost)
		if err != nil {
			return err
		}
		if err := insertSilkHistoryEarnedTx(tx.Tx, seq, earnedTaken); err != nil {
			return err
		}
	}
	if err := tx.upsertCharacter(division, character); err != nil {
		return err
	}
	return tx.Commit()
}
