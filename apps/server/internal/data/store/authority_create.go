/*
===========================================================================

authority_create.go - creating a fresh authority database from its seed

===========================================================================
*/

package store

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"opensro.online/server/internal/domain"
)

// authoritySeed is the complete current-schema publication unit used to
// create a fresh authority database.
type authoritySeed struct {
	characters map[string][]*domain.Character
	deleted    map[string][]json.RawMessage
	ground     map[string][]domain.GroundItemRecord
	meta       Meta
	updatedAt  int64
}

func emptyAuthoritySeed(updatedAt int64) *authoritySeed {
	return &authoritySeed{
		characters: map[string][]*domain.Character{},
		deleted:    map[string][]json.RawMessage{},
		ground:     map[string][]domain.GroundItemRecord{},
		meta: Meta{
			NextCharID:  map[string]int64{},
			NextGuildID: map[string]int64{},
		},
		updatedAt: updatedAt,
	}
}

// createAuthorityDatabase writes and validates a current-schema database in a
// staging file before atomically publishing it at targetPath.
func createAuthorityDatabase(targetPath string, data *authoritySeed) (err error) {
	dir := filepath.Dir(targetPath)
	tmp, err := os.CreateTemp(dir, tmpPattern(filepath.Base(targetPath)))
	if err != nil {
		return fmt.Errorf("creating database staging file: %w", err)
	}
	tmpPath := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("closing database staging file: %w", err)
	}
	defer func() {
		_ = os.Remove(tmpPath)
		removeDBSidecars(tmpPath)
	}()

	db, err := openDB(tmpPath)
	if err != nil {
		return fmt.Errorf("opening database staging file: %w", err)
	}
	closeDB := func() {
		if db != nil {
			_ = db.Close()
			db = nil
		}
	}
	defer closeDB()

	if err := ensureSchema(db); err != nil {
		return fmt.Errorf("creating authority schema: %w", err)
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("beginning authority seed: %w", err)
	}
	if err := seedAuthorityDBTx(tx, data); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("seeding authority database: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing authority seed: %w", err)
	}
	if err := quickCheck(context.Background(), db); err != nil {
		return fmt.Errorf("validating authority database: %w", err)
	}
	if _, err := loadDB(db, CurrentVersion, CurrentLayoutVersion); err != nil {
		return fmt.Errorf("loading newly created authority database: %w", err)
	}
	if _, err := db.Exec("PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
		return fmt.Errorf("checkpointing authority database: %w", err)
	}
	closeDB()

	file, err := os.OpenFile(tmpPath, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("opening staged database for sync: %w", err)
	}
	if err := syncFile(file); err != nil {
		_ = file.Close()
		return fmt.Errorf("syncing staged database: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("closing staged database after sync: %w", err)
	}
	removeDBSidecars(tmpPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return fmt.Errorf("publishing %s: %w", DBFileName, err)
	}
	return syncDir(dir)
}
