/*
===========================================================================

economy_ledger.go - every gold change, summed at commit (layout 10's
table, #3187's buckets)

Port-only, not native: the original keeps no economy journal. The staff
console's economy view reads the aggregates (through the GameWorld's
aggregate endpoint, never the live file).

The store attributes every change itself, at the door boundary: the ledger
remembers each live character's gold as of its last door, and a door that
leaves a different balance notes the change against the door and the signed
delta. Gameplay code needs no ledger calls, so no gold path can be missed;
a door the reason table does not know is recorded as "unattributed" with
its label. Every noted change joins the transaction that writes the
character, so a change and its record are durable together or not at all;
a failed commit keeps them pending, as it keeps the dirty records, and
the next commit writes both. The few doors with their own transaction
(fortress tax, warehouse, guild war stakes, fortress staff and production)
write their record in that transaction.

At flush time a change lands in the hourly buckets
(economy_ledger_hours: hour, division, reason, direction - the sums the
console reads), and the two bulk reasons (monster_gold, npc_sale, 99.5%
of production traffic) land in per-character hourly buckets instead of
rows (economy_ledger_char_hours). Every other reason also keeps its
exact row in economy_ledger as evidence. The bucket tables are purely
additive: no older binary reads them, and one ignores them on the same
file and writes rows again, so they are created at open with no schema
stamp (#3187). The rows history already holds are folded into the
buckets and deleted by economy_fold.go.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
	"sort"
	"sync"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/domain"
)

// economyLedgerSchema is layout 10's addition (authority_upgrade.go adds it
// to a layout 5..9 authority).
const economyLedgerSchema = `
CREATE TABLE IF NOT EXISTS economy_ledger (
  seq      INTEGER PRIMARY KEY,
  at_ms    INTEGER NOT NULL,
  division TEXT    NOT NULL,
  char_id  INTEGER NOT NULL,
  reason   TEXT    NOT NULL,
  door     TEXT    NOT NULL,
  gold     INTEGER NOT NULL,
  balance  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS economy_ledger_at ON economy_ledger (at_ms);
`

// economyBucketSchema is #3187's addition: the hourly buckets a gold
// change lands in at commit, beside the exact rows. A table an older
// binary ignores needs no schema stamp: the store creates them at open
// (ensureEconomyBuckets), a fresh authority gets them from dbSchema,
// and a revert is the same file under the old binary, which writes
// rows again.
const economyBucketSchema = `
CREATE TABLE IF NOT EXISTS economy_ledger_hours (
  hour_ms  INTEGER NOT NULL,
  division TEXT    NOT NULL,
  reason   TEXT    NOT NULL,
  gain     INTEGER NOT NULL,
  total    INTEGER NOT NULL,
  low      INTEGER NOT NULL,
  high     INTEGER NOT NULL,
  n        INTEGER NOT NULL,
  PRIMARY KEY (hour_ms, division, reason, gain)
);
CREATE TABLE IF NOT EXISTS economy_ledger_char_hours (
  hour_ms INTEGER NOT NULL,
  char_id INTEGER NOT NULL,
  reason  TEXT    NOT NULL,
  total   INTEGER NOT NULL,
  n       INTEGER NOT NULL,
  PRIMARY KEY (hour_ms, char_id, reason)
);
`

// economyHourMs is the bucket width: one hour in milliseconds.
const economyHourMs = 3_600_000

// ledgerBulkReasons are the two reasons that keep per-character hourly
// buckets instead of exact rows: on production they are 99.5% of the
// ledger's rows (monster_gold 94.3%, npc_sale 5.2% of the last 2M,
// #3187). Every other reason keeps its exact row as evidence.
var ledgerBulkReasons = map[string]bool{
	"monster_gold": true,
	"npc_sale":     true,
}

/*
================
ensureEconomyBuckets

Creates the bucket tables when the store opens (#3187): additive, so
no stamp moves and an older binary on the same file never notices.
Also adds the ledger's bucketed marker column when it is absent: a
row the bucketing commit path wrote carries bucketed = 1, so the fold
(economy_fold.go) folds every unmarked row - history's, and an older
binary's from a rollback window - into the buckets exactly once. The
older binary's inserts name their columns, so the marker simply
stays NULL on its rows.
================
*/
func ensureEconomyBuckets(db *sql.DB) error {
	if _, err := db.Exec(economyBucketSchema); err != nil {
		return fmt.Errorf("economy buckets: %w", err)
	}
	columns, err := db.Query("PRAGMA table_info(economy_ledger)")
	if err != nil {
		return err
	}
	defer columns.Close()
	marked := false
	for columns.Next() {
		var cid int
		var name, kind string
		var notNull, pk int
		var fallback any
		if err := columns.Scan(&cid, &name, &kind, &notNull, &fallback, &pk); err != nil {
			return err
		}
		if name == "bucketed" {
			marked = true
		}
	}
	if err := columns.Err(); err != nil {
		return err
	}
	if !marked {
		if _, err := db.Exec("ALTER TABLE economy_ledger ADD COLUMN bucketed INTEGER"); err != nil {
			return fmt.Errorf("economy ledger bucketed marker: %w", err)
		}
	}
	return nil
}

