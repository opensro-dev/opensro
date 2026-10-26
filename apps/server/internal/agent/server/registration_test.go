/*
===========================================================================

registration_test.go - public signup, native opt-out and confirmation policy

Exercises the real SQLite account authority through the browser API so signup
must produce an immediately usable login without changing existing accounts.

===========================================================================
*/
package agentserver

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/security/auth"
)

/*
================
registrationFixture
================
*/
func registrationFixture(t *testing.T, options RegistrationOptions) (agentFixture, *auth.Accounts) {
	t.Helper()
	accounts, err := auth.OpenAccountsWith(filepath.Join(t.TempDir(), "accounts.db"), auth.AccountsOptions{BcryptCost: bcrypt.MinCost})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler(), func(config *Config) {
		config.Accounts = accounts
		config.Registration = options
	})
	return fixture, accounts
}

/*
================
TestRegistrationCreatesLoginWithoutConfirmation
================
*/
func TestRegistrationCreatesLoginWithoutConfirmation(t *testing.T) {
	fixture, accounts := registrationFixture(t, RegistrationOptions{Enabled: true})
	response := performJSON(t, fixture.handler, http.MethodPost, "/title/register", `{"username":" PlayerOne ","password":"password123"}`, "")
	if response.Code != http.StatusCreated {
		t.Fatalf("registration: %d %s", response.Code, response.Body.String())
	}
	var body struct {
		OK                        bool `json:"ok"`
		EmailConfirmationRequired bool `json:"emailConfirmationRequired"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || !body.OK || body.EmailConfirmationRequired {
		t.Fatalf("registration result: %s (%v)", response.Body.String(), err)
	}
	if response.Header().Get("Set-Cookie") != "" || response.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("signup must neither create a session nor be cached")
	}
	id, hash, found := accounts.Credential("PLAYERONE")
	if !found || id != "PlayerOne" || bcrypt.CompareHashAndPassword(hash, []byte("password123")) != nil {
		t.Fatal("created username must resolve to a hashed, usable login")
	}
	duplicate := performJSON(t, fixture.handler, http.MethodPost, "/title/register", `{"username":"PLAYERONE","password":"replacement123"}`, "")
	if duplicate.Code != http.StatusConflict {
		t.Fatalf("duplicate: %d %s", duplicate.Code, duplicate.Body.String())
	}
	verified, err := accounts.Verify(context.Background(), id, "password123")
	if err != nil || !verified || accounts.Len() != 1 {
		t.Fatalf("duplicate changed the original account: verified=%v count=%d error=%v", verified, accounts.Len(), err)
	}
}

/*
================
TestRegistrationValidatesInput
================
*/
func TestRegistrationValidatesInput(t *testing.T) {
	cases := []struct {
		name, body string
	}{
		{"missing username", `{"password":"password123"}`},
		{"control in username", `{"username":"bad\u0000name","password":"password123"}`},
		{"reserved username", `{"username":"__open_dev__","password":"password123"}`},
		{"reserved username case", `{"username":"__OPEN_DEV__","password":"password123"}`},
		{"short unicode password", `{"username":"PlayerOne","password":"\u754c\u754c\u754c"}`},
		{"short password", `{"username":"PlayerOne","password":"short"}`},
		{"long password", `{"username":"PlayerOne","password":"` + strings.Repeat("a", auth.MaxPasswordBytes+1) + `"}`},
		{"unicode password bytes", `{"username":"PlayerOne","password":"` + strings.Repeat("界", 25) + `"}`},
		{"long username", `{"username":"` + strings.Repeat("a", 65) + `","password":"password123"}`},
		{"unknown field", `{"username":"PlayerOne","password":"password123","disabled":false}`},
		{"trailing json", `{"username":"PlayerOne","password":"password123"} {}`},
		{"oversized body", strings.Repeat(" ", int(maxRequestBytes)+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture, accounts := registrationFixture(t, RegistrationOptions{Enabled: true})
			response := performJSON(t, fixture.handler, http.MethodPost, "/title/register", tc.body, "")
			if response.Code != http.StatusBadRequest || accounts.Len() != 0 {
				t.Fatalf("invalid input created account: %d %s count=%d", response.Code, response.Body.String(), accounts.Len())
			}
		})
	}
}

/*
================
TestRegistrationPolicy
================
*/
func TestRegistrationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options RegistrationOptions
		status  int
	}{
		{"native default", RegistrationOptions{}, http.StatusForbidden},
		{"future confirmation", RegistrationOptions{Enabled: true, RequireEmailConfirmation: true}, http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixture, accounts := registrationFixture(t, tc.options)
			response := performJSON(t, fixture.handler, http.MethodPost, "/title/register", `{"username":"PlayerOne","password":"password123"}`, "")
			if response.Code != tc.status || accounts.Len() != 0 {
				t.Fatalf("policy: %d %s count=%d", response.Code, response.Body.String(), accounts.Len())
			}
		})
	}
}

/*
================
TestRegistrationSharesLoginBudget
================
*/
func TestRegistrationSharesLoginBudget(t *testing.T) {
	fixture, accounts := registrationFixture(t, RegistrationOptions{Enabled: true})
	for range loginAttemptBurst {
		performJSON(t, fixture.handler, http.MethodPost, "/title/register", `{}`, "")
	}
	response := performJSON(t, fixture.handler, http.MethodPost, "/title/register", `{"username":"PlayerOne","password":"password123"}`, "")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" || accounts.Len() != 0 {
		t.Fatalf("budget: %d %s count=%d", response.Code, response.Body.String(), accounts.Len())
	}
	response = performJSON(t, fixture.handler, http.MethodGet, "/title/register", "", "")
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("method: %d %s", response.Code, response.Body.String())
	}
}
