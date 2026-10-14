/*
===========================================================================

session_revocation_test.go - a session is only as good as its account

A signed Agent session outlives the account it was minted for; every
bearer route re-reads the account, so a disabled or removed one is refused
at once. The login's bcrypt cost is the same for a known and an unknown
account, and a hash made at a lower cost is raised on the next login.

===========================================================================
*/
package agentserver

import (
	"context"
	"net/http"
	"testing"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/security/auth"
)

/*
================
TestBearerRoutesRefuseADisabledAccountsSession

Before the account goes, its token lists characters; after, the list, the
enter-world and transport mints and the area routes all answer 401 and no
worker hears them.
================
*/
func TestBearerRoutesRefuseADisabledAccountsSession(t *testing.T) {
	calls := 0
	shardWorker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{}`))
	})
	accounts := &revocableAccounts{}
	fixture := newAgentFixture(t, shardWorker, shardWorker, func(config *Config) {
		accounts.AccountAuthority = config.Accounts
		config.Accounts = accounts
	})
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	login := decodeObject(t, performJSON(t, fixture.handler, http.MethodPost, "/title/login",
		`{"id":"tester","password":"123123","serverId":"alpha"}`, ""))
	token, _ := login["sessionToken"].(string)
	if token == "" {
		t.Fatalf("login = %+v", login)
	}
	if code := performJSON(t, fixture.handler, http.MethodPost, "/character/list", `{}`, token).Code; code != http.StatusOK || calls != 1 {
		t.Fatalf("live account list = %d, worker calls %d", code, calls)
	}
	accounts.revoked = "tester"
	for _, path := range []string{
		"/character/list",
		"/auth/enterworld-token",
		"/auth/transport-token",
		"/character/enter-area",
		"/character/leave-area",
	} {
		if code := performJSON(t, fixture.handler, http.MethodPost, path, `{}`, token).Code; code != http.StatusUnauthorized {
			t.Errorf("%s after disable = %d", path, code)
		}
	}
	if calls != 1 {
		t.Fatalf("a disabled account's requests reached the worker %d times", calls-1)
	}
}

/*
================
TestUnknownAccountRefusalCostsWhatALoginDoes
================
*/
func TestUnknownAccountRefusalCostsWhatALoginDoes(t *testing.T) {
	var captured Config
	newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler(), func(config *Config) { captured = *config })
	server, err := New(captured)
	if err != nil {
		t.Fatal(err)
	}
	if cost, err := bcrypt.Cost(server.dummyHash); err != nil || cost != auth.AccountBcryptCost {
		t.Fatalf("unknown-account hash cost %d (%v), want %d", cost, err, auth.AccountBcryptCost)
	}
}

/*
================
rehashingAccounts

The test accounts with a password writer that records each call and can
serve a hash of its own.
================
*/
type rehashingAccounts struct {
	AccountAuthority
	hash      []byte
	rehashed  []string
	passwords []string
}

/*
================
rehashingAccounts.Credential
================
*/
func (accounts *rehashingAccounts) Credential(typedID string) (string, []byte, bool) {
	id, hash, found := accounts.AccountAuthority.Credential(typedID)
	if found && accounts.hash != nil {
		hash = accounts.hash
	}
	return id, hash, found
}

/*
================
rehashingAccounts.SetPassword
================
*/
func (accounts *rehashingAccounts) SetPassword(_ context.Context, accountID, password string) error {
	accounts.rehashed = append(accounts.rehashed, accountID)
	accounts.passwords = append(accounts.passwords, password)
	return nil
}

/*
================
TestLoginRaisesALowCostHash

The seed hash is bcrypt's default cost: a correct login rewrites it at the
store's cost; a wrong password and an already-current hash do not.
================
*/
func TestLoginRaisesALowCostHash(t *testing.T) {
	accounts := &rehashingAccounts{}
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler(), func(config *Config) {
		accounts.AccountAuthority = config.Accounts
		config.Accounts = accounts
	})
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	login := func(password string) {
		performJSON(t, fixture.handler, http.MethodPost, "/title/login",
			`{"id":"tester","password":"`+password+`","serverId":"alpha"}`, "")
	}
	login("wrong-password")
	if len(accounts.rehashed) != 0 {
		t.Fatalf("a refused login rehashed %v", accounts.rehashed)
	}
	login("123123")
	if len(accounts.rehashed) != 1 || accounts.rehashed[0] != "tester" || accounts.passwords[0] != "123123" {
		t.Fatalf("low-cost login rehashed %v", accounts.rehashed)
	}
	current, err := bcrypt.GenerateFromPassword([]byte("123123"), auth.AccountBcryptCost)
	if err != nil {
		t.Fatal(err)
	}
	accounts.hash = current
	login("123123")
	if len(accounts.rehashed) != 1 {
		t.Fatalf("a current-cost hash was rehashed again: %v", accounts.rehashed)
	}
}
