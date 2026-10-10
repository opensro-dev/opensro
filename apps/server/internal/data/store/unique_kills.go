/*
===========================================================================

unique_kills.go - the record of every unique monster kill (layout 9)

Port-only, not native: the original announces a unique's killer (0x300C
kind 6) but keeps no history. The community site's unique tracker, kill
feed, leaderboard and world-firsts read this append-only table through the
public read API. A row names the killer by character id as well as by
name, so the read side can apply the character's current privacy choice to
every past kill.

===========================================================================
*/
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	"opensro.online/server/internal/domain"
)

// uniqueKillsSchema is layout 9's addition (authority_upgrade.go adds it to
// a layout 5..8 authority).
const uniqueKillsSchema = `
CREATE TABLE IF NOT EXISTS unique_kills (
  division TEXT    NOT NULL,
  seq      INTEGER NOT NULL,
  record   TEXT    NOT NULL,
  PRIMARY KEY (division, seq)
);
`

// UniqueKillPage is the most kills one UniqueKillsAfter read returns; a
// reader catching up pages until a read returns fewer.
const UniqueKillPage = 10000

/*
================
decodeUniqueKill

A row's JSON must carry its own sequence, a unique and a time.
================
*/
func decodeUniqueKill(seq int64, raw string) (domain.UniqueKill, error) {
	var kill domain.UniqueKill
	if err := decodeJSONStrict([]byte(raw), &kill); err != nil {
		return kill, fmt.Errorf("unique kill %d: %w", seq, err)
	}
	if kill.Seq != seq || kill.RefObjID == 0 || kill.AtMs <= 0 {
		return kill, fmt.Errorf("unique kill %d: inconsistent record", seq)
	}
	return kill, nil
}

/*
================
validateUniqueKills

Startup validates without creating the table or repairing a row.
================
*/
func validateUniqueKills(db *sql.DB) error {
	rows, err := db.Query("SELECT seq, record FROM unique_kills")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var raw string
		if err := rows.Scan(&seq, &raw); err != nil {
			return err
		}
		if _, err := decodeUniqueKill(seq, raw); err != nil {
			return err
		}
	}
	return rows.Err()
}

/*
================
RecordUniqueKill

Appends one kill with the next sequence of its division and returns it
numbered. The kill commits on its own: it never waits for, or joins, a
character commit.
================
*/
func (s *Store) RecordUniqueKill(division string, kill domain.UniqueKill) (domain.UniqueKill, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return kill, fmt.Errorf("unique kill: store unavailable")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return kill, err
	}
	defer func() { _ = tx.Rollback() }()
	var last sql.NullInt64
	if err := tx.QueryRow("SELECT MAX(seq) FROM unique_kills WHERE division = ?", division).Scan(&last); err != nil {
		return kill, err
	}
	kill.Seq = last.Int64 + 1
	raw, err := json.Marshal(kill)
	if err != nil {
		return kill, err
	}
	if _, err := decodeUniqueKill(kill.Seq, string(raw)); err != nil {
		return kill, err
	}
	if _, err := tx.Exec("INSERT INTO unique_kills (division, seq, record) VALUES (?, ?, ?)", division, kill.Seq, string(raw)); err != nil {
		return kill, err
	}
	return kill, tx.Commit()
}

/*
================
UniqueKillsAfter

The division's kills with a sequence above afterSeq, oldest first, at most
UniqueKillPage of them. Readers keep the last sequence they saw and ask
only for what is new, so all-time answers never lose their oldest rows and
no read grows with the table. Takes the read lock: a reader never holds
off a game commit.
================
*/
func (s *Store) UniqueKillsAfter(division string, afterSeq int64) ([]domain.UniqueKill, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil, fmt.Errorf("unique kill: store unavailable")
	}
	rows, err := s.db.Query(`SELECT seq, record FROM unique_kills
WHERE division = ? AND seq > ? ORDER BY seq LIMIT ?`, division, afterSeq, UniqueKillPage)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.UniqueKill
	for rows.Next() {
		var seq int64
		var raw string
		if err := rows.Scan(&seq, &raw); err != nil {
			return nil, err
		}
		kill, err := decodeUniqueKill(seq, raw)
		if err != nil {
			return nil, err
		}
		out = append(out, kill)
	}
	return out, rows.Err()
}
