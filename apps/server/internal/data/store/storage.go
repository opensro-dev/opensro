/*
===========================================================================

storage.go - the account warehouse rows and their atomic transfers

One JSON record per account. A transfer commits the warehouse row and the
complete character record in one transaction, so an item or gold amount is
never in both places, or in neither, after a crash.

===========================================================================
*/
package store

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"opensro.online/server/internal/domain"
)

// accountStorageSchema is part of the layout-5 account tables.
const accountStorageSchema = `
CREATE TABLE IF NOT EXISTS account_storage (
  account_id TEXT PRIMARY KEY,
  record     TEXT NOT NULL
) WITHOUT ROWID;
`

/*
================
AccountStorage
================
*/
func (s *Store) AccountStorage(character *domain.Character) (domain.AccountStorage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if character == nil || character.AccountID == "" || s.db == nil {
		return domain.AccountStorage{}, fmt.Errorf("storage: account unavailable")
	}
	if _, known := s.charDivision[character]; !known {
		return domain.AccountStorage{}, fmt.Errorf("storage: unknown character")
	}
	return readAccountStorage(s.db, character.AccountID)
}

/*
================
TransactStorage
================
*/
func (s *Store) TransactStorage(character *domain.Character, mutate func(next *domain.Character, storage *domain.AccountStorage) error) (domain.AccountStorage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if character == nil || character.AccountID == "" || character.DeletePending || mutate == nil || s.db == nil {
		return domain.AccountStorage{}, fmt.Errorf("storage: transfer unavailable")
	}
	division, known := s.charDivision[character]
	if !known {
		return domain.AccountStorage{}, fmt.Errorf("storage: unknown character")
	}
	current, err := readAccountStorage(s.db, character.AccountID)
	if err != nil {
		return current, err
	}
	next := character.Snapshot()
	storage := current
	storage.Rows = append([]domain.InventoryRow(nil), current.Rows...)
	for i := range storage.Rows {
		storage.Rows[i].Summon = domain.CloneCOS(current.Rows[i].Summon)
		storage.Rows[i].MagicOptions = append([]uint64(nil), current.Rows[i].MagicOptions...)
	}
	if err := mutate(next, &storage); err != nil {
		return current, err
	}
	if err := validateAccountStorage(storage); err != nil {
		return current, err
	}
	// Only the inventory and gold cross into the warehouse transaction; any
	// other field the callback touched is not committed.
	committed := *character
	committed.MissionInventory = next.MissionInventory
	committed.Gold = next.Gold
	if err := s.commitStorageLocked(division, &committed, storage); err != nil {
		s.recordWriteFailureLocked("storage-transfer", err)
		return current, err
	}
	character.MissionInventory = next.MissionInventory
	character.Gold = next.Gold
	if s.changes.empty() {
		s.recordWriteSuccessLocked()
	}
	return storage, nil
}

/*
================
commitStorageLocked
================
*/
func (s *Store) commitStorageLocked(division string, character *domain.Character, storage domain.AccountStorage) error {
	if s.commitFail != nil {
		return s.commitFail
	}
	record, err := json.Marshal(storage)
	if err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(`INSERT INTO account_storage (account_id, record) VALUES (?, ?)
ON CONFLICT(account_id) DO UPDATE SET record = excluded.record`, character.AccountID, string(record))
	if err != nil {
		return err
	}
	if err := upsertCharacterTx(tx, division, character); err != nil {
		return err
	}
	return tx.Commit()
}

/*
================
readAccountStorage

An account without a row has never stored anything: the default warehouse.
A row that fails to decode or validate is an error, never an empty room.
================
*/
func readAccountStorage(db *sql.DB, account string) (domain.AccountStorage, error) {
	var record string
	err := db.QueryRow("SELECT record FROM account_storage WHERE account_id = ?", account).Scan(&record)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.NewAccountStorage(), nil
	}
	if err != nil {
		return domain.AccountStorage{}, err
	}
	return decodeAccountStorage(record)
}

/*
================
decodeAccountStorage
================
*/
func decodeAccountStorage(record string) (domain.AccountStorage, error) {
	var storage domain.AccountStorage
	dec := json.NewDecoder(bytes.NewReader([]byte(record)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&storage); err != nil {
		return storage, fmt.Errorf("storage: record: %w", err)
	}
	return storage, validateAccountStorage(storage)
}

/*
================
validateAccountStorage

Capacity fits the list's byte, gold is non-negative and every row owns one
slot inside the capacity.
================
*/
func validateAccountStorage(storage domain.AccountStorage) error {
	if storage.Capacity < 1 || storage.Capacity > domain.StorageMaxCapacity || storage.Gold < 0 {
		return fmt.Errorf("storage: invalid capacity %d or gold %d", storage.Capacity, storage.Gold)
	}
	seen := map[int64]bool{}
	for _, row := range storage.Rows {
		if row.Slot < 0 || row.Slot >= storage.Capacity || seen[row.Slot] || row.RefObjID == 0 || row.Codename == "" {
			return fmt.Errorf("storage: invalid row at slot %d", row.Slot)
		}
		seen[row.Slot] = true
	}
	return nil
}

/*
================
validateAccountStorages

Validate every stored warehouse at boot, including accounts without
characters.
================
*/
func validateAccountStorages(db *sql.DB) error {
	rows, err := db.Query("SELECT account_id, record FROM account_storage")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var account, record string
		if err := rows.Scan(&account, &record); err != nil {
			return err
		}
		if account == "" {
			return fmt.Errorf("storage: warehouse without account identity")
		}
		if _, err := decodeAccountStorage(record); err != nil {
			return fmt.Errorf("storage: account %s: %w", account, err)
		}
	}
	return rows.Err()
}
