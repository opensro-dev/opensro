/*
===========================================================================

wiring_census_test.go - the census installs only behind its flag

SRO_CONNECTION_JOURNAL off (the native posture) means no observer on
the hub, no journal on the application and no files in the shard
directory; on means all three (#2716, both settings tested).
===========================================================================
*/
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/data/store"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/platform/census"
	"opensro.online/server/internal/platform/connjournal"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/transport"
)

/*
================
installCensusForTest
================
*/
func installCensusForTest(t *testing.T) (*gameWorldApplication, *transport.Server, *history.Journal, string) {
	t.Helper()
	directory := t.TempDir()
	t.Setenv(store.EnvStateDir, directory)
	named, err := history.Open(filepath.Join(directory, "history.sqlite"), "gameworld", "global-official", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = named.Close() })
	application := &gameWorldApplication{}
	server := &transport.Server{Hub: &transport.Hub{}}
	return application, server, named, directory
}

/*
================
TestConnectionJournalOffInstallsNothing
================
*/
func TestConnectionJournalOffInstallsNothing(t *testing.T) {
	t.Setenv(connjournal.EnvConnectionJournal, "")
	application, server, named, directory := installCensusForTest(t)
	if err := installConnectionJournal(application, server, named, "global-official"); err != nil {
		t.Fatal(err)
	}
	if server.Hub.Census != nil || application.connJournal != nil {
		t.Fatal("the census installed with its flag off")
	}
	for _, name := range []string{"connections.sqlite", "connections.key"} {
		if _, err := os.Stat(filepath.Join(directory, name)); !os.IsNotExist(err) {
			t.Fatalf("%s exists with the census off", name)
		}
	}
}

/*
================
TestConnectionJournalOnInstallsTheObserver
================
*/
func TestConnectionJournalOnInstallsTheObserver(t *testing.T) {
	t.Setenv(connjournal.EnvConnectionJournal, "on")
	application, server, named, directory := installCensusForTest(t)
	if err := installConnectionJournal(application, server, named, "global-official"); err != nil {
		t.Fatal(err)
	}
	if server.Hub.Census == nil || application.connJournal == nil {
		t.Fatal("the census did not install with its flag on")
	}
	for _, name := range []string{"connections.sqlite", "connections.key"} {
		if _, err := os.Stat(filepath.Join(directory, name)); err != nil {
			t.Fatalf("%s missing with the census on: %v", name, err)
		}
	}
	// The observer names sessions exactly as history does, so the two
	// journals join on them.
	if got, want := server.Hub.Census.SessionID("7"), named.SessionID("7"); got != want {
		t.Fatalf("census session name = %q, history names it %q", got, want)
	}
	if err := application.connJournal.Close(); err != nil {
		t.Fatal(err)
	}
}

/*
================
censusTestAPI

A real control API over a throwaway store, so the census install is
tested through the mux it will serve on.
================
*/
func censusTestAPI(t *testing.T) *agentapi.API {
	t.Helper()
	authority, err := store.Open(t.TempDir(), store.Options{Now: time.Now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(authority.Close)
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	// The ring names a key by its public key's hash (security/auth).
	digest := sha256.Sum256(public)
	keysPath := filepath.Join(t.TempDir(), "agent-session-public-keys.json")
	keys := fmt.Sprintf("{\"keys\":[{\"id\":%q,\"publicKey\":%q,\"createdAt\":\"2026-07-30T00:00:00Z\"}]}\n",
		hex.EncodeToString(digest[:16]), base64.RawURLEncoding.EncodeToString(public))
	if err := os.WriteFile(keysPath, []byte(keys), 0o600); err != nil {
		t.Fatal(err)
	}
	verifier, err := auth.NewAgentSessionVerifier(keysPath)
	if err != nil {
		t.Fatal(err)
	}
	ready := readiness.NewGate()
	ready.Open()
	api, err := agentapi.New(agentapi.Config{
		Store: authority,
		CharacterPresentation: func(*domain.Character) agentapi.CharacterPresentation {
			return agentapi.CharacterPresentation{}
		},
		CharacterCreationValid: func(*domain.Character) bool { return true },
		CharacterNameAllowed:   func(string) bool { return true },
		ShardID:                "global-official",
		AgentSessionVerifier:   verifier,
		EnterWorldAuthSecret:   []byte("test-enterworld-secret-at-least-32-bytes"),
		Readiness:              ready,
	})
	if err != nil {
		t.Fatal(err)
	}
	return api
}

// censusOnlineCode reads the census online route through the mux.
func censusOnlineCode(t *testing.T, api *agentapi.API, token string) int {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1"+census.Path+"/online", nil)
	request.RemoteAddr = "127.0.0.1:50000"
	request.Header.Set("X-SRO-Local-Diagnostics", "1")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	api.Handler().ServeHTTP(recorder, request)
	return recorder.Code
}

/*
================
TestCensusReadsInstallOnlyWithTheJournal

The census routes (part 2, #2717) exist only while the journal
runs: flag off, the mux answers its own 404; flag on with an
operator token, the online read answers 200 through the real mux.
================
*/
func TestCensusReadsInstallOnlyWithTheJournal(t *testing.T) {
	const token = "census-wiring-token-census-wiring-token-12"

	t.Setenv(connjournal.EnvConnectionJournal, "")
	application, server, named, _ := installCensusForTest(t)
	application.controlAPI = censusTestAPI(t)
	if err := installConnectionJournal(application, server, named, "global-official"); err != nil {
		t.Fatal(err)
	}
	if err := installCensusReads(application, "global-official"); err != nil {
		t.Fatal(err)
	}
	if code := censusOnlineCode(t, application.controlAPI, token); code != http.StatusNotFound {
		t.Fatalf("census off: got %d, want 404", code)
	}
	if application.census != nil {
		t.Fatal("census off: application.census set, the public counts would ride an off census")
	}

	t.Setenv(connjournal.EnvConnectionJournal, "on")
	application, server, named, directory := installCensusForTest(t)
	if err := os.WriteFile(filepath.Join(directory, "operator-token"), []byte(token), 0o600); err != nil {
		t.Fatal(err)
	}
	application.controlAPI = censusTestAPI(t)
	if err := installConnectionJournal(application, server, named, "global-official"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = application.connJournal.Close() }()
	if err := installCensusReads(application, "global-official"); err != nil {
		t.Fatal(err)
	}
	if application.census == nil {
		t.Fatal("census on: application.census nil, the public status answer would lose its people counts")
	}
	if code := censusOnlineCode(t, application.controlAPI, ""); code != http.StatusForbidden {
		t.Fatalf("census on without credentials: got %d, want 403", code)
	}
	if code := censusOnlineCode(t, application.controlAPI, token); code != http.StatusOK {
		t.Fatalf("census on: got %d, want 200", code)
	}
}
