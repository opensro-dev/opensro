/*
===========================================================================

accounts.go - the live global account authority

The Agent is the only holder of login credentials. Accounts live in one
SQLite database the Agent owns, so a new account (a website signup) takes
effect at once, with no redeploy. The deploy-time catalog (catalog.go) is a
seed: its rows are inserted when absent and never overwrite a stored
account, so a password changed here is not reverted by the next deploy.

Account ids are stored exactly as created, and lookups are exact, because
characters are owned by the exact id. A unique index on the ASCII-folded id
stops a second account that differs only in case ("Bob" beside "bob").

A disabled account keeps its row (its characters still have an owner) but
cannot log in: PasswordHash reports it absent, which also ends its browser
sessions at their next check (browser_session.go).

===========================================================================
*/
package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
	"opensro.online/server/internal/domain"
)

const (
	// AccountBcryptCost is the cost of hashes this store creates. It stays
	// inside the catalog's accepted range so a store row can always be
	// exported back into a seed catalog.
	AccountBcryptCost = MaxBcryptCost

	// MinPasswordBytes and MaxPasswordBytes bound new passwords. bcrypt
	// ignores bytes past 72, so a longer password would silently truncate.
	MinPasswordBytes = 8
	MaxPasswordBytes = 72

	// AgentProvisioningTokenFile is the cluster-state file holding the
	// provisioning API bearer token (internal/agent/provisioning), and
	// MinProvisioningTokenBytes the shortest token the API accepts.
	AgentProvisioningTokenFile = "agent-provisioning-token"
	MinProvisioningTokenBytes  = 32

	accountsSchemaVersion = 1
	accountsBusyTimeoutMS = 5000
)

var (
	ErrAccountExists    = errors.New("account already exists")
	ErrAccountNotFound  = errors.New("account not found")
	ErrAccountIDInvalid = errors.New("account id is invalid")
	ErrPasswordInvalid  = fmt.Errorf("password must be %d to %d bytes", MinPasswordBytes, MaxPasswordBytes)
)

// AccountInfo is the public view of one account: never its hash.
type AccountInfo struct {
	ID        string
	CreatedAt time.Time
	Disabled  bool
}

// Accounts is the SQLite-backed account authority.
type Accounts struct {
	db *sql.DB

	// hashing bounds concurrent bcrypt work so account creation cannot
	// starve logins of CPU.
	hashing chan struct{}

	closeOnce sync.Once
}

const accountsSchema = `
CREATE TABLE IF NOT EXISTS accounts (
	id            TEXT    NOT NULL PRIMARY KEY,
	password_hash BLOB    NOT NULL,
	created_at_ms INTEGER NOT NULL,
	disabled      INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1))
) STRICT;
CREATE UNIQUE INDEX IF NOT EXISTS accounts_folded_id ON accounts (lower(id));
`

/*
================
OpenAccounts

Opens (creating when absent) the account database at path. The database is
the login authority, so it must be a regular file: symlinks are refused as
catalog.go refuses them.
================
*/
func OpenAccounts(path string) (*Accounts, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("account database path is empty")
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("account database %s is not a regular file", path)
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	// One connection serializes writers; account traffic is tiny.
	db.SetMaxOpenConns(1)
	pragmas := []string{
		fmt.Sprintf("PRAGMA busy_timeout=%d", accountsBusyTimeoutMS),
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=FULL",
		"PRAGMA foreign_keys=ON",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		_ = db.Close()
		return nil, err
	}
	if version > accountsSchemaVersion {
		_ = db.Close()
		return nil, fmt.Errorf(
			"account database schema %d is newer than this Agent (%d)",
			version,
			accountsSchemaVersion,
		)
	}
	if _, err := db.Exec(accountsSchema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("account schema: %w", err)
	}
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version=%d", accountsSchemaVersion)); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &Accounts{
		db:      db,
		hashing: make(chan struct{}, 2),
	}, nil
}

/*
================
Close
================
*/
func (accounts *Accounts) Close() error {
	var err error
	accounts.closeOnce.Do(func() {
		err = accounts.db.Close()
	})
	return err
}

