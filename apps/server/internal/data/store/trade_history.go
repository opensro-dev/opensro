/*
===========================================================================

trade_history.go - one row per party per committed trade (layout 13)

Port-only, not native: the original keeps no record of a player-to-player
exchange or a stall sale (domain/trade_history.go). The community account
page lists these rows, newest first, for the signed-in account.

A trade's rows are noted inside the trade's own commit door: the closure
that moves the gold and the items calls NoteTrade last, before it reports
success. NoteTrade takes only the pending list's own lock, never the store
lock, so it can run inside that closure. The rows then ride the commit's
transaction (persistence.go), so a trade and its records are durable together
or not at all. A failed commit keeps the rows pending, as economy_ledger.go
keeps its rows, and the next commit that succeeds writes each row exactly once.

Rows older than tradeHistoryRetention are dropped by the same commits that
write new ones, a bounded batch at a time (pruneTradeHistoryTx), so the
table cannot grow without end. The original keeps no record at all, so
the window is the port's: ninety days (tradeHistoryRetention, the owner's
choice on 2026-11-16).

The read side answers from the table, never from the roster. A counterparty's
current privacy choice is applied by the read side (publicstats): TradeHistory
reports whether the other character is hidden now, and the name it returns is
the one stored at the trade.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"opensro.online/server/internal/domain"
)

// tradeHistorySchema is layout 13's addition (authority_upgrade.go adds it
// to a layout 12 authority). The items columns hold a JSON array of
// {refObjId, quantity} per side; no item body is ever stored.
const tradeHistorySchema = `
CREATE TABLE IF NOT EXISTS trade_history (
  seq              INTEGER PRIMARY KEY,
  at_ms            INTEGER NOT NULL,
  division         TEXT    NOT NULL,
  account_id       TEXT    NOT NULL,
  char_id          INTEGER NOT NULL,
  char_name        TEXT    NOT NULL,
  other_account_id TEXT    NOT NULL,
  other_char_id    INTEGER NOT NULL,
  other_name       TEXT    NOT NULL,
  kind             TEXT    NOT NULL,
  gold_paid        INTEGER NOT NULL,
  gold_received    INTEGER NOT NULL,
  commission       INTEGER NOT NULL,
  items_given      TEXT    NOT NULL,
  items_received   TEXT    NOT NULL
);
CREATE INDEX IF NOT EXISTS trade_history_account_at ON trade_history (account_id, at_ms);
CREATE INDEX IF NOT EXISTS trade_history_at ON trade_history (at_ms);
`

const (
	// tradeHistoryRetention is how long a trade row is kept.
	tradeHistoryRetention = 90 * 24 * time.Hour
	// tradeHistoryPruneBatch bounds the rows one commit deletes, so a large
	// backlog (a retention change, a long-idle shard) drains over several
	// commits instead of stalling one.
	tradeHistoryPruneBatch = 500
)

// maxTradeGold is the largest gold amount a row stores: SQLite's INTEGER is
// signed 64-bit, and a gold amount above it is refused, not wrapped.
const maxTradeGold = math.MaxInt64

// ErrTradeCharacterNotOwned answers a TradeHistory read naming a character
// the account does not own (or no character at all of that name): the same
// answer for both, so the read reveals nothing about other accounts.
var ErrTradeCharacterNotOwned = errors.New("trade history: character not on this account")

/*
================
tradeHistoryState

pending holds the rows of trades whose commit has not succeeded yet, in the
order they were noted. mu guards pending alone: the note runs inside a
commit door, which already holds the store lock, so the note must not take it.
================
*/
type tradeHistoryState struct {
	mu      sync.Mutex
	pending []domain.TradeRecord
}

/*
================
validateTradeHistory

Startup reads the table's shape without scanning it: the history grows with
play and its rows are never decoded into the authority graph.
================
*/
func validateTradeHistory(db *sql.DB) error {
	rows, err := db.Query("SELECT seq, at_ms, division, account_id, char_id, char_name, other_account_id, other_char_id, other_name, kind, gold_paid, gold_received, commission, items_given, items_received FROM trade_history LIMIT 0")
	if err != nil {
		return err
	}
	defer rows.Close()
	return rows.Err()
}

