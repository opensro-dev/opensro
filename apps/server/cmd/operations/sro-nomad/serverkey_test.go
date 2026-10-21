/*
===========================================================================

serverkey_test.go - the deployer refuses what the Agent would refuse

===========================================================================
*/
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/security/serverkey"
)

/*
================
TestConfiguredLicense
================
*/
func TestConfiguredLicense(t *testing.T) {
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	licensor := func() (ed25519.PublicKey, error) { return public, nil }
	now := time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC)
	const official = "https://opensro.online"

	stateDir := t.TempDir()
	if _, err := configuredLicense(stateDir, official, licensor, now); err == nil {
		t.Fatal("a public deployment without a key passed")
	}
	if settings, err := configuredLicense(stateDir, "http://127.0.0.1:5180", licensor, now); err != nil || settings.jobKeyPath() != "" {
		t.Fatalf("loopback deployment: %+v, %v", settings, err)
	}

	key, err := serverkey.Sign(serverkey.Grant{Operator: "official", Domains: []string{"opensro.online"}, Issued: now, Expires: now.Add(serverkey.DefaultLifetime)}, private)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(stateDir, "agent"), 0o700); err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(stateDir, "agent", serverKeyFile)
	if err := os.WriteFile(keyPath, key, 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err := configuredLicense(stateDir, official, licensor, now)
	if err != nil {
		t.Fatal(err)
	}
	if settings.jobKeyPath() != filepath.ToSlash(keyPath) {
		t.Fatalf("job key path %q", settings.jobKeyPath())
	}
	if _, err := configuredLicense(stateDir, "https://play.example", licensor, now); err == nil {
		t.Fatal("a key for another domain passed")
	}
	if _, err := configuredLicense(stateDir, official, licensor, now.Add(serverkey.DefaultLifetime)); err == nil {
		t.Fatal("an expired key passed")
	}

	sourceDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(sourceDir, publicSourceURLFile), []byte("https://git.example/fork\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	settings, err = configuredLicense(sourceDir, "https://play.example", licensor, now)
	if err != nil || settings.SourceURL != "https://git.example/fork" {
		t.Fatalf("published source: %+v, %v", settings, err)
	}
	deployment := &deployment{License: settings}
	if got := deployment.agentVariables()["public_source_url"]; got != "https://git.example/fork" {
		t.Fatalf("Agent public_source_url = %#v", got)
	}
}