/*
================
ledgerRow

One gold change: gold is signed, balance is the character's gold after it.
================
*/
type ledgerRow struct {
	atMs     int64
	division string
	charID   int64
	reason   string
	door     string
	gold     int64
	balance  int64
}

/*
================
economyLedger

seen is each live character's gold as of its last door. pending holds the
rows of doors whose commit has not succeeded yet. totals are the committed
gold per reason and direction, for the metrics endpoint.
================
*/
type economyLedger struct {
	seen    map[*domain.Character]int64
	pending []ledgerRow
	// warned names the unattributed doors already logged, once each.
	warned map[string]bool
	mu     sync.Mutex
	totals map[string]uint64
}

/*
================
ledgerGold
================
*/
func ledgerGold(c *domain.Character) int64 {
	if c == nil || c.Gold == nil {
		return 0
	}
	return *c.Gold
}

/*
================
seedLedgerLocked

A character the store starts to own: its balance is where its rows begin.
================
*/
func (s *Store) seedLedgerLocked(c *domain.Character) {
	s.seedLedgerAtLocked(c, ledgerGold(c))
}

/*
================
seedLedgerAtLocked

Starts c's rows at balance: its loaded gold, or nothing for a character
whose creation mints its starting gold.
================
*/
func (s *Store) seedLedgerAtLocked(c *domain.Character, balance int64) {
	if s.ledger.seen == nil {
		s.ledger.seen = map[*domain.Character]int64{}
	}
	s.ledger.seen[c] = balance
}

/*
================
forgetLedgerLocked
================
*/
func (s *Store) forgetLedgerLocked(c *domain.Character) {
	delete(s.ledger.seen, c)
}

/*
================
noteGoldLocked

A door finished: every dirty character whose gold moved since its last
door gets a row naming this door. A character noted by an earlier door of
the same commit has not moved again, so nothing counts twice.
================
*/
func (s *Store) noteGoldLocked(label string) {
	if s.changes.all {
		for division, records := range s.characters {
			for _, c := range records {
				s.noteCharacterGoldLocked(label, division, c)
			}
		}
		return
	}
	for c := range s.changes.characters {
		if division, known := s.charDivision[c]; known {
			s.noteCharacterGoldLocked(label, division, c)
		}
	}
}

/*
================
noteDoorGoldLocked

A deferred door: only the characters it declared can have moved, so the
tick batch never rescans its whole dirty set per door. A door that
escalated to the whole world notes everyone.
================
*/
func (s *Store) noteDoorGoldLocked(label string, cs []*domain.Character) {
	if s.changes.all {
		s.noteGoldLocked(label)
		return
	}
	for _, c := range cs {
		if division, known := s.charDivision[c]; c != nil && known {
			s.noteCharacterGoldLocked(label, division, c)
		}
	}
}

