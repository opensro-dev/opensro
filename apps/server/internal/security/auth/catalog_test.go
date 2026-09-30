/*
===========================================================================

catalog_test.go - the account catalog's loading and lookup contract

===========================================================================
*/
package auth

import (
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

/*
================
TestLoadStrictAccountCatalog
================
*/
func TestLoadStrictAccountCatalog(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "accounts.json")
	payload := `[{"id":"account-a","passwordHash":"` + string(hash) + `"}]`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	catalog, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := catalog.PasswordHash("account-a")
	if !ok || string(got) != string(hash) {
		t.Fatalf("hash = %q, %v", got, ok)
	}
	got[0] ^= 0xff
	again, _ := catalog.PasswordHash("account-a")
	if string(got) == string(again) {
		t.Fatal("caller mutated catalog hash")
	}
}

/*
================
TestCatalogCredentialFoldsCaseAndRejectsCaseDuplicates
================
*/
func TestCatalogCredentialFoldsCaseAndRejectsCaseDuplicates(t *testing.T) {
	catalog := writeSeedCatalog(t, map[string]string{"Bob": "password1"})
	for _, typed := range []string{"Bob", "bob", "BOB"} {
		id, hash, found := catalog.Credential(typed)
		if !found || id != "Bob" || bcrypt.CompareHashAndPassword(hash, []byte("password1")) != nil {
			t.Fatalf("Credential(%q) = %q, %v", typed, id, found)
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "accounts.json")
	payload := `[{"id":"Bob","passwordHash":"` + string(hash) + `"},{"id":"bob","passwordHash":"` + string(hash) + `"}]`
	if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("a catalog with ids differing only in case loaded")
	}
}
