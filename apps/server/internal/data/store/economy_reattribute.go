/*
===========================================================================

economy_reattribute.go - gold rows recorded before their door was named

Port-only, not native (the economy ledger is). A door the reason table
did not know wrote its gold as "unattributed" (economy_ledger.go), and
the console showed it as an unknown kind: the So-Ok arena payouts reached
204,060,000 gold that way on the live shard. The row keeps its door, so
once the table names the door the row's reason can be told after the
fact. This pass runs once at every open, before the fold lane: it
renames each such row's reason, and for a row already summed into the
hourly buckets it moves the sum from the "unattributed" bucket to the
new reason's. A row not yet folded needs only the rename; the fold
picks up its new reason.

The old bucket keeps its low and high (a bound, not an exact figure,
once a row leaves it); one left with no rows is removed.

===========================================================================
*/

package store

import (
	"database/sql"
	"fmt"

	"opensro.online/server/internal/domain"
)

/*
================
reattributeLedger

Renames every unattributed gold row whose door now has a reason, and
moves the folded rows' sums between buckets, in one transaction. It
returns how many rows changed.
================
*/
func reattributeLedger(db *sql.DB) (int, error) {
	foldSeq, err := readMetaInt64(db, metaKeyEconomyFoldSeq, 0)
	if err != nil {
		return 0, err
	}
	rows, err := db.Query(`SELECT seq, at_ms, division, door, gold, COALESCE(bucketed, 0) FROM economy_ledger WHERE reason = ?`,
		domain.EconomyReasonUnattributed)
	if err != nil {
		return 0, err
	}
	type renamed struct {
		seq    int64
		reason string
	}
	var renames []renamed
	moved := map[ledgerBucketKey]*ledgerBucketFold{}
	left := map[ledgerBucketKey]*ledgerBucketFold{}
	for rows.Next() {
		var seq, atMs, gold, bucketed int64
		var division, door string
		if err := rows.Scan(&seq, &atMs, &division, &door, &gold, &bucketed); err != nil {
			rows.Close()
			return 0, err
		}
		reason := domain.EconomyReason(door, gold > 0)
		if reason == domain.EconomyReasonUnattributed {
			continue
		}
		renames = append(renames, renamed{seq: seq, reason: reason})
		if bucketed == 0 && seq > foldSeq {
			continue
		}
		hour := (atMs / economyHourMs) * economyHourMs
		from := ledgerBucketKey{hourMs: hour, division: division, reason: domain.EconomyReasonUnattributed, gain: gold > 0}
		to := from
		to.reason = reason
		addLedgerFold(left, from, gold)
		addLedgerFold(moved, to, gold)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	if len(renames) == 0 {
		return 0, nil
	}
	tx, err := db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	for _, r := range renames {
		if _, err := tx.Exec(`UPDATE economy_ledger SET reason = ? WHERE seq = ?`, r.reason, r.seq); err != nil {
			return 0, fmt.Errorf("reattribute row %d: %w", r.seq, err)
		}
	}
	for key, fold := range left {
		if _, err := tx.Exec(`UPDATE economy_ledger_hours SET total = total - ?, n = n - ?
WHERE hour_ms = ? AND division = ? AND reason = ? AND gain = ?`,
			fold.total, fold.n, key.hourMs, key.division, key.reason, key.gain); err != nil {
			return 0, fmt.Errorf("reattribute bucket: %w", err)
		}
	}
	if _, err := tx.Exec(`DELETE FROM economy_ledger_hours WHERE reason = ? AND n <= 0`, domain.EconomyReasonUnattributed); err != nil {
		return 0, err
	}
	if err := upsertLedgerBucketsTx(tx, moved, nil); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(renames), nil
}

/*
================
addLedgerFold

Adds one row's gold to a bucket fold.
================
*/
func addLedgerFold(folds map[ledgerBucketKey]*ledgerBucketFold, key ledgerBucketKey, gold int64) {
	fold := folds[key]
	if fold == nil {
		folds[key] = &ledgerBucketFold{total: gold, low: gold, high: gold, n: 1}
		return
	}
	fold.total += gold
	fold.low = min(fold.low, gold)
	fold.high = max(fold.high, gold)
	fold.n++
}