/*
================
Seed

Inserts every catalog account the store does not already hold, under its
catalog hash. Existing rows, including any whose id differs only in case,
are left alone. Returns how many rows were inserted.
================
*/
func (accounts *Accounts) Seed(catalog *Catalog) (int, error) {
	if catalog == nil {
		return 0, nil
	}
	tx, err := accounts.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UnixMilli()
	inserted := 0
	for _, id := range catalog.IDs() {
		hash, _ := catalog.PasswordHash(id)
		result, err := tx.Exec(
			`INSERT INTO accounts (id, password_hash, created_at_ms)
			 SELECT ?, ?, ?
			 WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE lower(id) = lower(?))`,
			id, hash, now, id,
		)
		if err != nil {
			return 0, fmt.Errorf("seed account %q: %w", id, err)
		}
		if rows, _ := result.RowsAffected(); rows == 1 {
			inserted++
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

/*
================
PasswordHash

The login hash for an enabled account. A missing or disabled account, or a
database error, reports false: the authority fails closed.
================
*/
func (accounts *Accounts) PasswordHash(accountID string) ([]byte, bool) {
	var hash []byte
	err := accounts.db.QueryRow(
		`SELECT password_hash FROM accounts WHERE id = ? AND disabled = 0`,
		accountID,
	).Scan(&hash)
	if err != nil {
		return nil, false
	}
	return hash, true
}

/*
================
IDs

Every account id, disabled ones included: GameWorld uses the set to prove
each durable character has an owner, and a disabled owner still owns.
Returns nil on a database error, which GameWorld treats as a refusal.
================
*/
func (accounts *Accounts) IDs() []string {
	rows, err := accounts.db.Query(`SELECT id FROM accounts`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil
	}
	sort.Strings(ids)
	return ids
}

/*
================
Len
================
*/
func (accounts *Accounts) Len() int {
	var count int
	if err := accounts.db.QueryRow(`SELECT count(*) FROM accounts`).Scan(&count); err != nil {
		return 0
	}
	return count
}

/*
================
Lookup
================
*/
func (accounts *Accounts) Lookup(accountID string) (AccountInfo, error) {
	var createdAt int64
	var disabled int
	err := accounts.db.QueryRow(
		`SELECT created_at_ms, disabled FROM accounts WHERE id = ?`,
		accountID,
	).Scan(&createdAt, &disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return AccountInfo{}, ErrAccountNotFound
	}
	if err != nil {
		return AccountInfo{}, err
	}
	return AccountInfo{
		ID:        accountID,
		CreatedAt: time.UnixMilli(createdAt).UTC(),
		Disabled:  disabled == 1,
	}, nil
}

/*
================
Create

Creates an enabled account. The id must satisfy domain.AccountIDValid, must
not be reserved, and must not match an existing id case-insensitively.
================
*/
func (accounts *Accounts) Create(ctx context.Context, accountID, password string) (AccountInfo, error) {
	if !domain.AccountIDValid(accountID) || accountID == domain.ReservedAccountID {
		return AccountInfo{}, ErrAccountIDInvalid
	}
	hash, err := accounts.hash(ctx, password)
	if err != nil {
		return AccountInfo{}, err
	}
	now := time.Now().UTC()
	result, err := accounts.db.ExecContext(
		ctx,
		`INSERT INTO accounts (id, password_hash, created_at_ms)
		 SELECT ?, ?, ?
		 WHERE NOT EXISTS (SELECT 1 FROM accounts WHERE lower(id) = lower(?))`,
		accountID, hash, now.UnixMilli(), accountID,
	)
	if err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed") {
		// Another writer won the race the NOT EXISTS guard could not see.
		return AccountInfo{}, ErrAccountExists
	}
	if err != nil {
		return AccountInfo{}, err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return AccountInfo{}, ErrAccountExists
	}
	return AccountInfo{ID: accountID, CreatedAt: time.UnixMilli(now.UnixMilli()).UTC()}, nil
}

/*
================
Verify

Whether password opens an enabled account. A missing account still pays
one bcrypt comparison so response time does not reveal which ids exist.
================
*/
func (accounts *Accounts) Verify(ctx context.Context, accountID, password string) (bool, error) {
	if len(password) == 0 || len(password) > MaxPasswordBytes {
		return false, nil
	}
	if err := accounts.acquire(ctx); err != nil {
		return false, err
	}
	defer accounts.release()
	hash, found := accounts.PasswordHash(accountID)
	if !found {
		hash = verifyDummyHash()
	}
	return found && bcrypt.CompareHashAndPassword(hash, []byte(password)) == nil, nil
}

/*
================
SetPassword
================
*/
func (accounts *Accounts) SetPassword(ctx context.Context, accountID, password string) error {
	hash, err := accounts.hash(ctx, password)
	if err != nil {
		return err
	}
	result, err := accounts.db.ExecContext(
		ctx,
		`UPDATE accounts SET password_hash = ? WHERE id = ?`,
		hash, accountID,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrAccountNotFound
	}
	return nil
}

/*
================
SetDisabled
================
*/
func (accounts *Accounts) SetDisabled(ctx context.Context, accountID string, disabled bool) error {
	flag := 0
	if disabled {
		flag = 1
	}
	result, err := accounts.db.ExecContext(
		ctx,
		`UPDATE accounts SET disabled = ? WHERE id = ?`,
		flag, accountID,
	)
	if err != nil {
		return err
	}
	if rows, _ := result.RowsAffected(); rows != 1 {
		return ErrAccountNotFound
	}
	return nil
}

//============================================================================

/*
================
hash
================
*/
func (accounts *Accounts) hash(ctx context.Context, password string) ([]byte, error) {
	if len(password) < MinPasswordBytes || len(password) > MaxPasswordBytes {
		return nil, ErrPasswordInvalid
	}
	if err := accounts.acquire(ctx); err != nil {
		return nil, err
	}
	defer accounts.release()
	return bcrypt.GenerateFromPassword([]byte(password), AccountBcryptCost)
}

/*
================
acquire
================
*/
func (accounts *Accounts) acquire(ctx context.Context) error {
	select {
	case accounts.hashing <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

/*
================
release
================
*/
func (accounts *Accounts) release() {
	<-accounts.hashing
}

var (
	dummyHashOnce sync.Once
	dummyHash     []byte
)

/*
================
verifyDummyHash

A hash at the store's cost that no password matches.
================
*/
func verifyDummyHash() []byte {
	dummyHashOnce.Do(func() {
		dummyHash, _ = bcrypt.GenerateFromPassword([]byte("\x00unmatchable\x00"), AccountBcryptCost)
	})
	return dummyHash
}
