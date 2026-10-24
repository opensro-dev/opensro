/*
===========================================================================

revocation_test.go - GameWorld account session and ticket revocation tests

Verifies that a signed account revocation notice from the Agent advances the
minimum accepted SAS2 session epoch on the shard, invalidates unconsumed
STA1 and SEA3 tickets minted before the revocation, and invokes the live
transport account evictor.

===========================================================================
*/
package agentapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/platform/readiness"
	"opensro.online/server/internal/security/auth"
)

/*
================
TestAccountRevocationInvalidatesSessionTicketsAndEvictsAccount
================
*/
func TestAccountRevocationInvalidatesSessionTicketsAndEvictsAccount(t *testing.T) {
	api, _ := newTestAPI(t)
	private, err := auth.GenerateAgentSessionKeyRing(api.now())
	if err != nil {
		t.Fatal(err)
	}
	public, err := auth.PublicAgentSessionKeyRing(private)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	privatePath := filepath.Join(dir, "private.json")
	publicPath := filepath.Join(dir, "public.json")
	if err := os.WriteFile(privatePath, private, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, public, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := auth.NewAgentSessionSigner(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	api.agentSessionVerifier, err = auth.NewAgentSessionVerifier(publicPath)
	if err != nil {
		t.Fatal(err)
	}

	var evicted []string
	api.InstallAccountRevoker(func(accountID string) {
		evicted = append(evicted, accountID)
	})

	handler := api.Handler()
	oldSession, err := signer.MintWithEpoch(testAccount, testDivision, api.now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}

	callAPI := func(sessionToken, method, path, body string) (int, []byte) {
		req := httptest.NewRequest(method, "http://127.0.0.1:8791"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		declareBrowser(req)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code, rec.Body.Bytes()
	}

	created := postJSON(t, bearerHandler(handler, oldSession), "/character/create", createBody("RevokeHero"))
	if created["nativeResult"] != float64(1) {
		t.Fatalf("create = %v", created)
	}

	mintTicket := func(sessionToken, path, body string) (int, string) {
		code, raw := callAPI(sessionToken, http.MethodPost, path, body)
		var resp struct {
			OK    bool   `json:"ok"`
			Token string `json:"token"`
		}
		_ = json.Unmarshal(raw, &resp)
		return code, resp.Token
	}

	code, staToken := mintTicket(oldSession, "/auth/transport-token", `{}`)
	if code != http.StatusOK || staToken == "" {
		t.Fatalf("transport-token = %d, %q", code, staToken)
	}
	code, seaToken := mintTicket(oldSession, "/auth/enterworld-token", `{"characterName":"RevokeHero"}`)
	if code != http.StatusOK || seaToken == "" {
		t.Fatalf("enterworld-token = %d, %q", code, seaToken)
	}

	revokeNotice, err := signer.MintAccountRevocation(testDivision, testAccount, 2, api.now())
	if err != nil {
		t.Fatal(err)
	}
	wrongShardNotice, err := signer.MintAccountRevocation("other-shard", testAccount, 2, api.now())
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name   string
		method string
		origin string
		body   string
		status int
	}{
		{"browser forbidden", http.MethodPost, "https://invalid.example", revokeNotice, http.StatusForbidden},
		{"method not allowed", http.MethodGet, "", revokeNotice, http.StatusMethodNotAllowed},
		{"wrong shard", http.MethodPost, "", wrongShardNotice, http.StatusUnauthorized},
		{"forged", http.MethodPost, "", "invalid-token", http.StatusUnauthorized},
		{"accepted", http.MethodPost, "", revokeNotice, http.StatusOK},
		{"idempotent retry", http.MethodPost, "", revokeNotice, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://127.0.0.1:8791"+AccountRevokePath, strings.NewReader(tc.body))
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body.String())
			}
		})
	}

	if len(evicted) != 1 || evicted[0] != testAccount {
		t.Fatalf("evicted = %v, want [%s]", evicted, testAccount)
	}

	// Prior epoch session token must now be refused on every route.
	if code, _ := callAPI(oldSession, http.MethodGet, "/character/list", ""); code != http.StatusUnauthorized {
		t.Fatalf("/character/list with old epoch session = %d, want 401", code)
	}
	if code, _ := mintTicket(oldSession, "/auth/transport-token", `{}`); code != http.StatusUnauthorized {
		t.Fatalf("/auth/transport-token with old epoch session = %d, want 401", code)
	}

	// Unconsumed STA1 and SEA3 tickets minted before the revocation must be refused.
	staVerify := api.WrapTransportAdmissionVerifier(auth.TransportAdmissionVerifier(api.enterWorldAuthSecret, api.now))
	if _, err := staVerify(staToken); err == nil {
		t.Fatal("revoked STA1 transport-admission ticket was accepted")
	}
	seaVerify := api.WrapEnterWorldVerifier(auth.Verifier(api.enterWorldAuthSecret, api.now))
	if err := seaVerify(seaToken, testDivision, "RevokeHero"); err == nil {
		t.Fatal("revoked SEA3 EnterWorld ticket was accepted")
	}

	// A session minted at the new epoch succeeds and mints fresh tickets.
	newSession, err := signer.MintWithEpoch(testAccount, testDivision, api.now().Add(time.Hour), 2)
	if err != nil {
		t.Fatal(err)
	}
	code, newSTA := mintTicket(newSession, "/auth/transport-token", `{}`)
	if code != http.StatusOK || newSTA == "" {
		t.Fatalf("new transport-token = %d, %q", code, newSTA)
	}
	code, newSEA := mintTicket(newSession, "/auth/enterworld-token", `{"characterName":"RevokeHero"}`)
	if code != http.StatusOK || newSEA == "" {
		t.Fatalf("new enterworld-token = %d, %q", code, newSEA)
	}

	// A duplicate or delayed revocation notice at the same epoch (epoch = 2)
	// must be a no-op and must not revoke the newly minted epoch-2 tickets or
	// re-evict the relogged session.
	staleNotice, err := signer.MintAccountRevocation(testDivision, testAccount, 2, api.now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, AccountRevokePath, strings.NewReader(staleNotice))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("duplicate-epoch revoke status = %d: %s", rec.Code, rec.Body.String())
	}
	if len(evicted) != 1 {
		t.Fatalf("duplicate-epoch notice triggered extra eviction: %v", evicted)
	}
	if _, err := staVerify(newSTA); err != nil {
		t.Fatalf("fresh STA1 ticket after relogin rejected: %v", err)
	}
	if err := seaVerify(newSEA, testDivision, "RevokeHero"); err != nil {
		t.Fatalf("fresh SEA3 ticket after relogin rejected: %v", err)
	}
}