/*
================
validateTradeRecord

A row the store can hold: a known kind, accounts the store could have
created, and gold that fits the column.
================
*/
func validateTradeRecord(record domain.TradeRecord) error {
	switch record.Kind {
	case domain.TradeKindExchange, domain.TradeKindStallBuy, domain.TradeKindStallSell,
		domain.TradeKindStallNetworkBuy, domain.TradeKindStallNetworkSell:
	default:
		return fmt.Errorf("trade history: unknown kind %q", record.Kind)
	}
	if !domain.AccountIDValid(record.AccountID) || !domain.AccountIDValid(record.OtherAccountID) {
		return fmt.Errorf("trade history: a party has no valid account")
	}
	if record.GoldPaid > maxTradeGold || record.GoldReceived > maxTradeGold || record.Commission > maxTradeGold {
		return fmt.Errorf("trade history: gold outside the stored range")
	}
	return nil
}

/*
================
NoteTrade

Notes a committed trade's rows (one per party) for the commit that carries
them. Called from inside the trade's commit door, so the rows join that
transaction. Every row is stamped with the store's clock. A refused row
refuses the whole note, so a trade's two rows are noted together or not at
all. The caller logs a refusal: the trade has already happened, and a history
write must never undo it.
================
*/
func (s *Store) NoteTrade(records []domain.TradeRecord) error {
	for _, record := range records {
		if err := validateTradeRecord(record); err != nil {
			return err
		}
	}
	nowMs := s.now().UnixMilli()
	s.trades.mu.Lock()
	defer s.trades.mu.Unlock()
	for _, record := range records {
		record.AtMs = nowMs
		s.trades.pending = append(s.trades.pending, record)
	}
	return nil
}

/*
================
snapshotPendingTrades

A copy of the pending rows, for the commit about to write them. The commit
forgets exactly this many (forgetPendingTrades) once it succeeds.
================
*/
func (s *Store) snapshotPendingTrades() []domain.TradeRecord {
	s.trades.mu.Lock()
	defer s.trades.mu.Unlock()
	return append([]domain.TradeRecord(nil), s.trades.pending...)
}

/*
================
forgetPendingTrades

Drops the first n pending rows, which a successful commit wrote. Rows noted
after the snapshot stay pending.
================
*/
func (s *Store) forgetPendingTrades(n int) {
	s.trades.mu.Lock()
	defer s.trades.mu.Unlock()
	if n >= len(s.trades.pending) {
		s.trades.pending = nil
		return
	}
	s.trades.pending = append([]domain.TradeRecord(nil), s.trades.pending[n:]...)
}

