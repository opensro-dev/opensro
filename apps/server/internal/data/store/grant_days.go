/*
===========================================================================

grant_days.go - free grants per account per day (#3307)

The 11-12 live investigation asked how much an account had been handed
for nothing, and the stores could not say: the item ledger is keyed by
character, the account lives only in the character's record, and a
character deleted after selling its kit takes the evidence into the
archive. This table keeps the answer where the question asks it: per
account, UTC day, grant and item, the total granted and how many times.

It is written at grant time, inside the transaction that grants:
  - item grants (domain.FreeGrantDoor: the starter kit and its refill)
    fold from the item ledger's own rows at flushItemLedgerTx, so they
    share its guarantee - a failed commit keeps the rows pending and the
    next commit writes them, and their day, exactly once;
  - silk grants (starter, beta, referral) note beside the silk history
    row their writer already adds (betasilk.go, referrals.go).

Only gains count: a grant never takes an item away.

Additive like the item ledger's tables: created at open, no schema
stamp, an older binary ignores it. No retention, as the ledgers keep
none; one row per account, day, grant and item stays small.

Port-only, not native: the original keeps no such record.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
)

// grantDayMs is a bucket's width: one UTC day.
const grantDayMs = 24 * 60 * 60 * 1000

// The item names the silk grants record under.
const (
	grantItemSilk     = "silk"
	grantItemGiftSilk = "gift-silk"
)

// grantDaySchema is the day table and its per-account index.
const grantDaySchema = `
CREATE TABLE IF NOT EXISTS grant_account_days (
  day_ms     INTEGER NOT NULL,
  account_id TEXT    NOT NULL,
  source     TEXT    NOT NULL,
  item       TEXT    NOT NULL,
  total      INTEGER NOT NULL,
  n          INTEGER NOT NULL,
  PRIMARY KEY (day_ms, account_id, source, item)
);
CREATE INDEX IF NOT EXISTS grant_account_days_account ON grant_account_days (account_id, day_ms);
`

/*
================
ensureGrantDays

Creates the day table when the store opens.
================
*/
func ensureGrantDays(db *sql.DB) error {
	if _, err := db.Exec(grantDaySchema); err != nil {
		return fmt.Errorf("grant days: %w", err)
	}
	return nil
}

/*
================
FreeGrantDayCount

How many grants of one source an account received on the UTC day
containing atMs (the n column: grants, not units). The EventBot host
reads its daily win cap from the same rows its audit writes
(social/eventbot, #3533).
================
*/
func (s *Store) FreeGrantDayCount(accountID, source string, atMs int64) (int64, error) {
	dayMs := atMs - atMs%grantDayMs
	var n sql.NullInt64
	err := s.db.QueryRow(`SELECT SUM(n) FROM grant_account_days WHERE day_ms = ? AND account_id = ? AND source = ?`,
		dayMs, accountID, source).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("grant days: %w", err)
	}
	return n.Int64, nil
}

/*
================
addGrantDayTx

Adds one grant of total to the account's day, inside the granting
transaction. A grant with no account or nothing granted writes nothing.
================
*/
func addGrantDayTx(tx sqlExecer, atMs int64, account, source, item string, total int64) error {
	if account == "" || total <= 0 {
		return nil
	}
	if _, err := tx.Exec(`INSERT INTO grant_account_days (day_ms, account_id, source, item, total, n)
VALUES (?, ?, ?, ?, ?, 1)
ON CONFLICT (day_ms, account_id, source, item) DO UPDATE SET
  total = total + excluded.total, n = n + 1`,
		atMs-atMs%grantDayMs, account, source, item, total); err != nil {
		return fmt.Errorf("grant days: %w", err)
	}
	return nil
}
