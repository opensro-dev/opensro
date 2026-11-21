/*
===========================================================================

item_ledger.go - every item entering or leaving a character, at commit
(#3307)

Port-only, not native: the original keeps no item journal. The store
attributes every change itself, at the door boundary (ledger_door.go),
so no gameplay path needs a ledger call and none can be missed: a door
snapshots the row counts of the characters it changes
(domain.AppendItemRowCounts), and when it finishes each one whose
holdings differ (domain.FoldItemCounts, a count per item codename) notes
every item's signed change against the door. A door that moved no item
leaves the row counts equal and costs one linear comparison.

Moves inside one character (bag slots, a pet's bag, the body) leave the
holdings unchanged and write nothing. Every noted change joins the
transaction that writes the character, so a change and its record are
durable together or not at all; a failed commit keeps them pending, and
the next commit writes them once. A door with its own transaction
(characterRowTx) notes the copies it writes inside that transaction.

What is written: an exact row in item_ledger for every door the domain
does not name as bulk (grants, quest rewards, GM and operator doors, the
mall, exchange, stall, storage, market, drops, the forge, transfers - the
evidence a duplication case reads), and, for the bulk doors
(domain.ItemDoorBulk: consumption and loot), the per-character hourly
bucket item_ledger_char_hours instead. Both tables are additive, created
at open with no schema stamp (an older binary ignores them), like the
gold buckets (#3187).

Decisions recorded here: a deleted character writes no rows (the archive
keeps its record's bytes, and a restore brings the items back); the
warehouse and guild storage are not holders, so a deposit reads as the
character's loss under its door and a withdrawal as its gain, as on the
gold side; the exact rows have no retention, as the gold ledger's have
none - a fold like economy_fold.go follows if their volume asks for it.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"

	"opensro.online/server/internal/domain"
)

// itemLedgerSchema creates the item ledger's two tables: the exact rows
// and the bulk doors' hourly buckets.
const itemLedgerSchema = `
CREATE TABLE IF NOT EXISTS item_ledger (
  seq      INTEGER PRIMARY KEY,
  at_ms    INTEGER NOT NULL,
  division TEXT    NOT NULL,
  char_id  INTEGER NOT NULL,
  door     TEXT    NOT NULL,
  item     TEXT    NOT NULL,
  count    INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS item_ledger_char_at ON item_ledger (char_id, at_ms);
CREATE TABLE IF NOT EXISTS item_ledger_char_hours (
  hour_ms INTEGER NOT NULL,
  char_id INTEGER NOT NULL,
  door    TEXT    NOT NULL,
  item    TEXT    NOT NULL,
  total   INTEGER NOT NULL,
  n       INTEGER NOT NULL,
  PRIMARY KEY (hour_ms, char_id, door, item)
);
`

/*
================
ensureItemLedger

Creates the item ledger's tables when the store opens: additive, so no
stamp moves and an older binary on the same file never notices.
================
*/
func ensureItemLedger(db *sql.DB) error {
	if _, err := db.Exec(itemLedgerSchema); err != nil {
		return fmt.Errorf("item ledger: %w", err)
	}
	return nil
}