/*
================
TestAccountRevocationFloorSurvivesRestart
================
*/
func TestAccountRevocationFloorSurvivesRestart(t *testing.T) {
	api, authority := newTestAPI(t)
	private, err := auth.GenerateAgentSessionKeyRing(api.now())
	if err != nil {
		t.Fatal(err)
	}
	public, err := auth.PublicAgentSessionKeyRing(private)
	if err != nil {
		t.Fatal(err)
	}
	keyDir := t.TempDir()
	privatePath := filepath.Join(keyDir, "private.json")
	publicPath := filepath.Join(keyDir, "public.json")
	if err := os.WriteFile(privatePath, private, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, public, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := auth.NewAgentSessionSigner(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := auth.NewAgentSessionVerifier(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	api.agentSessionVerifier = verifier

	epoch1Session, err := signer.MintWithEpoch(testAccount, testDivision, api.now().Add(time.Hour), 1)
	if err != nil {
		t.Fatal(err)
	}
	legacySession, err := signer.MintWithEpoch(testAccount, testDivision, api.now().Add(time.Hour), 0)
	if err != nil {
		t.Fatal(err)
	}
	epoch2Session, err := signer.MintWithEpoch(testAccount, testDivision, api.now().Add(time.Hour), 2)
	if err != nil {
		t.Fatal(err)
	}

	notice, err := signer.MintAccountRevocation(testDivision, testAccount, 2, api.now())
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, AccountRevokePath, strings.NewReader(notice))
	rec := httptest.NewRecorder()
	api.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke status = %d: %s", rec.Code, rec.Body.String())
	}

	// Simulate a GameWorld restart by constructing a new API over the same
	// authority store directory without delivering any new revocation notice.
	ready := readiness.NewGate()
	ready.Open()
	restarted, err := New(Config{
		Store:                 authority,
		CharacterPresentation: testCharacterPresentation,
		CharacterCreationValid: func(character *domain.Character) bool {
			return enterworld.CharacterCreationValid(character, testCharacterRoster())
		},
		CharacterNameAllowed: testNameFilter(t).Allowed,
		ShardID:              testDivision,
		AgentSessionVerifier: verifier,
		EnterWorldAuthSecret: []byte(testEnterWorldSecret),
		Readiness:            ready,
		Now:                  api.now,
	})
	if err != nil {
		t.Fatalf("restarted New: %v", err)
	}
	restartedHandler := restarted.Handler()

	callRestarted := func(sessionToken string) int {
		r := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8791/character/list", nil)
		r.Header.Set("Authorization", "Bearer "+sessionToken)
		declareBrowser(r)
		w := httptest.NewRecorder()
		restartedHandler.ServeHTTP(w, r)
		return w.Code
	}

	if code := callRestarted(epoch1Session); code != http.StatusUnauthorized {
		t.Fatalf("epoch-1 session after GameWorld restart = %d, want 401", code)
	}
	if code := callRestarted(legacySession); code != http.StatusUnauthorized {
		t.Fatalf("epoch-0 session after GameWorld restart = %d, want 401", code)
	}
	if code := callRestarted(epoch2Session); code != http.StatusOK {
		t.Fatalf("epoch-2 session after GameWorld restart = %d, want 200", code)
	}

	// Corrupting the persisted revocation file must fail closed on startup.
	if err := os.WriteFile(filepath.Join(authority.Dir(), revocationFloorFileName), []byte(`{"version":1,"epochs":{"bad":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{
		Store:                 authority,
		CharacterPresentation: testCharacterPresentation,
		CharacterCreationValid: func(character *domain.Character) bool {
			return enterworld.CharacterCreationValid(character, testCharacterRoster())
		},
		CharacterNameAllowed: testNameFilter(t).Allowed,
		ShardID:              testDivision,
		AgentSessionVerifier: verifier,
		EnterWorldAuthSecret: []byte(testEnterWorldSecret),
		Readiness:            ready,
		Now:                  api.now,
	}); err == nil {
		t.Fatal("New succeeded over corrupted account-revocations.json")
	}
}