/*
================
encodeTradeItems

The JSON array an items column holds. A nil list is an empty array, so the
column is always a well-formed array.
================
*/
func encodeTradeItems(items []domain.TradeItem) (string, error) {
	if items == nil {
		items = []domain.TradeItem{}
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

/*
================
flushTradeHistoryTx

Writes the pending rows inside tx, the commit's transaction. Any failure fails
the commit, and the rows stay pending for the next one.
================
*/
func flushTradeHistoryTx(tx sqlExecer, records []domain.TradeRecord) error {
	for _, record := range records {
		given, err := encodeTradeItems(record.ItemsGiven)
		if err != nil {
			return fmt.Errorf("trade history items: %w", err)
		}
		received, err := encodeTradeItems(record.ItemsReceived)
		if err != nil {
			return fmt.Errorf("trade history items: %w", err)
		}
		if _, err := tx.Exec(`INSERT INTO trade_history (at_ms, division, account_id, char_id, char_name, other_account_id, other_char_id, other_name, kind, gold_paid, gold_received, commission, items_given, items_received)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			record.AtMs, record.Division, record.AccountID, record.CharID, record.CharName,
			record.OtherAccountID, record.OtherCharID, record.OtherName, record.Kind,
			int64(record.GoldPaid), int64(record.GoldReceived), int64(record.Commission), given, received); err != nil {
			return fmt.Errorf("trade history: %w", err)
		}
	}
	return nil
}

/*
================
pruneTradeHistoryTx

Deletes at most tradeHistoryPruneBatch rows older than the retention
window, inside the commit's transaction. The at_ms index keeps it a range
read, so a commit with nothing to drop pays one index probe.
================
*/
func pruneTradeHistoryTx(tx sqlExecer, now time.Time) error {
	cutoff := now.Add(-tradeHistoryRetention).UnixMilli()
	if _, err := tx.Exec(`DELETE FROM trade_history WHERE seq IN (SELECT seq FROM trade_history WHERE at_ms < ? ORDER BY at_ms LIMIT ?)`,
		cutoff, tradeHistoryPruneBatch); err != nil {
		return fmt.Errorf("trade history prune: %w", err)
	}
	return nil
}

/*
================
decodeTradeItems

A stored items column back into its stacks.
================
*/
func decodeTradeItems(raw string) ([]domain.TradeItem, error) {
	var items []domain.TradeItem
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("trade history items: %w", err)
	}
	return items, nil
}

/*
================
TradeHistory

One page of the division's trades for accountID, newest first: every trade
of the account, or only characterName's when it is set. characterName must
be one of the account's characters in the division, or the read answers
ErrTradeCharacterNotOwned. before, when set, is the page boundary: only rows
strictly older than it in (AtMs, Seq) order. The page holds at most limit
rows and says whether older ones remain. Each entry reports whether its
counterparty's character is hidden now. Takes the read lock: a reader never
holds off a commit's write of the rows it reads.
================
*/
func (s *Store) TradeHistory(division, accountID, characterName string, before *domain.TradeCursor, limit int) (domain.TradePage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil || accountID == "" {
		return domain.TradePage{}, fmt.Errorf("trade history: store unavailable")
	}
	if limit < 1 {
		return domain.TradePage{}, fmt.Errorf("trade history: invalid limit %d", limit)
	}
	index := s.characterLookup(division)
	query := `SELECT seq, at_ms, division, account_id, char_id, char_name, other_account_id, other_char_id, other_name, kind, gold_paid, gold_received, commission, items_given, items_received
FROM trade_history WHERE division = ? AND account_id = ?`
	args := []any{division, accountID}
	if characterName != "" {
		character := index.names[characterLookupName(characterName)]
		if character == nil || character.AccountID != accountID {
			return domain.TradePage{}, ErrTradeCharacterNotOwned
		}
		query += " AND char_id = ?"
		args = append(args, character.ID)
	}
	if before != nil {
		query += " AND (at_ms < ? OR (at_ms = ? AND seq < ?))"
		args = append(args, before.AtMs, before.AtMs, before.Seq)
	}
	query += " ORDER BY at_ms DESC, seq DESC LIMIT ?"
	args = append(args, limit+1)
	rows, err := s.syncDBLocked().Query(query, args...)
	if err != nil {
		return domain.TradePage{}, err
	}
	defer rows.Close()
	entries := []domain.TradeEntry{}
	for rows.Next() {
		entry, err := scanTradeEntry(rows)
		if err != nil {
			return domain.TradePage{}, err
		}
		// A counterparty no longer on the roster (reaped) reads as hidden:
		// its choice cannot be checked, and its name may be someone's now.
		if other := index.ids[entry.Record.OtherCharID]; other == nil || other.PublicHidden {
			entry.OtherHidden = true
		}
		entries = append(entries, entry)
	}
	if err := rows.Err(); err != nil {
		return domain.TradePage{}, err
	}
	page := domain.TradePage{Entries: entries}
	if len(entries) > limit {
		page.Entries = entries[:limit]
		page.More = true
	}
	return page, nil
}

/*
================
scanTradeEntry

The row under the cursor, decoded. Gold and items are refused when they hold
values this package never writes (a negative gold, an unparsable items array).
================
*/
func scanTradeEntry(rows *sql.Rows) (domain.TradeEntry, error) {
	var entry domain.TradeEntry
	var goldPaid, goldReceived, commission int64
	var given, received string
	if err := rows.Scan(&entry.Seq, &entry.Record.AtMs, &entry.Record.Division, &entry.Record.AccountID,
		&entry.Record.CharID, &entry.Record.CharName, &entry.Record.OtherAccountID, &entry.Record.OtherCharID,
		&entry.Record.OtherName, &entry.Record.Kind, &goldPaid, &goldReceived, &commission, &given, &received); err != nil {
		return domain.TradeEntry{}, err
	}
	if goldPaid < 0 || goldReceived < 0 || commission < 0 {
		return domain.TradeEntry{}, fmt.Errorf("trade history %d: negative gold", entry.Seq)
	}
	entry.Record.GoldPaid = uint64(goldPaid)
	entry.Record.GoldReceived = uint64(goldReceived)
	entry.Record.Commission = uint64(commission)
	var err error
	if entry.Record.ItemsGiven, err = decodeTradeItems(given); err != nil {
		return domain.TradeEntry{}, err
	}
	if entry.Record.ItemsReceived, err = decodeTradeItems(received); err != nil {
		return domain.TradeEntry{}, err
	}
	return entry, nil
}