/*
================
validateItemLedger

Startup reads the tables' shape without scanning them, when a file
already holds them (one an older binary wrote has neither, and gains
both at this open).
================
*/
func validateItemLedger(db *sql.DB) error {
	var tables int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema
WHERE type = 'table' AND name IN ('item_ledger', 'item_ledger_char_hours')`).Scan(&tables); err != nil {
		return err
	}
	if tables == 0 {
		return nil
	}
	if tables != 2 {
		return fmt.Errorf("item ledger: one of its tables is missing")
	}
	for _, shape := range []string{
		"SELECT seq, at_ms, division, char_id, door, item, count FROM item_ledger LIMIT 0",
		"SELECT hour_ms, char_id, door, item, total, n FROM item_ledger_char_hours LIMIT 0",
	} {
		rows, err := db.Query(shape)
		if err != nil {
			return err
		}
		// One pooled connection: let go of it before the next shape asks.
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

/*
================
itemLedgerRow

One item's change through one door: count is signed.
================
*/
type itemLedgerRow struct {
	atMs     int64
	division string
	charID   int64
	door     string
	item     string
	count    int64
}

/*
================
itemLedger

pending holds the rows of doors whose commit has not succeeded yet.
================
*/
type itemLedger struct {
	pending []itemLedgerRow
}

/*
================
itemChangeRowsLocked

One row per item whose count differs from before to after (row counts,
domain.DiffItemRowCounts), stamped now under the door word. Both note
paths (the commit's and an own transaction's) build their rows here.
================
*/
func (s *Store) itemChangeRowsLocked(label, division string, charID int64, before, after []domain.ItemCount) []itemLedgerRow {
	door := domain.EconomyDoor(label)
	atMs := s.now().UnixMilli()
	var rows []itemLedgerRow
	for _, change := range domain.DiffItemRowCounts(before, after) {
		rows = append(rows, itemLedgerRow{
			atMs: atMs, division: division, charID: charID,
			door: door, item: change.Item, count: change.Count,
		})
	}
	return rows
}

/*
================
itemBucketKey

One character's hourly bucket of one item through one bulk door.
================
*/
type itemBucketKey struct {
	hourMs int64
	charID int64
	door   string
	item   string
}

/*
================
itemBucketFold

A bucket's signed total and change count while a batch folds.
================
*/
type itemBucketFold struct {
	total, n int64
}

/*
================
flushItemLedgerTx

Writes a commit's noted item changes inside tx: a bulk door's change
joins its hourly bucket, every other change keeps its exact row.
================
*/
func flushItemLedgerTx(tx sqlExecer, rows []itemLedgerRow) error {
	buckets := map[itemBucketKey]*itemBucketFold{}
	for _, row := range rows {
		if domain.ItemDoorBulk(row.door) {
			key := itemBucketKey{hourMs: economyHourStartMs(row.atMs), charID: row.charID, door: row.door, item: row.item}
			fold := buckets[key]
			if fold == nil {
				fold = &itemBucketFold{}
				buckets[key] = fold
			}
			fold.total += row.count
			fold.n++
			continue
		}
		if _, err := tx.Exec(`INSERT INTO item_ledger (at_ms, division, char_id, door, item, count)
VALUES (?, ?, ?, ?, ?, ?)`, row.atMs, row.division, row.charID, row.door, row.item, row.count); err != nil {
			return fmt.Errorf("item ledger: %w", err)
		}
	}
	return visitSorted(buckets, func(a, b itemBucketKey) bool {
		if a.hourMs != b.hourMs {
			return a.hourMs < b.hourMs
		}
		if a.charID != b.charID {
			return a.charID < b.charID
		}
		if a.door != b.door {
			return a.door < b.door
		}
		return a.item < b.item
	}, func(key itemBucketKey, fold *itemBucketFold) error {
		if _, err := tx.Exec(`INSERT INTO item_ledger_char_hours (hour_ms, char_id, door, item, total, n)
VALUES (?, ?, ?, ?, ?, ?)
ON CONFLICT (hour_ms, char_id, door, item) DO UPDATE SET
  total = total + excluded.total, n = n + excluded.n`,
			key.hourMs, key.charID, key.door, key.item, fold.total, fold.n); err != nil {
			return fmt.Errorf("item ledger char hours: %w", err)
		}
		return nil
	})
}

/*
================
ItemLedgerRow

One exact item change as read back: the door, the item and the signed
count.
================
*/
type ItemLedgerRow struct {
	AtMs     int64
	Division string
	Door     string
	Item     string
	Count    int64
}

/*
================
ItemLedger

The character's exact item changes since sinceMs, oldest first: which
item, how many, and through which door (#3307).
================
*/
func (s *Store) ItemLedger(charID int64, sinceMs int64) ([]ItemLedgerRow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("item ledger: no database open")
	}
	rows, err := s.db.Query(`SELECT at_ms, division, door, item, count FROM item_ledger
WHERE char_id = ? AND at_ms >= ? ORDER BY seq`, charID, sinceMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemLedgerRow
	for rows.Next() {
		var row ItemLedgerRow
		if err := rows.Scan(&row.AtMs, &row.Division, &row.Door, &row.Item, &row.Count); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

/*
================
ItemLedgerHour

One character's hourly total of one item through one bulk door.
================
*/
type ItemLedgerHour struct {
	HourMs int64
	Door   string
	Item   string
	Total  int64
	N      int64
}

/*
================
ItemLedgerHours

The character's bulk-door item buckets since sinceMs, oldest first.
================
*/
func (s *Store) ItemLedgerHours(charID int64, sinceMs int64) ([]ItemLedgerHour, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("item ledger hours: no database open")
	}
	rows, err := s.db.Query(`SELECT hour_ms, door, item, total, n FROM item_ledger_char_hours
WHERE char_id = ? AND hour_ms >= ? ORDER BY hour_ms, door, item`, charID, economyHourStartMs(sinceMs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ItemLedgerHour
	for rows.Next() {
		var hour ItemLedgerHour
		if err := rows.Scan(&hour.HourMs, &hour.Door, &hour.Item, &hour.Total, &hour.N); err != nil {
			return nil, err
		}
		out = append(out, hour)
	}
	return out, rows.Err()
}
