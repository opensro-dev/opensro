/*
===========================================================================

sqlitestore.go - the settings store: overrides, proposals and the audit

Port-only, not native (owner decision 2026-10-15: a separate database
next to the shard's other operator stores, never inside the authority
store). settings.db in the shard's state directory holds the approved
overrides, the pending proposals and an append-only audit; it holds no
player data. Its name ends in .db so the host backup snapshots it online
with SQLite's backup API and checks its integrity (ops/backup), like the
authority store, rather than copying a live WAL.

An older GameWorld never opens the file, so a revert is a normal release:
the file stays, ignored, until a build that reads it returns. Its own
schema version lives in user_version; a newer file is refused, never
written by an older binary.

===========================================================================
*/
package settingsapi

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"opensro.online/server/internal/game/tunables"

	// The pure-Go SQLite driver, as platform/history uses.
	_ "modernc.org/sqlite"
)

// FileName is the store's file in the shard's state directory.
const FileName = "settings.db"

// storeVersion is the file's schema (PRAGMA user_version).
const storeVersion = 1

/*
================
SQLiteStore
================
*/
type SQLiteStore struct {
	db *sql.DB
}

/*
================
OpenSQLiteStore

Opens or creates path. A file written by a newer schema is refused.
================
*/
func OpenSQLiteStore(path string) (*SQLiteStore, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &SQLiteStore{db: db}
	if err := s.initialize(); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

/*
================
SQLiteStore.initialize
================
*/
func (s *SQLiteStore) initialize() error {
	// The pool holds one connection (SetMaxOpenConns(1)), so these
	// per-connection pragmas hold for every statement, as in history.
	if _, err := s.db.Exec(`PRAGMA journal_mode=WAL; PRAGMA synchronous=FULL; PRAGMA busy_timeout=5000;`); err != nil {
		return err
	}
	var version int
	if err := s.db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > storeVersion {
		return fmt.Errorf("settings store: schema %d is newer than this build's %d", version, storeVersion)
	}
	_, err := s.db.Exec(fmt.Sprintf(`
CREATE TABLE IF NOT EXISTS overrides (key TEXT PRIMARY KEY, value TEXT NOT NULL, at INTEGER NOT NULL, by TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS proposals (id TEXT PRIMARY KEY, key TEXT NOT NULL UNIQUE, from_value TEXT NOT NULL,
 to_value TEXT NOT NULL, by TEXT NOT NULL, by_name TEXT NOT NULL, at INTEGER NOT NULL, reason TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audit (seq INTEGER PRIMARY KEY AUTOINCREMENT, at INTEGER NOT NULL, who TEXT NOT NULL,
 name TEXT NOT NULL, action TEXT NOT NULL, key TEXT NOT NULL, from_value TEXT NOT NULL, to_value TEXT NOT NULL,
 reason TEXT NOT NULL, result TEXT NOT NULL, error TEXT NOT NULL);
CREATE TRIGGER IF NOT EXISTS audit_append_only_update BEFORE UPDATE ON audit BEGIN SELECT RAISE(ABORT, 'audit is append-only'); END;
CREATE TRIGGER IF NOT EXISTS audit_append_only_delete BEFORE DELETE ON audit BEGIN SELECT RAISE(ABORT, 'audit is append-only'); END;
PRAGMA user_version = %d;`, storeVersion))
	return err
}

/*
================
SQLiteStore.Close
================
*/
func (s *SQLiteStore) Close() error { return s.db.Close() }

/*
================
SQLiteStore.Overrides
================
*/
func (s *SQLiteStore) Overrides() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM overrides`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

/*
================
SQLiteStore.Pending
================
*/
func (s *SQLiteStore) Pending() ([]Proposal, error) {
	rows, err := s.db.Query(`SELECT id, key, from_value, to_value, by, by_name, at, reason FROM proposals ORDER BY at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Proposal
	for rows.Next() {
		var p Proposal
		if err := rows.Scan(&p.ID, &p.Key, &p.From, &p.To, &p.By, &p.ByName, &p.At, &p.Reason); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

/*
================
SQLiteStore.Propose
================
*/
func (s *SQLiteStore) Propose(p Proposal, entry AuditEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var pending int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM proposals WHERE key = ?`, p.Key).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return ErrConflict
	}
	if _, err := tx.Exec(`INSERT INTO proposals VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Key, p.From, p.To, p.By, p.ByName, p.At, p.Reason); err != nil {
		return err
	}
	if err := insertAudit(tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

/*
================
SQLiteStore.Decide
================
*/
func (s *SQLiteStore) Decide(id string, check func(Proposal) (AuditEntry, bool, error)) (Proposal, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Proposal{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var p Proposal
	err = tx.QueryRow(`SELECT id, key, from_value, to_value, by, by_name, at, reason FROM proposals WHERE id = ?`, id).
		Scan(&p.ID, &p.Key, &p.From, &p.To, &p.By, &p.ByName, &p.At, &p.Reason)
	if errors.Is(err, sql.ErrNoRows) {
		return Proposal{}, ErrNotFound
	}
	if err != nil {
		return Proposal{}, err
	}
	entry, override, err := check(p)
	if err != nil {
		return p, err
	}
	if _, err := tx.Exec(`DELETE FROM proposals WHERE id = ?`, id); err != nil {
		return p, err
	}
	if override && p.To == tunables.ClearOverride {
		if _, err := tx.Exec(`DELETE FROM overrides WHERE key = ?`, p.Key); err != nil {
			return p, err
		}
	} else if override {
		if _, err := tx.Exec(`INSERT INTO overrides (key, value, at, by) VALUES (?, ?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, at = excluded.at, by = excluded.by`,
			p.Key, p.To, entry.At, entry.Who); err != nil {
			return p, err
		}
	}
	if err := insertAudit(tx, entry); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

/*
================
SQLiteStore.Refuse
================
*/
func (s *SQLiteStore) Refuse(entry AuditEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := insertAudit(tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

/*
================
SQLiteStore.DropOverride
================
*/
func (s *SQLiteStore) DropOverride(key string, entry AuditEntry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.Exec(`DELETE FROM overrides WHERE key = ?`, key); err != nil {
		return err
	}
	if err := insertAudit(tx, entry); err != nil {
		return err
	}
	return tx.Commit()
}

/*
================
SQLiteStore.Audit

Newest first; before (a seq) pages back, 0 starts at the newest.
================
*/
func (s *SQLiteStore) Audit(limit int, before int64) ([]AuditEntry, error) {
	query, args := `SELECT seq, at, who, name, action, key, from_value, to_value, reason, result, error FROM audit`, []any{}
	if before > 0 {
		query += ` WHERE seq < ?`
		args = append(args, before)
	}
	query += ` ORDER BY seq DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEntry
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.Seq, &e.At, &e.Who, &e.Name, &e.Action, &e.Key, &e.From, &e.To, &e.Reason, &e.Result,
			&e.Error); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

/*
================
insertAudit
================
*/
func insertAudit(tx *sql.Tx, e AuditEntry) error {
	_, err := tx.Exec(`INSERT INTO audit (at, who, name, action, key, from_value, to_value, reason, result, error)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, e.At, e.Who, e.Name, e.Action, e.Key, e.From, e.To, e.Reason, e.Result, e.Error)
	return err
}
