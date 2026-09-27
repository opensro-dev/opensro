/*
===========================================================================

accounts_test.go - the live account authority's contract

===========================================================================
*/
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

/*
================
openTestAccounts
================
*/
func openTestAccounts(t *testing.T) (*Accounts, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent", "accounts.db")
	accounts, err := OpenAccounts(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	return accounts, path
}

/*
================
writeSeedCatalog
================
*/
func writeSeedCatalog(t *testing.T, rows map[string]string) *Catalog {
	t.Helper()
	var document []map[string]string
	for id, password := range rows {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			t.Fatal(err)
		}
		document = append(document, map[string]string{"id": id, "passwordHash": string(hash)})
	}
	payload, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "accounts.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(path)
	if err != nil {
		t.Fatalf("load seed: %v", err)
	}
	return catalog
}

func TestAccountsCreateVerifyAndSurviveReopen(t *testing.T) {
	ctx := context.Background()
	accounts, path := openTestAccounts(t)
	info, err := accounts.Create(ctx, "hunter", "correct horse")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if info.ID != "hunter" || info.Disabled || info.CreatedAt.IsZero() {
		t.Fatalf("created info = %+v", info)
	}
	if ok, err := accounts.Verify(ctx, "hunter", "correct horse"); err != nil || !ok {
		t.Fatalf("verify right password = %v, %v", ok, err)
	}
	if ok, _ := accounts.Verify(ctx, "hunter", "wrong horse!"); ok {
		t.Fatal("wrong password verified")
	}
	if ok, _ := accounts.Verify(ctx, "nobody", "correct horse"); ok {
		t.Fatal("missing account verified")
	}
	if err := accounts.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenAccounts(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer reopened.Close()
	if ok, _ := reopened.Verify(ctx, "hunter", "correct horse"); !ok {
		t.Fatal("account lost across reopen")
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0o600 && os.PathSeparator == '/' {
		t.Fatalf("database mode = %v, %v", info.Mode(), err)
	}
}

func TestAccountsRefuseCaseVariantsReservedAndBadPasswords(t *testing.T) {
	ctx := context.Background()
	accounts, _ := openTestAccounts(t)
	if _, err := accounts.Create(ctx, "Bob", "password1"); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"Bob", "bob", "BOB"} {
		if _, err := accounts.Create(ctx, id, "password1"); !errors.Is(err, ErrAccountExists) {
			t.Fatalf("create %q = %v, want ErrAccountExists", id, err)
		}
	}
	for _, id := range []string{"", " padded", "__open_dev__", "tab\tid"} {
		if _, err := accounts.Create(ctx, id, "password1"); !errors.Is(err, ErrAccountIDInvalid) {
			t.Fatalf("create %q = %v, want ErrAccountIDInvalid", id, err)
		}
	}
	long := string(make([]byte, MaxPasswordBytes+1))
	for _, password := range []string{"short", long} {
		if _, err := accounts.Create(ctx, "carol", password); !errors.Is(err, ErrPasswordInvalid) {
			t.Fatalf("password of %d bytes = %v, want ErrPasswordInvalid", len(password), err)
		}
	}
	// Lookups are exact: the id that owns characters is the stored spelling.
	if _, found := accounts.PasswordHash("bob"); found {
		t.Fatal("lookup folded case")
	}
}

func TestAccountsDisableBlocksLoginButKeepsOwnership(t *testing.T) {
	ctx := context.Background()
	accounts, _ := openTestAccounts(t)
	if _, err := accounts.Create(ctx, "dave", "password1"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetDisabled(ctx, "dave", true); err != nil {
		t.Fatal(err)
	}
	if _, found := accounts.PasswordHash("dave"); found {
		t.Fatal("disabled account still has a login hash")
	}
	if ok, _ := accounts.Verify(ctx, "dave", "password1"); ok {
		t.Fatal("disabled account verified")
	}
	if ids := accounts.IDs(); len(ids) != 1 || ids[0] != "dave" {
		t.Fatalf("ids = %v, disabled owner must remain", ids)
	}
	if info, err := accounts.Lookup("dave"); err != nil || !info.Disabled {
		t.Fatalf("lookup = %+v, %v", info, err)
	}
	if err := accounts.SetDisabled(ctx, "dave", false); err != nil {
		t.Fatal(err)
	}
	if ok, _ := accounts.Verify(ctx, "dave", "password1"); !ok {
		t.Fatal("re-enabled account refused")
	}
	if err := accounts.SetDisabled(ctx, "ghost", true); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("disable missing = %v", err)
	}
}

func TestAccountsSetPassword(t *testing.T) {
	ctx := context.Background()
	accounts, _ := openTestAccounts(t)
	if _, err := accounts.Create(ctx, "erin", "password1"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetPassword(ctx, "erin", "password2"); err != nil {
		t.Fatal(err)
	}
	if ok, _ := accounts.Verify(ctx, "erin", "password1"); ok {
		t.Fatal("old password still works")
	}
	if ok, _ := accounts.Verify(ctx, "erin", "password2"); !ok {
		t.Fatal("new password refused")
	}
	if err := accounts.SetPassword(ctx, "ghost", "password2"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("set missing = %v", err)
	}
}

func TestAccountsSeedNeverOverwrites(t *testing.T) {
	ctx := context.Background()
	accounts, _ := openTestAccounts(t)
	if _, err := accounts.Create(ctx, "Frank", "changed-later"); err != nil {
		t.Fatal(err)
	}
	catalog := writeSeedCatalog(t, map[string]string{
		"opensro": "seed-password",
		"frank":   "seed-password",
	})
	inserted, err := accounts.Seed(catalog)
	if err != nil {
		t.Fatal(err)
	}
	if inserted != 1 {
		t.Fatalf("inserted = %d, want 1 (frank collides with Frank)", inserted)
	}
	if ok, _ := accounts.Verify(ctx, "opensro", "seed-password"); !ok {
		t.Fatal("seeded account refused")
	}
	if ok, _ := accounts.Verify(ctx, "Frank", "changed-later"); !ok {
		t.Fatal("seed overwrote an existing account")
	}
	// A second deploy with the same catalog changes nothing.
	if err := accounts.SetPassword(ctx, "opensro", "rotated-password"); err != nil {
		t.Fatal(err)
	}
	if inserted, err := accounts.Seed(catalog); err != nil || inserted != 0 {
		t.Fatalf("reseed = %d, %v", inserted, err)
	}
	if ok, _ := accounts.Verify(ctx, "opensro", "rotated-password"); !ok {
		t.Fatal("reseed reverted a changed password")
	}
}

func TestOpenAccountsRefusesSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.db")
	if err := os.WriteFile(target, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "accounts.db")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := OpenAccounts(link); err == nil {
		t.Fatal("opened a symlinked account database")
	}
}
