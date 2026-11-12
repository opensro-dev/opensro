/*
===========================================================================

sookarenaaccounts.go - the per-account survival arena daily entry count

Port-only, not native (#3322; SRO_SOOK_SURVIVAL_ARENA). The three
entries a UTC day count per account, so the count lives here, in the
metadata plane, keyed by account id: every character of an account
enters against the one record. An entry mutates the record inside its
door and the commit stages it by digest, exactly like a character
record; the store owns the pointers. Older authorities have no
sookArenaAccounts row: at first load the plane is seeded from the
characters' schema 42 per-character counts, so the day's entries
survive the move. The Halloween box plane (halloweenboxes.go) is the
same shape.

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

const metaKeySookArenaAccounts = "sookArenaAccounts"

/*
================
Store.SookArenaAccount

The account's live entry record, created empty on first touch. The
pointer is the store's record: read or mutate it only inside a write
door (the arena entry), never across one. An empty account id is a
fixture character with no account and shares the empty key's record,
so it stays capped like any other.
================
*/
func (s *Store) SookArenaAccount(accountID string) *domain.SookArenaAccount {
	// Every touch marks the plane for the next commit: the record's
	// fields mutate through the returned pointer where the commit
	// cannot see them, so the touch is its only signal (#3164). A
	// read-only touch costs one marshal the digest then skips.
	s.changes.sookArenaAccounts = true
	if s.meta.SookArenaAccounts == nil {
		s.meta.SookArenaAccounts = map[string]*domain.SookArenaAccount{}
	}
	record := s.meta.SookArenaAccounts[accountID]
	if record == nil {
		record = &domain.SookArenaAccount{}
		s.meta.SookArenaAccounts[accountID] = record
	}
	return record
}

/*
================
loadSookArenaAccounts

The second result reports whether the row exists at all: an authority
that predates the account count has none, and its plane is seeded from
the legacy character counts instead (seedSookArenaAccounts). Unknown
fields or a count past the daily allowance are corruption, not a
reason to silently hand an account more entries.
================
*/
func loadSookArenaAccounts(db *sql.DB) (map[string]*domain.SookArenaAccount, bool, error) {
	var raw string
	err := db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKeySookArenaAccounts).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]*domain.SookArenaAccount{}, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	accounts := map[string]*domain.SookArenaAccount{}
	if err := decodeJSONStrict([]byte(raw), &accounts); err != nil {
		return nil, false, fmt.Errorf("survival arena accounts: %w", err)
	}
	for accountID, record := range accounts {
		if strings.TrimSpace(accountID) != accountID || record == nil || record.Day < 0 || record.Entries > domain.SookArenaDailyEntries {
			return nil, false, fmt.Errorf("invalid survival arena account %q", accountID)
		}
	}
	return accounts, true, nil
}

/*
================
seedSookArenaAccounts

The first-load derivation from the schema 42 per-character counts: per
account, the latest day any character entered on, and that day's
entries summed over the account's characters. The sum saturates at the
daily allowance: past it the account is simply spent for the day, and
the cap keeps the record inside what loadSookArenaAccounts accepts.
Characters without an account id or an entry seed nothing.
================
*/
func seedSookArenaAccounts(characters map[string][]*domain.Character) map[string]*domain.SookArenaAccount {
	accounts := map[string]*domain.SookArenaAccount{}
	for _, division := range characters {
		for _, c := range division {
			if c == nil || c.AccountID == "" || c.SookArenaEntries == 0 || c.SookArenaDay <= 0 {
				continue
			}
			record := accounts[c.AccountID]
			if record == nil {
				record = &domain.SookArenaAccount{}
				accounts[c.AccountID] = record
			}
			entries := min(c.SookArenaEntries, domain.SookArenaDailyEntries)
			switch {
			case c.SookArenaDay > record.Day:
				record.Day, record.Entries = c.SookArenaDay, entries
			case c.SookArenaDay == record.Day:
				record.Entries = min(record.Entries+entries, domain.SookArenaDailyEntries)
			}
		}
	}
	return accounts
}

/*
================
sookArenaAccountsRecord

The meta row's bytes for the account plane; empty when no account has
entered, in which case no writer puts a row down.
================
*/
func sookArenaAccountsRecord(accounts map[string]*domain.SookArenaAccount) (string, error) {
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
writeSookArenaAccounts
================
*/
func writeSookArenaAccounts(tx *sql.Tx, accounts map[string]*domain.SookArenaAccount) error {
	record, err := sookArenaAccountsRecord(accounts)
	if err != nil || record == "" {
		return err
	}
	return upsertMetaTx(tx, metaKeySookArenaAccounts, record)
}
