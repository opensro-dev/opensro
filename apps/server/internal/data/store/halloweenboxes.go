/*
===========================================================================

halloweenboxes.go - the per-account Halloween daily box count

Port-only, not native (#2547; SRO_HALLOWEEN_EVENT). The daily cap counts
per account, so the count lives here, in the metadata plane, keyed by
account id: every character of an account mints against the one record.
A settlement mutates the record inside its door and the commit stages
it by digest, exactly like a character record; the store owns the
pointers. Older authorities have no halloweenBoxes row: at first load
the plane is seeded from the characters' legacy per-character counts
(schema 34), so the day's progress survives the move.

===========================================================================
*/
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

const metaKeyHalloweenBoxes = "halloweenBoxes"

/*
================
Store.HalloweenAccount

The account's live box record, created empty on first touch. The
pointer is the store's record: read or mutate it only inside a write
door (a kill settlement), never across one. An empty account id is a
fixture character with no account and shares the empty key's record,
so it stays capped like any other.
================
*/
func (s *Store) HalloweenAccount(accountID string) *domain.HalloweenBoxAccount {
	// Every touch marks the plane for the next commit: the record's
	// fields mutate through the returned pointer where the commit
	// cannot see them, so the touch is its only signal (#3164). A
	// read-only touch costs one marshal the digest then skips.
	s.changes.halloweenBoxes = true
	if s.meta.HalloweenBoxes == nil {
		s.meta.HalloweenBoxes = map[string]*domain.HalloweenBoxAccount{}
	}
	record := s.meta.HalloweenBoxes[accountID]
	if record == nil {
		record = &domain.HalloweenBoxAccount{}
		s.meta.HalloweenBoxes[accountID] = record
	}
	return record
}

/*
================
loadHalloweenBoxes

The second result reports whether the row exists at all: an authority
that predates the account count has none, and its plane is seeded from
the legacy character counts instead (seedHalloweenBoxes). A negative
count is corruption, not a reason to silently discard a day's progress.
An account's unknown top-level fields are returned for the next write
(json.go).
================
*/
func loadHalloweenBoxes(db *sql.DB) (map[string]*domain.HalloweenBoxAccount, map[string]unknownFields, bool, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKeyHalloweenBoxes).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]*domain.HalloweenBoxAccount{}, nil, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}
	accounts := map[string]*domain.HalloweenBoxAccount{}
	unknown, err := decodePlaneFields([]byte(raw), &accounts)
	if err != nil {
		return nil, nil, false, fmt.Errorf("halloween box accounts: %w", err)
	}
	for accountID, record := range accounts {
		if strings.TrimSpace(accountID) != accountID || record == nil || record.Count < 0 || record.GourdCount < 0 {
			return nil, nil, false, fmt.Errorf("invalid halloween box account %q", accountID)
		}
	}
	return accounts, unknown, true, nil
}

/*
================
seedHalloweenBoxes

The first-load derivation from the legacy per-character counts: per
account, the latest day any character counted on, and that day's boxes
summed over the account's characters. Characters without an account id
or a count seed nothing.
================
*/
func seedHalloweenBoxes(characters map[string][]*domain.Character) map[string]*domain.HalloweenBoxAccount {
	accounts := map[string]*domain.HalloweenBoxAccount{}
	for _, division := range characters {
		for _, c := range division {
			if c == nil || c.AccountID == "" || c.HalloweenBoxCount <= 0 {
				continue
			}
			record := accounts[c.AccountID]
			if record == nil {
				record = &domain.HalloweenBoxAccount{}
				accounts[c.AccountID] = record
			}
			switch {
			case c.HalloweenBoxDay > record.Day:
				record.Day, record.Count = c.HalloweenBoxDay, c.HalloweenBoxCount
			case c.HalloweenBoxDay == record.Day:
				record.Count += c.HalloweenBoxCount
			}
		}
	}
	return accounts
}

/*
================
halloweenBoxesRecord

The meta row's bytes for the account plane; empty when no account has
minted, in which case no writer puts a row down. unknown holds the
accounts' stored unknown fields.
================
*/
func halloweenBoxesRecord(accounts map[string]*domain.HalloweenBoxAccount, unknown map[string]unknownFields) (string, error) {
	if len(accounts) == 0 {
		return "", nil
	}
	raw, err := encodePlaneFields(accounts, unknown)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

/*
================
writeHalloweenBoxes
================
*/
func writeHalloweenBoxes(tx *sql.Tx, accounts map[string]*domain.HalloweenBoxAccount) error {
	record, err := halloweenBoxesRecord(accounts, nil)
	if err != nil || record == "" {
		return err
	}
	return upsertMetaTx(tx, metaKeyHalloweenBoxes, record)
}
