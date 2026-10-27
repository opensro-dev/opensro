/*
===========================================================================

name_reservation.go - the names a rename leaves behind (layout 11)

Port-only, not native (#1689, SRO_NAME_CHANGE_SCROLL). When a queued
rename applies, the character's old name enters this table for 90 days:
character creation and rename targets refuse a reserved name, so a freed
name cannot be re-registered at once and traded on (bookmarks, share
cards and player trust all run on the visible string). The row is
written in the rename transaction itself, so no window exists where
the old name is free but unrecorded. Expiry is lazy: the two readers
compare expires_ms against now, and no sweeper runs. A staff release
is one row delete through the operator door.

===========================================================================
*/
package store

import (
	"database/sql"
	"fmt"
	"strings"
)

// nameReservationsSchema is layout 11's addition (authority_upgrade.go
// adds it to a layout 5..10 authority).
const nameReservationsSchema = `
CREATE TABLE IF NOT EXISTS name_reservations (
  division       TEXT    NOT NULL,
  name_lower     TEXT    NOT NULL,
  expires_ms     INTEGER NOT NULL,
  former_char_id INTEGER NOT NULL,
  PRIMARY KEY (division, name_lower)
);
`

// NameReservation is one held name: the lower-cased name, the instant
// the hold lapses and the character that gave the name up.
/*
================
NameReservation
================
*/
type NameReservation struct {
	Division     string
	NameLower    string
	ExpiresMs    int64
	FormerCharID int64
}

/*
================
validateNameReservations

Boot validation for the reservation plane: every row is well-formed.
A reservation is never required to name a live character - deleting
the former character does not free the name early.
================
*/
func validateNameReservations(db *sql.DB) error {
	rows, err := db.Query("SELECT division, name_lower, expires_ms, former_char_id FROM name_reservations")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var row NameReservation
		if err := rows.Scan(&row.Division, &row.NameLower, &row.ExpiresMs, &row.FormerCharID); err != nil {
			return err
		}
		if row.Division == "" || row.NameLower == "" || strings.ToLower(row.NameLower) != row.NameLower ||
			!persistedCharacterNameValid(row.NameLower) {
			return fmt.Errorf("name reservation %q/%q is malformed", row.Division, row.NameLower)
		}
		if row.ExpiresMs <= 0 || row.FormerCharID < 1 {
			return fmt.Errorf("name reservation %q/%q has expiry %d, former character %d",
				row.Division, row.NameLower, row.ExpiresMs, row.FormerCharID)
		}
	}
	return rows.Err()
}

/*
================
loadNameReservations

Reads the whole plane into its in-memory shape (division -> name ->
row). Called by loadDB at layout 11.
================
*/
func loadNameReservations(db *sql.DB) (map[string]map[string]NameReservation, error) {
	out := map[string]map[string]NameReservation{}
	rows, err := db.Query("SELECT division, name_lower, expires_ms, former_char_id FROM name_reservations ORDER BY division, name_lower")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var row NameReservation
		if err := rows.Scan(&row.Division, &row.NameLower, &row.ExpiresMs, &row.FormerCharID); err != nil {
			return nil, err
		}
		if out[row.Division] == nil {
			out[row.Division] = map[string]NameReservation{}
		}
		out[row.Division][row.NameLower] = row
	}
	return out, rows.Err()
}

/*
================
nameReservedLocked

Whether name is held in division at nowMs for anyone but exceptCharID
(0 accepts no exception: character creation). An expired row holds
nothing. The caller holds the store lock.
================
*/
func (s *Store) nameReservedLocked(division, name string, nowMs, exceptCharID int64) bool {
	row, ok := s.nameReservations[division][strings.ToLower(name)]
	return ok && row.ExpiresMs > nowMs && row.FormerCharID != exceptCharID
}

/*
================
insertNameReservationTx

Writes (or refreshes) one reservation row inside the caller's
transaction - the rename commit, so the hold is durable with the
rename or not at all.
================
*/
func insertNameReservationTx(tx *sql.Tx, row NameReservation) error {
	_, err := tx.Exec(
		"INSERT INTO name_reservations (division, name_lower, expires_ms, former_char_id) VALUES (?, ?, ?, ?) "+
			"ON CONFLICT(division, name_lower) DO UPDATE SET expires_ms = excluded.expires_ms, former_char_id = excluded.former_char_id",
		row.Division, row.NameLower, row.ExpiresMs, row.FormerCharID)
	return err
}
