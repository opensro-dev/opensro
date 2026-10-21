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
	"time"

	"golang.org/x/crypto/bcrypt"
)

// testAccountsOptions hashes at bcrypt's minimum cost: these tests prove
// the store's rules, not bcrypt's speed, and TestProductionAccountsHashAt
// AccountBcryptCost keeps one store at the production cost.
var testAccountsOptions = AccountsOptions{BcryptCost: bcrypt.MinCost}

// productionBcryptCost is the cost production hashes are pinned to.
const productionBcryptCost = 12

/*
================
TestProductionAccountsHashAtAccountBcryptCost

The production store (OpenAccounts) hashes at cost 12, inside the seed
catalog's range; a cost outside bcrypt's or the catalog's range is refused.
================
*/
func TestProductionAccountsHashAtAccountBcryptCost(t *testing.T) {
	if AccountBcryptCost != productionBcryptCost {
		t.Fatalf("AccountBcryptCost = %d, want %d", AccountBcryptCost, productionBcryptCost)
	}
	path := filepath.Join(t.TempDir(), "accounts.db")
	accounts, err := OpenAccounts(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	if _, err := accounts.Create(context.Background(), "prod", "password1"); err != nil {
		t.Fatal(err)
	}
	stored, _ := accounts.PasswordHash("prod")
	if cost, _ := bcrypt.Cost(stored); cost != AccountBcryptCost || accounts.BcryptCost() != AccountBcryptCost {
		t.Fatalf("production hash cost %d, store %d", cost, accounts.BcryptCost())
	}
	for _, cost := range []int{bcrypt.MinCost - 1, MaxBcryptCost + 1} {
		if _, err := OpenAccountsWith(filepath.Join(t.TempDir(), "x.db"), AccountsOptions{BcryptCost: cost}); err == nil {
			t.Fatalf("cost %d accepted", cost)
		}
	}
}

/*
================
openTestAccounts
================
*/
func openTestAccounts(t *testing.T) (*Accounts, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "agent", "accounts.db")
	accounts, err := OpenAccountsWith(path, testAccountsOptions)
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
	reopened, err := OpenAccountsWith(path, testAccountsOptions)
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

/*
================
TestAccountsCredentialIgnoresCaseAndReturnsStoredID

The website stores "demiurgs"; a player typing "Demiurgs" in game must reach
the same account, and the session must carry the stored id that owns the
characters. A disabled account resolves to nothing.
================
*/
func TestAccountsCredentialIgnoresCaseAndReturnsStoredID(t *testing.T) {
	ctx := context.Background()
	accounts, _ := openTestAccounts(t)
	if _, err := accounts.Create(ctx, "demiurgs", "password1"); err != nil {
		t.Fatal(err)
	}
	for _, typed := range []string{"demiurgs", "Demiurgs", "DEMIURGS"} {
		id, hash, found := accounts.Credential(typed)
		if !found || id != "demiurgs" {
			t.Fatalf("Credential(%q) = %q, %v", typed, id, found)
		}
		if bcrypt.CompareHashAndPassword(hash, []byte("password1")) != nil {
			t.Fatalf("Credential(%q) returned another hash", typed)
		}
	}
	if _, _, found := accounts.Credential("demiurg"); found {
		t.Fatal("a different id resolved")
	}
	if err := accounts.SetDisabled(ctx, "demiurgs", true); err != nil {
		t.Fatal(err)
	}
	if _, _, found := accounts.Credential("Demiurgs"); found {
		t.Fatal("disabled account resolved")
	}
}

/*
================
TestAccountsUpgradePasswordHashIsACompareAndSwap

A login verified the cost-4 hash; the upgrade raises it to the store's
cost. A password reset that lands between the verify and the upgrade is
kept: the stale swap matches nothing and the new password still works.
================
*/
func TestAccountsUpgradePasswordHashIsACompareAndSwap(t *testing.T) {
	ctx := context.Background()
	accounts, _ := openTestAccounts(t)
	if _, err := accounts.Create(ctx, "erin", "password1"); err != nil {
		t.Fatal(err)
	}
	low, err := bcrypt.GenerateFromPassword([]byte("password1"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.db.ExecContext(ctx, `UPDATE accounts SET password_hash = ? WHERE id = ?`, low, "erin"); err != nil {
		t.Fatal(err)
	}
	if upgraded, err := accounts.UpgradePasswordHash(ctx, "erin", low, "password1"); err != nil || !upgraded {
		t.Fatalf("upgrade = %v, %v", upgraded, err)
	}
	stored, _ := accounts.PasswordHash("erin")
	if cost, _ := bcrypt.Cost(stored); cost != accounts.BcryptCost() {
		t.Fatalf("upgraded cost %d, want the store's %d", cost, accounts.BcryptCost())
	}
	if ok, _ := accounts.Verify(ctx, "erin", "password1"); !ok {
		t.Fatal("upgraded password refused")
	}

	// The race: a login verified stored, then a reset lands.
	if err := accounts.SetPassword(ctx, "erin", "password2"); err != nil {
		t.Fatal(err)
	}
	if upgraded, err := accounts.UpgradePasswordHash(ctx, "erin", stored, "password1"); err != nil || upgraded {
		t.Fatalf("stale upgrade = %v, %v", upgraded, err)
	}
	if ok, _ := accounts.Verify(ctx, "erin", "password1"); ok {
		t.Fatal("a stale upgrade restored the old password")
	}
	if ok, _ := accounts.Verify(ctx, "erin", "password2"); !ok {
		t.Fatal("the reset password was overwritten")
	}
}

/*
================
TestAccountsSessionEpochAndRevocationNotice
================
*/
func TestAccountsSessionEpochAndRevocationNotice(t *testing.T) {
	ctx := context.Background()
	accounts, path := openTestAccounts(t)
	if _, err := accounts.Create(ctx, "hero", "password1"); err != nil {
		t.Fatal(err)
	}
	if epoch, ok := accounts.SessionEpoch("hero"); !ok || epoch != 1 {
		t.Fatalf("initial SessionEpoch = (%d, %v), want (1, true)", epoch, ok)
	}

	var observed []uint64
	accounts.SetRevokeObserver(func(accountID string, epoch uint64) {
		if accountID == "hero" {
			observed = append(observed, epoch)
		}
	})

	if err := accounts.SetPassword(ctx, "hero", "password2"); err != nil {
		t.Fatal(err)
	}
	if epoch, ok := accounts.SessionEpoch("hero"); !ok || epoch != 2 {
		t.Fatalf("after SetPassword SessionEpoch = (%d, %v), want (2, true)", epoch, ok)
	}

	if err := accounts.SetDisabled(ctx, "hero", true); err != nil {
		t.Fatal(err)
	}
	if _, ok := accounts.SessionEpoch("hero"); ok {
		t.Fatal("disabled account reported active SessionEpoch")
	}
	if err := accounts.SetDisabled(ctx, "hero", false); err != nil {
		t.Fatal(err)
	}
	if epoch, ok := accounts.SessionEpoch("hero"); !ok || epoch != 4 {
		t.Fatalf("after re-enable SessionEpoch = (%d, %v), want (4, true)", epoch, ok)
	}

	revokedEpoch, err := accounts.RevokeSessions(ctx, "hero")
	if err != nil || revokedEpoch != 5 {
		t.Fatalf("RevokeSessions = (%d, %v), want (5, nil)", revokedEpoch, err)
	}
	if len(observed) != 4 || observed[0] != 2 || observed[1] != 3 || observed[2] != 4 || observed[3] != 5 {
		t.Fatalf("observed epochs = %v, want [2 3 4 5]", observed)
	}

	if err := accounts.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenAccountsWith(path, testAccountsOptions)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if epoch, ok := reopened.SessionEpoch("hero"); !ok || epoch != 5 {
		t.Fatalf("reopened SessionEpoch = (%d, %v), want (5, true)", epoch, ok)
	}

	now := time.Unix(1_785_000_000, 0)
	privatePayload, err := GenerateAgentSessionKeyRing(now)
	if err != nil {
		t.Fatal(err)
	}
	publicPayload, err := PublicAgentSessionKeyRing(privatePayload)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privPath := filepath.Join(dir, "priv.json")
	pubPath := filepath.Join(dir, "pub.json")
	if err := os.WriteFile(privPath, privatePayload, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pubPath, publicPayload, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := NewAgentSessionSigner(privPath)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewAgentSessionVerifier(pubPath)
	if err != nil {
		t.Fatal(err)
	}
	sessionToken, err := signer.MintWithEpoch("hero", "global-official", now.Add(time.Hour), 5)
	if err != nil {
		t.Fatal(err)
	}
	sessionClaims, err := verifier.Verify(sessionToken, now)
	if err != nil || sessionClaims.Epoch != 5 || sessionClaims.Nonce == "" {
		t.Fatalf("VerifyWithEpoch = (%+v, %v)", sessionClaims, err)
	}
	revocationToken, err := signer.MintAccountRevocation("global-official", "hero", 5, now)
	if err != nil {
		t.Fatal(err)
	}
	revClaims, err := verifier.VerifyAccountRevocation(revocationToken, now)
	if err != nil || revClaims.AccountID != "hero" || revClaims.ShardID != "global-official" || revClaims.Epoch != 5 {
		t.Fatalf("VerifyAccountRevocation = (%+v, %v)", revClaims, err)
	}
	if _, err := verifier.VerifyAccountRevocation(revocationToken, now.Add(2*AccountRevocationLifetime)); err == nil {
		t.Fatal("expired account revocation verified")
	}
}
