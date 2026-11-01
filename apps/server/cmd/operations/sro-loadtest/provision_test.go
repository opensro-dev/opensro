/*
===========================================================================

provision_test.go - provisioning against the two fakes

The provisioning run must be idempotent (a re-run keeps what it made),
must refuse an account that exists under another password, and must
surface a character-create refusal before any step starts.
===========================================================================
*/

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeProvisioning is the Agent's provisioning API: bearer-gated
// account create/verify with the real status vocabulary.
type fakeProvisioning struct {
	token    string
	accounts map[string]string
}

/*
================
handler
================
*/
func (f *fakeProvisioning) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/accounts", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ID       string `json:"id"`
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, exists := f.accounts[body.ID]; exists {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "ACCOUNT_EXISTS", "message": "exists"})
			return
		}
		f.accounts[body.ID] = body.Password
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": body.ID})
	})
	mux.HandleFunc("POST /v1/accounts/{id}/verify", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Password string `json:"password"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]bool{"valid": f.accounts[r.PathValue("id")] == body.Password})
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+f.token {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{"code": "UNAUTHORIZED", "message": "no"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

// provisionFixture wires the fakes and a provision client.
type provisionFixture struct {
	cfg      runConfig
	prov     *provisionClient
	provFake *fakeProvisioning
	agent    *fakeAgent
	accounts []botAccount
}

/*
================
newProvisionFixture
================
*/
func newProvisionFixture(t *testing.T, n int) provisionFixture {
	t.Helper()
	agent := newFakeAgent()
	agentServer := httptest.NewServer(agent.handler())
	t.Cleanup(agentServer.Close)

	provFake := &fakeProvisioning{token: "test-provisioning-token-0123456789", accounts: map[string]string{}}
	provServer := httptest.NewServer(provFake.handler())
	t.Cleanup(provServer.Close)

	tokenFile := filepath.Join(t.TempDir(), "agent-provisioning-token")
	if err := os.WriteFile(tokenFile, []byte(provFake.token), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}
	prov, err := newProvisionClient(provServer.URL, tokenFile)
	if err != nil {
		t.Fatalf("newProvisionClient: %v", err)
	}
	accounts := make([]botAccount, 0, n)
	for i := 0; i < n; i++ {
		acct := botAccount{
			ID:        fmt.Sprintf("load%04d", i+1),
			Password:  fmt.Sprintf("pw-%04d-secret", i+1),
			Character: fmt.Sprintf("Load%04d", i+1),
		}
		agent.passwords[acct.ID] = acct.Password
		accounts = append(accounts, acct)
	}
	return provisionFixture{
		cfg:      runConfig{agentURL: agentServer.URL, serverID: "global-official", division: "global-official", timeout: 5 * time.Second},
		prov:     prov,
		provFake: provFake,
		agent:    agent,
		accounts: accounts,
	}
}

/*
================
TestProvisionRunCreatesThenKeeps

First run: accounts created, characters created, the run's character
resolved. Second run: everything kept, nothing duplicated.
================
*/
func TestProvisionRunCreatesThenKeeps(t *testing.T) {
	fx := newProvisionFixture(t, 3)
	if err := provisionRun(fx.cfg, fx.prov, fx.accounts); err != nil {
		t.Fatalf("provisionRun: %v", err)
	}
	for i, acct := range fx.accounts {
		if want := fmt.Sprintf("Load%04d", i+1); acct.Character != want {
			t.Fatalf("accounts[%d].Character = %q, want its created character %q", i, acct.Character, want)
		}
		fx.agent.mu.Lock()
		roster := append([]string(nil), fx.agent.rosters[acct.ID]...)
		fx.agent.mu.Unlock()
		if len(roster) != 1 || roster[0] != acct.Character {
			t.Fatalf("roster[%s] = %v, want exactly its character", acct.ID, roster)
		}
	}
	if err := provisionRun(fx.cfg, fx.prov, fx.accounts); err != nil {
		t.Fatalf("second provisionRun: %v", err)
	}
	for _, acct := range fx.accounts {
		fx.agent.mu.Lock()
		roster := append([]string(nil), fx.agent.rosters[acct.ID]...)
		fx.agent.mu.Unlock()
		if len(roster) != 1 {
			t.Fatalf("roster[%s] = %v after re-run, want no duplication", acct.ID, roster)
		}
	}
}

/*
================
TestProvisionRunRefusesForeignAccount

An account that exists under a different password stops the run: the
bots must never sign into a fixture the run does not own.
================
*/
func TestProvisionRunRefusesForeignAccount(t *testing.T) {
	fx := newProvisionFixture(t, 1)
	// The id already exists under another owner's password.
	fx.provFake.accounts[fx.accounts[0].ID] = "the-other-owners-password"
	err := provisionRun(fx.cfg, fx.prov, fx.accounts)
	if err == nil || !strings.Contains(err.Error(), "different password") {
		t.Fatalf("foreign account error = %v, want the different-password refusal", err)
	}
}

/*
================
TestProvisionRunCreateRefusal

A character-create refusal aborts provisioning with the native code
named, before any step could enter a half-made roster.
================
*/
func TestProvisionRunCreateRefusal(t *testing.T) {
	fx := newProvisionFixture(t, 1)
	fx.agent.createRefusals["Load0001"] = 7
	err := provisionRun(fx.cfg, fx.prov, fx.accounts)
	if err == nil || !strings.Contains(err.Error(), "native error code 7") {
		t.Fatalf("create refusal error = %v, want native error code 7 named", err)
	}
}
