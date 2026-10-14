/*
===========================================================================

economy_ledger.go - one row per gold change, in the commit that makes it
(layout 10)

Port-only, not native: the original keeps no economy journal. The staff
console's economy view reads this append-only table (through the GameWorld's
aggregate endpoint, never the live file).

The store attributes every change itself, at the door boundary: the ledger
remembers each live character's gold as of its last door, and a door that
leaves a different balance appends a row naming the door and the signed
change. Gameplay code needs no ledger calls, so no gold path can be missed;
a door the reason table does not know is recorded as "unattributed" with
its label. The rows join the transaction that writes the character, so a
change and its row are durable together or not at all; a failed commit
keeps them pending, as it keeps the dirty records, and the next commit
writes both. The few doors with their own transaction (fortress tax,
warehouse, guild war stakes, fortress staff and production) write their
row in that transaction.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
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
func insertLedgerRowsTx(tx *sql.Tx, rows []ledgerRow) error {
	for _, row := range rows {
		if _, err := tx.Exec(`INSERT INTO economy_ledger (at_ms, division, char_id, reason, door, gold, balance)
VALUES (?, ?, ?, ?, ?, ?, ?)`, row.atMs, row.division, row.charID, row.reason, row.door, row.gold, row.balance); err != nil {
			return fmt.Errorf("economy ledger: %w", err)
		}
	}
	return nil
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
	if err := insertLedgerRowsTx(tx, []ledgerRow{row}); err != nil {
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

Startup reads the table's shape without scanning it: the ledger grows
without bound and its rows are never decoded into the authority graph.
================
*/
func validateEconomyLedger(db *sql.DB) error {
	rows, err := db.Query("SELECT seq, at_ms, division, char_id, reason, door, gold, balance FROM economy_ledger LIMIT 0")
	if err != nil {
		return err
	}
	defer rows.Close()
	return rows.Err()
}
