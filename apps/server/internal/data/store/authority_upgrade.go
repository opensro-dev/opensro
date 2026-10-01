/*
===========================================================================

authority_upgrade.go - the offline, preserving authority upgrade

Brings an UpgradeFromVersion authority (record schema 13, table layout 4) to
the current format (schema 14, layout 5). Takes the same exclusive authority
lock as the game server, validates every existing record, keeps an
independent backup, and adds only the two account tables layout 5
introduces: the mall currency and the NPC warehouse. No record is rewritten:
schema 14 only adds optional character fields that schema 13 servers reject.
This operation is never called by server startup or a network request.

===========================================================================
*/
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// UpgradeFromVersion is the record schema the offline upgrade converts, with
// layout preMallLayoutVersion. The release contract reports it so admission
// knows this server can take over a database written by a schema 13 server.
const UpgradeFromVersion = 13

const preMallLayoutVersion = 4

// ErrAuthorityCurrent reports an authority already in the current format: a
// release retried after a committed upgrade has nothing left to do.
var ErrAuthorityCurrent = errors.New("authority upgrade: already in the current format")

/*
================
UpgradeAuthority

Without commit this validates the source and its exclusive ownership only.
With commit it returns the retained backup path, including on a later failure.
Account currencies start absent (zero); no character or inventory is rewritten.
An authority already in the current format answers ErrAuthorityCurrent after
the same validation, never a second upgrade.
================
*/
func UpgradeAuthority(dir string, commit bool) (string, error) {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, DBFileName)
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("authority upgrade: source is not a regular database")
	}
	release, _, err := claimAuthority(dir, time.Now())
	if err != nil {
		return "", err
	}
	defer release()
	db, err := connectDB(path)
	if err != nil {
		return "", err
	}
	defer db.Close()
	if err := quickCheck(db); err != nil {
		return "", err
	}
	schema, err := readMetaInt(db, metaKeySchemaVersion, 0)
	if err != nil {
		return "", err
	}
	layout, err := readMetaInt(db, metaKeyLayoutVersion, 0)
	if err != nil {
		return "", err
	}
	if schema == CurrentVersion && layout == CurrentLayoutVersion {
		if _, err := loadDB(db, CurrentVersion, CurrentLayoutVersion); err != nil {
			return "", fmt.Errorf("authority upgrade: current authority validation: %w", err)
		}
		return "", ErrAuthorityCurrent
	}
	if _, err := loadDB(db, UpgradeFromVersion, preMallLayoutVersion); err != nil {
		return "", fmt.Errorf("authority upgrade: source validation: %w", err)
	}
	for _, table := range []string{"mall_accounts", "account_storage"} {
		var existing int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = ?", table).Scan(&existing); err != nil {
			return "", err
		}
		if existing != 0 {
			return "", fmt.Errorf("authority upgrade: layout 4 unexpectedly contains %s", table)
		}
	}
	if !commit {
		return "", nil
	}
	// A unique, restrictive file prevents replacing any prior recovery copy.
	backup, err := os.CreateTemp(dir, "state.before-mall-*.db")
	if err != nil {
		return "", err
	}
	backupPath := backup.Name()
	if err := backup.Close(); err != nil {
		return backupPath, err
	}
	if _, err := db.Exec("VACUUM INTO ?", backupPath); err != nil {
		return backupPath, fmt.Errorf("authority upgrade: backup: %w", err)
	}
	file, err := os.OpenFile(backupPath, os.O_RDWR, 0)
	if err != nil {
		return backupPath, err
	}
	syncErr := file.Sync()
	closeErr := file.Close()
	if syncErr != nil {
		return backupPath, syncErr
	}
	if closeErr != nil {
		return backupPath, closeErr
	}
	if err := syncDir(dir); err != nil {
		return backupPath, err
	}
	if _, err := db.Exec("PRAGMA synchronous=FULL"); err != nil {
		return backupPath, err
	}
	tx, err := db.Begin()
	if err != nil {
		return backupPath, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(mallAccountsSchema + accountStorageSchema); err != nil {
		return backupPath, err
	}
	if err := upsertMetaTx(tx, metaKeyLayoutVersion, fmt.Sprint(CurrentLayoutVersion)); err != nil {
		return backupPath, err
	}
	if err := upsertMetaTx(tx, metaKeySchemaVersion, fmt.Sprint(CurrentVersion)); err != nil {
		return backupPath, err
	}
	if err := tx.Commit(); err != nil {
		return backupPath, err
	}
	if _, err := loadDB(db, CurrentVersion, CurrentLayoutVersion); err != nil {
		return backupPath, fmt.Errorf("authority upgrade committed, validation failed; retain backup %s: %w", backupPath, err)
	}
	return backupPath, nil
}
