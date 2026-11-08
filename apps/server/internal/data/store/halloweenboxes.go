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
	"encoding/json"
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
the legacy character counts instead (seedHalloweenBoxes). Unknown
fields or a negative count are corruption, not a reason to silently
discard a day's progress.
================
*/
func loadHalloweenBoxes(db *sql.DB) (map[string]*domain.HalloweenBoxAccount, bool, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKeyHalloweenBoxes).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]*domain.HalloweenBoxAccount{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	accounts := map[string]*domain.HalloweenBoxAccount{}
	if err := decodeJSONStrict([]byte(raw), &accounts); err != nil {
		return nil, false, fmt.Errorf("halloween box accounts: %w", err)
	}
	for accountID, record := range accounts {
		if strings.TrimSpace(accountID) != accountID || record == nil || record.Count < 0 || record.GourdCount < 0 {
			return nil, false, fmt.Errorf("invalid halloween box account %q", accountID)
		}
	}
	return accounts, true, nil
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
minted, in which case no writer puts a row down.
================
*/
func halloweenBoxesRecord(accounts map[string]*domain.HalloweenBoxAccount) (string, error) {
	if len(accounts) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(accounts)
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
	record, err := halloweenBoxesRecord(accounts)
	if err != nil || record == "" {
		return err
	}
	return upsertMetaTx(tx, metaKeyHalloweenBoxes, record)
}
