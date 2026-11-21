/*
===========================================================================

item_ledger.go - every item entering or leaving a character, at commit
(#3307)

Port-only, not native: the original keeps no item journal. The gold
ledger (economy_ledger.go) proved the design this file repeats for items:
the store attributes every change itself, at the door boundary, so no
gameplay path needs a ledger call and none can be missed. The ledger
remembers what each stored character held as of its last door (its row
counts, domain.AppendItemRowCounts); a door that leaves different
holdings (domain.FoldItemCounts, a count per item codename) notes each
item's signed change against the door. A door that moved no item leaves
the row counts equal and costs one linear comparison.

Moves inside one character (bag slots, a pet's bag, the body) leave the
holdings unchanged and write nothing. Every noted change joins the
transaction that writes the character, so a change and its record are
durable together or not at all; a failed commit keeps them pending, and
the next commit writes them once. A door with its own transaction
(characterRowTx) notes the characters it writes inside that transaction
and advances the baseline only when it commits.

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

seen is each stored character's row counts as of its last door. pending
holds the rows of doors whose commit has not succeeded yet. scratch is
the holdings buffer a door's read reuses, so a door that moved no item
allocates nothing.
================
*/
type itemLedger struct {
	seen    map[*domain.Character][]domain.ItemCount
	pending []itemLedgerRow
	scratch []domain.ItemCount
}

/*
================
seedItemsLocked

A character the store starts to own: what it holds is where its rows
begin.
================
*/
func (s *Store) seedItemsLocked(c *domain.Character) {
	if s.items.seen == nil {
		s.items.seen = map[*domain.Character][]domain.ItemCount{}
	}
	s.items.seen[c] = domain.AppendItemRowCounts(nil, c)
}

/*
================
seedItemsEmptyLocked

A new character: its creation grants what it starts with, so its rows
begin at nothing and the creation commit records the starting items.
================
*/
func (s *Store) seedItemsEmptyLocked(c *domain.Character) {
	if s.items.seen == nil {
		s.items.seen = map[*domain.Character][]domain.ItemCount{}
	}
	s.items.seen[c] = nil
}

/*
================
forgetItemsLocked
================
*/
func (s *Store) forgetItemsLocked(c *domain.Character) {
	delete(s.items.seen, c)
}

/*
================
noteLedgersLocked

A door finished: both ledgers read what its characters' gold and items
did. The commit path calls this one note, so a third ledger joins here.
================
*/
func (s *Store) noteLedgersLocked(label string) {
	s.noteGoldLocked(label)
	s.noteItemsLocked(label)
}

/*
================
noteDoorLedgersLocked

noteLedgersLocked for a deferred door: only its declared characters.
================
*/
func (s *Store) noteDoorLedgersLocked(label string, cs []*domain.Character) {
	s.noteDoorGoldLocked(label, cs)
	s.noteDoorItemsLocked(label, cs)
}

/*
================
noteItemsLocked

Every dirty character whose holdings changed since its last door gets
its rows under this door; one noted by an earlier door of the same
commit has not changed again, so nothing counts twice.
================
*/
func (s *Store) noteItemsLocked(label string) {
	if s.changes.all {
		for division, records := range s.characters {
			for _, c := range records {
				s.noteCharacterItemsLocked(label, division, c)
			}
		}
		return
	}
	for c := range s.changes.characters {
		if division, known := s.charDivision[c]; known {
			s.noteCharacterItemsLocked(label, division, c)
		}
	}
}

/*
================
noteDoorItemsLocked

A deferred door: only the characters it declared can have changed. A
door that escalated to the whole world notes everyone.
================
*/
func (s *Store) noteDoorItemsLocked(label string, cs []*domain.Character) {
	if s.changes.all {
		s.noteItemsLocked(label)
		return
	}
	for _, c := range cs {
		if division, known := s.charDivision[c]; c != nil && known {
			s.noteCharacterItemsLocked(label, division, c)
		}
	}
}

/*
================
noteCharacterItemsLocked

The door's rows for c, if what it holds changed. Equal row counts are
the common case (a door that moved no item) and stop at the comparison;
only a changed sequence is folded and diffed, and a move that changed it
writes nothing. A character the ledger never saw (a wiring bug) is
seeded, not guessed at.
================
*/
func (s *Store) noteCharacterItemsLocked(label, division string, c *domain.Character) {
	s.items.scratch = domain.AppendItemRowCounts(s.items.scratch, c)
	before, known := s.items.seen[c]
	if known && domain.SameItemHoldings(before, s.items.scratch) {
		return
	}
	if known {
		s.items.pending = append(s.items.pending, s.itemChangeRowsLocked(label, division, c.ID, before, s.items.scratch)...)
	}
	if s.items.seen == nil {
		s.items.seen = map[*domain.Character][]domain.ItemCount{}
	}
	// The baseline is this character's own buffer: reused in place.
	s.items.seen[c] = append(before[:0], s.items.scratch...)
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
ownTxItemWrite

One character an own-transaction door writes: its live record and the
row counts the written copy carries, which become its baseline once the
transaction commits.
================
*/
type ownTxItemWrite struct {
	live     *domain.Character
	holdings []domain.ItemCount
}

/*
================
characterRowTx::noteItems

The door wrote copy for division: the rows of the live record's holdings
becoming the copy's join this transaction (Commit writes them), and the
copy's holdings become the baseline once it commits. A second write of
the same character in one transaction diffs against the first, so a
change is never counted twice; a rolled-back door leaves no row and no
moved baseline.
================
*/
func (w *characterRowTx) noteItems(division string, copy *domain.Character) {
	s := w.store
	live := s.characterByIDLocked(division, copy.ID)
	if live == nil {
		// A row the roster does not hold (a fixture writing a detached
		// record): nothing to attribute it to.
		return
	}
	after := domain.AppendItemRowCounts(nil, copy)
	var prior *ownTxItemWrite
	for _, write := range w.itemWrites {
		if write.live == live {
			prior = write
		}
	}
	before, known := s.items.seen[live]
	if prior != nil {
		before, known = prior.holdings, true
	}
	if known && !domain.SameItemHoldings(before, after) {
		w.itemRows = append(w.itemRows, s.itemChangeRowsLocked(w.label, division, live.ID, before, after)...)
	}
	if prior != nil {
		prior.holdings = after
		return
	}
	w.itemWrites = append(w.itemWrites, &ownTxItemWrite{live: live, holdings: after})
}

/*
================
characterRowTx::settleItems

The transaction committed: each written character's baseline is what
its last written copy held.
================
*/
func (w *characterRowTx) settleItems() {
	s := w.store
	if s.items.seen == nil {
		s.items.seen = map[*domain.Character][]domain.ItemCount{}
	}
	for _, write := range w.itemWrites {
		s.items.seen[write.live] = write.holdings
	}
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