/*
================
noteCharacterGoldLocked
================
*/
func (s *Store) noteCharacterGoldLocked(label, division string, c *domain.Character) {
	if row, moved := s.goldRowLocked(label, division, c, ledgerGold(c)); moved {
		s.ledger.pending = append(s.ledger.pending, row)
		s.ledger.seen[c] = row.balance
	}
}

/*
================
goldRowLocked

The row of c's gold moving to balance through the door, if it moved. A
character the ledger never saw (a wiring bug) is seeded, not guessed at.
================
*/
func (s *Store) goldRowLocked(label, division string, c *domain.Character, balance int64) (ledgerRow, bool) {
	before, known := s.ledger.seen[c]
	if !known {
		s.seedLedgerLocked(c)
		return ledgerRow{}, false
	}
	if balance == before {
		return ledgerRow{}, false
	}
	door := domain.EconomyDoor(label)
	reason := domain.EconomyRowReason(label, c.ID, balance > before)
	if reason == domain.EconomyReasonUnattributed && !s.ledger.warned[door] {
		// A door the reason table does not name moved gold: the row keeps
		// the door, and the log names it once so the table can learn it.
		if s.ledger.warned == nil {
			s.ledger.warned = map[string]bool{}
		}
		s.ledger.warned[door] = true
		log.WithFields(log.Fields{"door": door, "gain": balance > before}).Warn("economy ledger: a door with no reason moved gold")
	}
	return ledgerRow{
		atMs:     s.now().UnixMilli(),
		division: division,
		charID:   c.ID,
		reason:   reason,
		door:     door,
		gold:     balance - before,
		balance:  balance,
	}, true
}

/*
================
insertLedgerRowsTx
================
*/
func insertLedgerRowsTx(tx sqlExecer, rows []ledgerRow) error {
	for _, row := range rows {
		if _, err := tx.Exec(`INSERT INTO economy_ledger (at_ms, division, char_id, reason, door, gold, balance, bucketed)
VALUES (?, ?, ?, ?, ?, ?, ?, 1)`, row.atMs, row.division, row.charID, row.reason, row.door, row.gold, row.balance); err != nil {
			return fmt.Errorf("economy ledger: %w", err)
		}
	}
	return nil
}

/*
================
ledgerBucketKey

One hourly bucket of the console's aggregates: the hour a change
landed in, its division, its reason and its direction.
================
*/
type ledgerBucketKey struct {
	hourMs   int64
	division string
	reason   string
	gain     bool
}

/*
================
ledgerBucketFold

A bucket's accumulated sums while a batch folds: the signed total,
the smallest and largest single change, and how many changes.
================
*/
type ledgerBucketFold struct {
	total, low, high, n int64
}

/*
================
add

Folds one signed change into the bucket.
================
*/
func (f *ledgerBucketFold) add(gold int64) {
	if f.n == 0 || gold < f.low {
		f.low = gold
	}
	if f.n == 0 || gold > f.high {
		f.high = gold
	}
	f.total += gold
	f.n++
}

/*
================
ledgerCharBucketKey

One character's hourly bucket of a bulk reason.
================
*/
type ledgerCharBucketKey struct {
	hourMs int64
	charID int64
	reason string
}

/*
================
foldLedgerRows

The buckets a batch of changes folds into: every change joins its
hourly bucket; a bulk reason's changes also join the character's own
hourly bucket, and its caller keeps no row for them.
================
*/
func foldLedgerRows(rows []ledgerRow) (map[ledgerBucketKey]*ledgerBucketFold, map[ledgerCharBucketKey]*ledgerBucketFold) {
	global := map[ledgerBucketKey]*ledgerBucketFold{}
	chars := map[ledgerCharBucketKey]*ledgerBucketFold{}
	for _, row := range rows {
		hourMs := row.atMs / economyHourMs * economyHourMs
		key := ledgerBucketKey{hourMs: hourMs, division: row.division, reason: row.reason, gain: row.gold > 0}
		fold := global[key]
		if fold == nil {
			fold = &ledgerBucketFold{}
			global[key] = fold
		}
		fold.add(row.gold)
		if ledgerBulkReasons[row.reason] {
			charKey := ledgerCharBucketKey{hourMs: hourMs, charID: row.charID, reason: row.reason}
			charFold := chars[charKey]
			if charFold == nil {
				charFold = &ledgerBucketFold{}
				chars[charKey] = charFold
			}
			charFold.add(row.gold)
		}
	}
	return global, chars
}

