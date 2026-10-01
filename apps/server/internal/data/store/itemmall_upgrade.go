/*
===========================================================================

itemmall_upgrade.go - offline, preserving layout 4 to 5 authority upgrade

Takes the same exclusive authority lock as the game server. Validates every
existing record, keeps an independent backup and adds only the currency table.
This operation is never called by server startup or a network request.

===========================================================================
*/
package store

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const preMallLayoutVersion = 4

/*
================
UpgradeMallAuthority

Without commit this validates the source and its exclusive ownership only.
With commit it returns the retained backup path, including on a later failure.
Account currencies start absent (zero); no character or inventory is rewritten.
================
*/
func UpgradeMallAuthority(dir string, commit bool) (string, error) {
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
	if _, err := loadDB(db, CurrentVersion, preMallLayoutVersion); err != nil {
		return "", fmt.Errorf("authority upgrade: source validation: %w", err)
	}
	var existing int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name = ?", "mall_accounts").Scan(&existing); err != nil {
		return "", err
	}
	if existing != 0 {
		return "", fmt.Errorf("authority upgrade: layout 4 unexpectedly contains mall_accounts")
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
	if _, err := tx.Exec(mallAccountsSchema); err != nil {
		return backupPath, err
	}
	if err := upsertMetaTx(tx, metaKeyLayoutVersion, fmt.Sprint(CurrentLayoutVersion)); err != nil {
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
