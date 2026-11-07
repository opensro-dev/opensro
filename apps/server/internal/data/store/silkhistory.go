/*
===========================================================================

silkhistory.go - one row per silk wallet movement (layout 12)

Port-only, not native: the v1.150 client ships the Item Mall's silk history
window (its "Buying list") but the original never records a
charge, so the window can never list anything. This append-only table is
the record the port's window reads: every durable silk credit (a charge)
and every mall debit (a use) lands here in the same transaction that moves
the wallet, so a movement and its row are durable together or not at all.

Rows are account-scoped because the wallet is: any character of the account
sees the same history. Silk gifted between players has no flow in the port
yet; when one lands it records here as its own kind.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"

	"opensro.online/server/internal/domain"
)

// silkHistorySchema is layout 12's addition (authority_upgrade.go adds it
// to a layout 11 authority).
const silkHistorySchema = `
CREATE TABLE IF NOT EXISTS silk_history (
  seq       INTEGER PRIMARY KEY,
  account_id TEXT    NOT NULL,
  at_ms     INTEGER NOT NULL,
  kind      INTEGER NOT NULL,
  silk      INTEGER NOT NULL,
  gift_silk INTEGER NOT NULL,
  points    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS silk_history_account_at ON silk_history (account_id, at_ms);
`

// The recorded movement kinds. A use is a mall purchase debit; a charge is
// a credit into the wallet (operator grant, beta earnings, starter silk).
const (
	silkHistoryUse = iota
	silkHistoryCharge
)

// One day in milliseconds: the inquiry periods are whole days (1, 7, 30
// and 90 for the native combo's 1 day / 7 days / 1 month / 3 months).
const silkHistoryDayMs = 24 * 60 * 60 * 1000

/*
================
insertSilkHistoryTx

Append one movement row inside the transaction that moves the wallet.
Amounts are the movement's own fields, not the balances around it.
================
*/
func insertSilkHistoryTx(tx *sql.Tx, account string, atMs int64, kind int, amount domain.MallBalance) error {
	if _, err := tx.Exec(`INSERT INTO silk_history (account_id, at_ms, kind, silk, gift_silk, points)
VALUES (?, ?, ?, ?, ?, ?)`, account, atMs, kind, amount.Silk, amount.GiftSilk, amount.Points); err != nil {
		return fmt.Errorf("silk history: %w", err)
	}
	return nil
}

/*
================
SilkHistory

The account's silk uses inside the last periodDays days, newest first.
The total charges every use in the period, including rows past the limit,
so the window's Total line is the period's whole spending.
================
*/
func (s *Store) SilkHistory(character *domain.Character, periodDays int, limit int) ([]domain.SilkHistoryRow, uint32, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if character == nil || character.AccountID == "" || s.db == nil {
		return nil, 0, fmt.Errorf("silk history: account unavailable")
	}
	if _, known := s.charDivision[character]; !known {
		return nil, 0, fmt.Errorf("silk history: unknown character")
	}
	if periodDays < 1 || limit < 1 {
		return nil, 0, fmt.Errorf("silk history: invalid period or limit")
	}
	since := s.now().UnixMilli() - int64(periodDays)*silkHistoryDayMs
	var total uint64
	err := s.db.QueryRow(`SELECT COALESCE(SUM(silk + gift_silk), 0) FROM silk_history
WHERE account_id = ? AND kind = ? AND at_ms >= ?`, character.AccountID, silkHistoryUse, since).Scan(&total)
	if err != nil {
		return nil, 0, err
	}
	found, err := s.db.Query(`SELECT at_ms, silk, gift_silk FROM silk_history
WHERE account_id = ? AND kind = ? AND at_ms >= ? ORDER BY at_ms DESC, seq DESC LIMIT ?`,
		character.AccountID, silkHistoryUse, since, limit)
	if err != nil {
		return nil, 0, err
	}
	defer found.Close()
	rows := []domain.SilkHistoryRow{}
	for found.Next() {
		var row domain.SilkHistoryRow
		var silk, gift uint32
		if err := found.Scan(&row.AtMs, &silk, &gift); err != nil {
			return nil, 0, err
		}
		row.Charged = silk + gift
		rows = append(rows, row)
	}
	if err := found.Err(); err != nil {
		return nil, 0, err
	}
	return rows, uint32(total), nil
}

/*
================
validateSilkHistory

Startup reads the table's shape without scanning it: the history grows
with play and its rows are never decoded into the authority graph.
================
*/
func validateSilkHistory(db *sql.DB) error {
	rows, err := db.Query("SELECT seq, account_id, at_ms, kind, silk, gift_silk, points FROM silk_history LIMIT 0")
	if err != nil {
		return err
	}
	defer rows.Close()
	return rows.Err()
}