/*
================
upsertLedgerBucketsTx

Applies folded buckets inside tx: an existing bucket grows by the
fold, a new one starts from it. Keys are visited in sorted order so
the statement sequence is deterministic.
================
*/
func upsertLedgerBucketsTx(tx sqlExecer, global map[ledgerBucketKey]*ledgerBucketFold, chars map[ledgerCharBucketKey]*ledgerBucketFold) error {
	keys := make([]ledgerBucketKey, 0, len(global))
	for key := range global {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		if a.hourMs != b.hourMs {
			return a.hourMs < b.hourMs
		}
		if a.division != b.division {
			return a.division < b.division
		}
		if a.reason != b.reason {
			return a.reason < b.reason
		}
		return !a.gain && b.gain
	})
	for _, key := range keys {
		fold := global[key]
		gain := 0
		if key.gain {
			gain = 1
		}
		if _, err := tx.Exec(`INSERT INTO economy_ledger_hours (hour_ms, division, reason, gain, total, low, high, n)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT (hour_ms, division, reason, gain) DO UPDATE SET
  total = total + excluded.total, low = min(low, excluded.low), high = max(high, excluded.high), n = n + excluded.n`,
			key.hourMs, key.division, key.reason, gain, fold.total, fold.low, fold.high, fold.n); err != nil {
			return fmt.Errorf("economy ledger hours: %w", err)
		}
	}
	charKeys := make([]ledgerCharBucketKey, 0, len(chars))
	for key := range chars {
		charKeys = append(charKeys, key)
	}
	sort.Slice(charKeys, func(i, j int) bool {
		a, b := charKeys[i], charKeys[j]
		if a.hourMs != b.hourMs {
			return a.hourMs < b.hourMs
		}
		if a.charID != b.charID {
			return a.charID < b.charID
		}
		return a.reason < b.reason
	})
	for _, key := range charKeys {
		fold := chars[key]
		if _, err := tx.Exec(`INSERT INTO economy_ledger_char_hours (hour_ms, char_id, reason, total, n)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT (hour_ms, char_id, reason) DO UPDATE SET
  total = total + excluded.total, n = n + excluded.n`,
			key.hourMs, key.charID, key.reason, fold.total, fold.n); err != nil {
			return fmt.Errorf("economy ledger char hours: %w", err)
		}
	}
	return nil
}

/*
================
flushLedgerTx

Writes a commit's noted changes (#3187): every change updates its
hourly bucket; a bulk reason's changes also update the character's
hourly bucket and keep no row, and every other change keeps its exact
row as evidence. All of it rides the caller's transaction, so a change
and its record are durable together or not at all.
================
*/
func flushLedgerTx(tx sqlExecer, rows []ledgerRow) error {
	var exact []ledgerRow
	for _, row := range rows {
		if !ledgerBulkReasons[row.reason] {
			exact = append(exact, row)
		}
	}
	if err := insertLedgerRowsTx(tx, exact); err != nil {
		return err
	}
	global, chars := foldLedgerRows(rows)
	return upsertLedgerBucketsTx(tx, global, chars)
}

/*
================
ledgerOwnTxLocked

A door with its own transaction: writes the row of live's gold moving to
balance inside tx, and returns what to run once tx commits (the ledger
then advances, as the door then assigns the balance).
================
*/
func (s *Store) ledgerOwnTxLocked(tx *sql.Tx, label, division string, live *domain.Character, balance int64) (func(), error) {
	row, moved := s.goldRowLocked(label, division, live, balance)
	if !moved {
		return func() {}, nil
	}
	if err := flushLedgerTx(tx, []ledgerRow{row}); err != nil {
		return nil, err
	}
	return func() {
		s.ledger.seen[live] = balance
		s.countLedgerRows([]ledgerRow{row})
	}, nil
}

