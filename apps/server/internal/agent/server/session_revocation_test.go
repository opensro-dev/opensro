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
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/security/auth"
)

/*
================
performBrowserJSON
================
*/
func performBrowserJSON(
	t *testing.T,
	handler http.Handler,
	path string,
	body string,
	cookies []*http.Cookie,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	declareBrowser(request)
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

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
	verified  [][]byte
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
rehashingAccounts.UpgradePasswordHash
================
*/
func (accounts *rehashingAccounts) UpgradePasswordHash(_ context.Context, accountID string, verified []byte, password string) (bool, error) {
	accounts.rehashed = append(accounts.rehashed, accountID)
	accounts.passwords = append(accounts.passwords, password)
	accounts.verified = append(accounts.verified, verified)
	return true, nil
}

/*
================
TestLoginRaisesALowCostHash

The seed hash is bcrypt's default cost: a correct login swaps it, from the
hash it verified, for one at the store's cost; a wrong password and an
already-current hash do not.
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
	// The swap is conditioned on the hash the login verified.
	if _, seed, _ := accounts.AccountAuthority.Credential("tester"); string(accounts.verified[0]) != string(seed) {
		t.Fatal("the upgrade was not conditioned on the verified hash")
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

/*
================
TestLogoutRevokesSessionTokenAndNotifiesShard
================
*/
func TestLogoutRevokesSessionTokenAndNotifiesShard(t *testing.T) {
	var (
		listCalls   int
		revocations []string
	)
	shardWorker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == agentapi.AccountRevokePath {
			body, _ := io.ReadAll(r.Body)
			revocations = append(revocations, strings.TrimSpace(string(body)))
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		listCalls++
		_, _ = w.Write([]byte(`{}`))
	})
	fixture := newAgentFixture(t, shardWorker, http.NotFoundHandler())
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)

	loginRec := performBrowserJSON(t, fixture.handler, "/title/login", `{"id":"tester","password":"123123","serverId":"alpha"}`, nil)
	login := decodeObject(t, loginRec)
	token, _ := login["sessionToken"].(string)
	if token == "" {
		t.Fatalf("login = %+v", login)
	}
	cookies := loginRec.Result().Cookies()

	if code := performJSON(t, fixture.handler, http.MethodPost, "/character/list", `{}`, token).Code; code != http.StatusOK || listCalls != 1 {
		t.Fatalf("before logout list = %d, calls = %d", code, listCalls)
	}

	logoutRec := performBrowserJSON(t, fixture.handler, "/title/logout", `{}`, cookies)
	if logoutRec.Code != http.StatusOK {
		t.Fatalf("logout = %d: %s", logoutRec.Code, logoutRec.Body.String())
	}
	if len(revocations) == 0 {
		t.Fatal("logout did not push account revocation to the shard")
	}

	// Both the old cookie and the old bearer token must now be refused.
	if code := performBrowserJSON(t, fixture.handler, "/title/session", `{}`, cookies).Code; code != http.StatusUnauthorized {
		t.Fatalf("/title/session with logged-out cookie = %d, want 401", code)
	}
	for _, path := range []string{"/character/list", "/auth/enterworld-token", "/auth/transport-token"} {
		if code := performJSON(t, fixture.handler, http.MethodPost, path, `{}`, token).Code; code != http.StatusUnauthorized {
			t.Fatalf("%s with logged-out bearer token = %d, want 401", path, code)
		}
	}
	if listCalls != 1 {
		t.Fatalf("logged-out token reached worker %d extra times", listCalls-1)
	}

	// A fresh login issues a new epoch token that succeeds again.
	relogin := decodeObject(t, performJSON(t, fixture.handler, http.MethodPost, "/title/login",
		`{"id":"tester","password":"123123","serverId":"alpha"}`, ""))
	newToken, _ := relogin["sessionToken"].(string)
	if newToken == "" {
		t.Fatalf("relogin = %+v", relogin)
	}
	if code := performJSON(t, fixture.handler, http.MethodPost, "/character/list", `{}`, newToken).Code; code != http.StatusOK {
		t.Fatalf("after relogin list = %d, want 200", code)
	}
}

/*
================
TestPasswordChangeRevokesSessionAndNotifiesShard
================
*/
func TestPasswordChangeRevokesSessionAndNotifiesShard(t *testing.T) {
	ctx := context.Background()
	accounts, err := auth.OpenAccounts(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	if _, err := accounts.Create(ctx, "tester", "password1"); err != nil {
		t.Fatal(err)
	}

	var revocations []string
	shardWorker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == agentapi.AccountRevokePath {
			body, _ := io.ReadAll(r.Body)
			revocations = append(revocations, strings.TrimSpace(string(body)))
			_, _ = w.Write([]byte(`{"ok":true}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	})
	fixture := newAgentFixture(t, shardWorker, http.NotFoundHandler(), func(config *Config) {
		config.Accounts = accounts
	})
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)

	loginRec := performBrowserJSON(t, fixture.handler, "/title/login", `{"id":"tester","password":"password1","serverId":"alpha"}`, nil)
	login := decodeObject(t, loginRec)
	oldToken, _ := login["sessionToken"].(string)
	if oldToken == "" {
		t.Fatalf("login = %+v", login)
	}
	cookies := loginRec.Result().Cookies()

	if err := accounts.SetPassword(ctx, "tester", "password2"); err != nil {
		t.Fatal(err)
	}
	if len(revocations) == 0 {
		t.Fatal("SetPassword did not push account revocation to shard")
	}
	if code := performBrowserJSON(t, fixture.handler, "/title/session", `{}`, cookies).Code; code != http.StatusUnauthorized {
		t.Fatalf("/title/session after SetPassword = %d, want 401", code)
	}
	if code := performJSON(t, fixture.handler, http.MethodPost, "/character/list", `{}`, oldToken).Code; code != http.StatusUnauthorized {
		t.Fatalf("/character/list after SetPassword = %d, want 401", code)
	}

	relogin := decodeObject(t, performJSON(t, fixture.handler, http.MethodPost, "/title/login",
		`{"id":"tester","password":"password2","serverId":"alpha"}`, ""))
	newToken, _ := relogin["sessionToken"].(string)
	if newToken == "" {
		t.Fatalf("relogin with new password = %+v", relogin)
	}
	if code := performJSON(t, fixture.handler, http.MethodPost, "/character/list", `{}`, newToken).Code; code != http.StatusOK {
		t.Fatalf("/character/list with new password token = %d, want 200", code)
	}
}
