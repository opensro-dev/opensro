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
	return readMallBalance(s.db, character.AccountID)
}

/*
================
readMallBalance

An account without a wallet row has never held game currency. Read failures
must remain errors rather than being presented as a zero balance.
================
*/
func readMallBalance(db *sql.DB, account string) (domain.MallBalance, error) {
	var balance domain.MallBalance
	err := db.QueryRow("SELECT silk, gift_silk, points FROM mall_accounts WHERE account_id = ?", account).
		Scan(&balance.Silk, &balance.GiftSilk, &balance.Points)
	if errors.Is(err, sql.ErrNoRows) {
		return balance, nil
	}
	return balance, err
}

/*
================
validateMallAccounts

Validate every stored balance at boot, including accounts without characters.
================
*/
func validateMallAccounts(db *sql.DB) error {
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
================
*/
func (s *Store) PurchaseMall(character *domain.Character, cost domain.MallBalance, grant func([]domain.InventoryRow) ([]domain.InventoryRow, error)) (domain.MallBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if character == nil || character.AccountID == "" || character.DeletePending || grant == nil || s.db == nil {
		return domain.MallBalance{}, fmt.Errorf("mall: purchase unavailable")
	}
	division, known := s.charDivision[character]
	if !known {
		return domain.MallBalance{}, fmt.Errorf("mall: unknown character")
	}
	balance, err := readMallBalance(s.db, character.AccountID)
	if err != nil {
		return balance, err
	}
	if cost.Silk > balance.Silk || cost.GiftSilk > balance.GiftSilk || cost.Points > balance.Points {
		return balance, domain.MallInsufficientCurrency{}
	}
	snapshot := character.Snapshot()
	nextInventory, err := grant(snapshot.MissionInventory)
	if err != nil {
		return balance, err
	}
	next := *character
	next.MissionInventory = nextInventory
	remaining := domain.MallBalance{Silk: balance.Silk - cost.Silk, GiftSilk: balance.GiftSilk - cost.GiftSilk, Points: balance.Points - cost.Points}
	if err := s.commitMallPurchaseLocked(division, &next, remaining); err != nil {
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
the mall's account row and the current complete character record.
================
*/
func (s *Store) commitMallPurchaseLocked(division string, character *domain.Character, balance domain.MallBalance) error {
	if s.commitFail != nil {
		return s.commitFail
	}
	tx, err := s.db.Begin()
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
	if err := upsertCharacterTx(tx, division, character); err != nil {
		return err
	}
	return tx.Commit()
}
