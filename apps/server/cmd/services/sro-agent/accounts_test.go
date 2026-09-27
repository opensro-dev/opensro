/*
===========================================================================

accounts_test.go - Agent account authority and provisioning startup

===========================================================================
*/
package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

const testProvisioningToken = "0123456789abcdef0123456789abcdef"

/*
================
writeSeed
================
*/
func writeSeed(t *testing.T, dir, id, password string) string {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal([]map[string]string{{"id": id, "passwordHash": string(hash)}})
	path := filepath.Join(dir, "accounts.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestOpenAccountAuthoritySeedsAndRefusesEmpty(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envAccountsDBPath, filepath.Join(dir, "state", "accounts.db"))
	t.Setenv(envAccountsPath, "")
	if _, err := openAccountAuthority(); err == nil || !strings.Contains(err.Error(), "no accounts") {
		t.Fatalf("empty authority = %v, want refusal", err)
	}

	t.Setenv(envAccountsPath, writeSeed(t, dir, "opensro", "seed-password"))
	accounts, err := openAccountAuthority()
	if err != nil {
		t.Fatalf("seeded open: %v", err)
	}
	if ok, _ := accounts.Verify(t.Context(), "opensro", "seed-password"); !ok {
		t.Fatal("seeded account refused")
	}
	_ = accounts.Close()

	// A later start without the seed keeps the stored accounts.
	t.Setenv(envAccountsPath, "")
	accounts, err = openAccountAuthority()
	if err != nil {
		t.Fatalf("reopen without seed: %v", err)
	}
	defer accounts.Close()
	if accounts.Len() != 1 {
		t.Fatalf("len = %d", accounts.Len())
	}
}

func TestStartProvisioningServesOnlyWithTokenOnLoopback(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(envAccountsDBPath, filepath.Join(dir, "accounts.db"))
	t.Setenv(envAccountsPath, writeSeed(t, dir, "opensro", "seed-password"))
	accounts, err := openAccountAuthority()
	if err != nil {
		t.Fatal(err)
	}
	defer accounts.Close()
	errors := make(chan error, 1)

	t.Setenv(envProvisioningTokenPath, "")
	if server, _, err := startProvisioning(accounts, errors); err != nil || server != nil {
		t.Fatalf("no token = %v, %v; want disabled", server, err)
	}

	tokenPath := filepath.Join(dir, "provisioning-token")
	if err := os.WriteFile(tokenPath, []byte(testProvisioningToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envProvisioningTokenPath, tokenPath)
	t.Setenv(envProvisioningAddr, "0.0.0.0:0")
	if _, _, err := startProvisioning(accounts, errors); err == nil {
		t.Fatal("served on a non-loopback address")
	}

	t.Setenv(envProvisioningAddr, "127.0.0.1:0")
	server, api, err := startProvisioning(accounts, errors)
	if err != nil || server == nil {
		t.Fatalf("start = %v", err)
	}
	defer server.Close()
	body, _ := json.Marshal(map[string]string{"id": "hunter", "password": "correct horse"})
	request, _ := http.NewRequest("POST", "http://"+api+"/v1/accounts", bytes.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+testProvisioningToken)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("create through listener = %d", response.StatusCode)
	}
	if _, found := accounts.PasswordHash("hunter"); !found {
		t.Fatal("provisioned account is not visible to login")
	}
}