/*
================
countLedgerRows

Committed rows join the per-reason totals.
================
*/
func (s *Store) countLedgerRows(rows []ledgerRow) {
	if len(rows) == 0 {
		return
	}
	s.ledger.mu.Lock()
	defer s.ledger.mu.Unlock()
	if s.ledger.totals == nil {
		s.ledger.totals = map[string]uint64{}
	}
	for _, row := range rows {
		if row.gold > 0 {
			s.ledger.totals["gold."+row.reason+".gain"] += uint64(row.gold)
		} else {
			s.ledger.totals["gold."+row.reason+".loss"] += uint64(-row.gold)
		}
	}
}

/*
================
EconomyCounters

The gold committed since the process started, per reason and direction
("gold.<reason>.gain" / ".loss"), for /transport/metrics. Never takes the
store lock: a metrics read never waits for a commit.
================
*/
func (s *Store) EconomyCounters() map[string]uint64 {
	s.ledger.mu.Lock()
	defer s.ledger.mu.Unlock()
	out := make(map[string]uint64, len(s.ledger.totals))
	for key, value := range s.ledger.totals {
		out[key] = value
	}
	return out
}

/*
================
validateEconomyLedger

Startup reads the tables' shapes without scanning them: the ledger
grows without bound and its rows are never decoded into the authority
graph. The bucket tables (#3187) are shape-checked when they exist: a
first boot of the new binary over an older file has none yet, and the
open creates them right after this check.
================
*/
func validateEconomyLedger(db *sql.DB) error {
	rows, err := db.Query("SELECT seq, at_ms, division, char_id, reason, door, gold, balance FROM economy_ledger LIMIT 0")
	if err != nil {
		return err
	}
	// The pool is one connection: the shape rows must let go of it
	// before the bucket checks below ask for it again, or open hangs.
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var buckets int
	if err := db.QueryRow(`SELECT count(*) FROM sqlite_schema
WHERE type = 'table' AND name IN ('economy_ledger_hours', 'economy_ledger_char_hours')`).Scan(&buckets); err != nil {
		return err
	}
	if buckets == 0 {
		return nil
	}
	if buckets != 2 {
		return fmt.Errorf("economy buckets: one of the two bucket tables is missing")
	}
	for _, shape := range []string{
		"SELECT hour_ms, division, reason, gain, total, low, high, n FROM economy_ledger_hours LIMIT 0",
		"SELECT hour_ms, char_id, reason, total, n FROM economy_ledger_char_hours LIMIT 0",
	} {
		shapeRows, err := db.Query(shape)
		if err != nil {
			return err
		}
		if err := shapeRows.Err(); err != nil {
			shapeRows.Close()
			return err
		}
		if err := shapeRows.Close(); err != nil {
			return err
		}
	}
	return nil
}

/*
================
LedgerCharHour

One character's hourly total of one bulk reason (#3187): who gained
how much of it, and when.
================
*/
type LedgerCharHour struct {
	HourMs int64
	Reason string
	Total  int64
	N      int64
}

/*
================
EconomyCharacterHours

The character's per-hour bulk-reason totals since sinceMs, oldest
first: the answerable store behind "who gained how much monster gold,
and when" (#3187). The console reads aggregates; this is the drill-in
the buckets keep possible.
================
*/
func (s *Store) EconomyCharacterHours(charID int64, sinceMs int64) ([]LedgerCharHour, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("economy char hours: no database open")
	}
	rows, err := s.db.Query(`SELECT hour_ms, reason, total, n FROM economy_ledger_char_hours
WHERE char_id = ? AND hour_ms >= ? ORDER BY hour_ms, reason`, charID, sinceMs/economyHourMs*economyHourMs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LedgerCharHour
	for rows.Next() {
		var hour LedgerCharHour
		if err := rows.Scan(&hour.HourMs, &hour.Reason, &hour.Total, &hour.N); err != nil {
			return nil, err
		}
		out = append(out, hour)
	}
	return out, rows.Err()
}
