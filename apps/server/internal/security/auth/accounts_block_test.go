/*
===========================================================================

accounts_block_test.go - the staff block's storage contract (#2271 PR-D)

===========================================================================
*/
package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

/*
================
TestBlockRoundTrip
================
*/
func TestBlockRoundTrip(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	accounts, err := OpenAccountsWith(path, testAccountsOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	if _, err := accounts.Create(ctx, "mallory", "password1"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := accounts.Verify(ctx, "mallory", "password1"); !ok {
		t.Fatal("fresh account does not verify")
	}
	const until = int64(1893456000000)
	if err := accounts.SetBlock(ctx, "mallory", "chargeback review", until); err != nil {
		t.Fatal(err)
	}
	info, err := accounts.Lookup("mallory")
	if err != nil {
		t.Fatal(err)
	}
	if !info.Disabled || info.BlockReason != "chargeback review" || info.BlockUntilMs != until {
		t.Fatalf("lookup after block = %+v", info)
	}
	if disabled, reason, gotUntil, found := accounts.Block("mallory"); !found || !disabled ||
		reason != "chargeback review" || gotUntil != until {
		t.Fatalf("Block() = %v %q %d %v", disabled, reason, gotUntil, found)
	}
	if ok, _ := accounts.Verify(ctx, "mallory", "password1"); ok {
		t.Fatal("blocked account still verifies")
	}
	if _, found := accounts.PasswordHash("mallory"); found {
		t.Fatal("blocked account still has a login hash")
	}
	if err := accounts.ClearBlock(ctx, "mallory"); err != nil {
		t.Fatal(err)
	}
	info, err = accounts.Lookup("mallory")
	if err != nil {
		t.Fatal(err)
	}
	if info.Disabled || info.BlockReason != "" || info.BlockUntilMs != 0 {
		t.Fatalf("lookup after clear = %+v", info)
	}
	if ok, _ := accounts.Verify(ctx, "mallory", "password1"); !ok {
		t.Fatal("cleared account does not verify")
	}
	// Enabling through the plain switch clears a block too.
	if err := accounts.SetBlock(ctx, "mallory", "second look", 0); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetDisabled(ctx, "mallory", false); err != nil {
		t.Fatal(err)
	}
	if _, reason, _, _ := accounts.Block("mallory"); reason != "" {
		t.Fatalf("plain enable left block reason %q", reason)
	}
}

/*
================
TestBlockRevokesSessions

Blocking and clearing both advance the session epoch and notify the
revoke observer, exactly like the plain disable switch: live sessions
die on the block and stay dead after it lifts.
================
*/
func TestBlockRevokesSessions(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	accounts, err := OpenAccountsWith(path, testAccountsOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	if _, err := accounts.Create(ctx, "hero", "password1"); err != nil {
		t.Fatal(err)
	}
	var observed []uint64
	accounts.SetRevokeObserver(func(accountID string, epoch uint64) {
		if accountID == "hero" {
			observed = append(observed, epoch)
		}
	})
	if err := accounts.SetBlock(ctx, "hero", "chargeback review", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := accounts.SessionEpoch("hero"); ok {
		t.Fatal("blocked account reported an active SessionEpoch")
	}
	if err := accounts.ClearBlock(ctx, "hero"); err != nil {
		t.Fatal(err)
	}
	if epoch, ok := accounts.SessionEpoch("hero"); !ok || epoch != 3 {
		t.Fatalf("after ClearBlock SessionEpoch = (%d, %v), want (3, true)", epoch, ok)
	}
	if len(observed) != 2 || observed[0] != 2 || observed[1] != 3 {
		t.Fatalf("observed epochs = %v, want [2 3] (block bumps, clear bumps)", observed)
	}
}

/*
================
TestBlockValidation
================
*/
func TestBlockValidation(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	accounts, err := OpenAccountsWith(path, testAccountsOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	if _, err := accounts.Create(ctx, "mallory", "password1"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		reason  string
		untilMs int64
	}{
		{"empty", "", 0},
		{"blank", "   ", 0},
		{"separator", "bad # reason", 0},
		{"too long", strings.Repeat("x", MaxBlockReasonChars+1), 0},
		{"negative end", "fair reason", -1},
	} {
		if err := accounts.SetBlock(ctx, "mallory", tc.reason, tc.untilMs); !errors.Is(err, ErrBlockInvalid) {
			t.Fatalf("%s: SetBlock = %v, want ErrBlockInvalid", tc.name, err)
		}
	}
	if err := accounts.SetBlock(ctx, "mallory", strings.Repeat("x", MaxBlockReasonChars), 0); err != nil {
		t.Fatalf("longest allowed reason refused: %v", err)
	}
	if err := accounts.SetBlock(ctx, "ghost", "fair reason", 0); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("SetBlock(ghost) = %v", err)
	}
	if err := accounts.ClearBlock(ctx, "ghost"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("ClearBlock(ghost) = %v", err)
	}
	if _, _, _, found := accounts.Block("ghost"); found {
		t.Fatal("Block(ghost) reported found")
	}
}

/*
================
writeV1AccountsDB

A pre-#2271 database: the schema exactly as schema version 1 created
it, stamped user_version 1, holding one enabled account.
================
*/
func writeV1AccountsDB(t *testing.T, path, id, password string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	const schema = `
CREATE TABLE accounts (
	id            TEXT    NOT NULL PRIMARY KEY,
	password_hash BLOB    NOT NULL,
	created_at_ms INTEGER NOT NULL,
	disabled      INTEGER NOT NULL DEFAULT 0 CHECK (disabled IN (0, 1)),
	session_epoch INTEGER NOT NULL DEFAULT 1
) STRICT;
CREATE UNIQUE INDEX accounts_folded_id ON accounts (lower(id));
PRAGMA user_version=1;`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, password_hash, created_at_ms) VALUES (?, ?, ?)`,
		id, hash, int64(1700000000000),
	); err != nil {
		t.Fatal(err)
	}
}

/*
================
TestBlockColumnsAddedToV1Database

An existing version-1 database gains the block columns at open, keeps
its rows, and accepts blocks afterwards.
================
*/
func TestBlockColumnsAddedToV1Database(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	writeV1AccountsDB(t, path, "legacy", "password1")
	accounts, err := OpenAccountsWith(path, testAccountsOptions)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	if ok, _ := accounts.Verify(ctx, "legacy", "password1"); !ok {
		t.Fatal("legacy account does not verify after the upgrade")
	}
	if disabled, reason, _, found := accounts.Block("legacy"); !found || disabled || reason != "" {
		t.Fatalf("Block(legacy) = %v %q found=%v", disabled, reason, found)
	}
	if err := accounts.SetBlock(ctx, "legacy", "inherited account", 0); err != nil {
		t.Fatalf("block on upgraded database: %v", err)
	}
	if _, reason, _, _ := accounts.Block("legacy"); reason != "inherited account" {
		t.Fatalf("Block(legacy) reason = %q", reason)
	}
}

/*
================
TestBlockColumnsToleratedByV1Statements

The rollback proof: after this binary has blocked an account, the
previous Agent's own statements (its column-named reads and writes,
its version-1 stamp) still run against the same file. The stamp is
never bumped, so its Open cannot refuse the database.
================
*/
func TestBlockColumnsToleratedByV1Statements(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "accounts.db")
	accounts, err := OpenAccountsWith(path, testAccountsOptions)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Create(ctx, "mallory", "password1"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetBlock(ctx, "mallory", "chargeback review", 0); err != nil {
		t.Fatal(err)
	}
	if err := accounts.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	if version != 1 {
		t.Fatalf("user_version = %d, want 1 (a newer stamp crash-loops a reverted Agent)", version)
	}
	// The v1 login read: a blocked account is invisible, like any disabled one.
	var hash []byte
	err = db.QueryRow(
		`SELECT password_hash FROM accounts WHERE id = ? AND disabled = 0`, "mallory",
	).Scan(&hash)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("v1 login read = %v, want no rows", err)
	}
	// The v1 write shapes: insert with the v1 column list, plain disable flip.
	plain, err := bcrypt.GenerateFromPassword([]byte("password2"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(
		`INSERT INTO accounts (id, password_hash, created_at_ms) VALUES (?, ?, ?)`,
		"newbie", plain, int64(1700000000000),
	); err != nil {
		t.Fatalf("v1 insert: %v", err)
	}
	if _, err := db.Exec(`UPDATE accounts SET disabled = 1 WHERE id = ?`, "newbie"); err != nil {
		t.Fatalf("v1 disable: %v", err)
	}
	if _, err := db.Exec(`UPDATE accounts SET disabled = 0 WHERE id = ?`, "mallory"); err != nil {
		t.Fatalf("v1 enable: %v", err)
	}
}
