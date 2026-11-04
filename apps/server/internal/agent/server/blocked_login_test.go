/*
===========================================================================

blocked_login_test.go - the blocked account's login refusal (#2271)

A blocked account that proves its password gets the native blocked
answer (status 3, argument 1) with the stored reason and end; every
other credential failure stays the byte-identical status 2, so the
block is never revealed to someone who cannot prove the password.

===========================================================================
*/

package agentserver

import (
	"context"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/security/auth"
)

/*
================
blockedLoginFixture

An Agent fixture whose account authority is the real store, so a
test can seed blocks with the same verbs the console uses.
================
*/
func blockedLoginFixture(t *testing.T) (agentFixture, *auth.Accounts) {
	t.Helper()
	accounts, err := auth.OpenAccountsWith(
		filepath.Join(t.TempDir(), "accounts.db"),
		auth.AccountsOptions{BcryptCost: bcrypt.MinCost},
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	fixture := newAgentFixture(
		t,
		http.NotFoundHandler(),
		http.NotFoundHandler(),
		func(config *Config) { config.Accounts = accounts },
	)
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	return fixture, accounts
}

/*
================
blockedLoginAnswer
================
*/
func blockedLoginAnswer(t *testing.T, fixture agentFixture, body string) map[string]any {
	t.Helper()
	response := performJSON(t, fixture.handler, http.MethodPost, "/title/login", body, "")
	if response.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", response.Code, response.Body.String())
	}
	return decodeObject(t, response)
}

/*
================
TestBlockedLoginAnswersReasonAndEnd
================
*/
func TestBlockedLoginAnswersReasonAndEnd(t *testing.T) {
	ctx := context.Background()
	fixture, accounts := blockedLoginFixture(t)
	if _, err := accounts.Create(ctx, "mallory", "password1"); err != nil {
		t.Fatal(err)
	}
	until := time.Date(2026, 8, 10, 3, 4, 5, 0, time.UTC).UnixMilli()
	if err := accounts.SetBlock(ctx, "mallory", "chargeback review", until); err != nil {
		t.Fatal(err)
	}

	answer := blockedLoginAnswer(t, fixture, `{"id":"mallory","password":"password1","serverId":"alpha"}`)
	if answer["ok"] != false || answer["nativeTitleStatus"] != float64(3) ||
		answer["nativeTitleArgument"] != float64(1) || answer["code"] != "ACCOUNT_BLOCKED" ||
		answer["blockReason"] != "chargeback review" || answer["blockEnd"] != "2026-08-10 03:04:05" {
		t.Fatalf("blocked login = %v", answer)
	}

	// A wrong password hides the block behind the plain credential
	// refusal, identical to an unknown id.
	wrong := blockedLoginAnswer(t, fixture, `{"id":"mallory","password":"password2","serverId":"alpha"}`)
	unknown := blockedLoginAnswer(t, fixture, `{"id":"nobody","password":"password2","serverId":"alpha"}`)
	if wrong["ok"] != false || wrong["nativeTitleStatus"] != float64(2) ||
		wrong["nativeTitleStatus"] != unknown["nativeTitleStatus"] ||
		wrong["nativeTitleArgument"] != unknown["nativeTitleArgument"] ||
		wrong["code"] != unknown["code"] || wrong["message"] != unknown["message"] {
		t.Fatalf("wrong-password blocked = %v, unknown = %v", wrong, unknown)
	}
	if _, revealed := wrong["blockReason"]; revealed {
		t.Fatalf("wrong-password answer reveals the block: %v", wrong)
	}
}

/*
================
TestExpiredBlockLoginClearsAndEnters
================
*/
func TestExpiredBlockLoginClearsAndEnters(t *testing.T) {
	ctx := context.Background()
	fixture, accounts := blockedLoginFixture(t)
	if _, err := accounts.Create(ctx, "mallory", "password1"); err != nil {
		t.Fatal(err)
	}
	// The fixture clock stands at 2026-07-30; this block ended weeks ago.
	until := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	if err := accounts.SetBlock(ctx, "mallory", "chargeback review", until); err != nil {
		t.Fatal(err)
	}

	answer := blockedLoginAnswer(t, fixture, `{"id":"mallory","password":"password1","serverId":"alpha"}`)
	if answer["ok"] != true || answer["sessionToken"] == "" || answer["sessionToken"] == nil {
		t.Fatalf("expired-block login = %v", answer)
	}
	if disabled, reason, untilMs, found := accounts.Block("mallory"); !found || disabled ||
		reason != "" || untilMs != 0 {
		t.Fatalf("block after expired login = %v %q %d %v", disabled, reason, untilMs, found)
	}
}

/*
================
TestPermanentAndPlainBlocksShowNoEnd
================
*/
func TestPermanentAndPlainBlocksShowNoEnd(t *testing.T) {
	ctx := context.Background()
	fixture, accounts := blockedLoginFixture(t)
	if _, err := accounts.Create(ctx, "mallory", "password1"); err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Create(ctx, "eve", "password1"); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetBlock(ctx, "mallory", "botting", 0); err != nil {
		t.Fatal(err)
	}
	if err := accounts.SetDisabled(ctx, "eve", true); err != nil {
		t.Fatal(err)
	}

	answer := blockedLoginAnswer(t, fixture, `{"id":"mallory","password":"password1","serverId":"alpha"}`)
	if answer["nativeTitleStatus"] != float64(3) || answer["nativeTitleArgument"] != float64(1) ||
		answer["blockReason"] != "botting" || answer["blockEnd"] != "" {
		t.Fatalf("permanent block login = %v", answer)
	}
	answer = blockedLoginAnswer(t, fixture, `{"id":"eve","password":"password1","serverId":"alpha"}`)
	if answer["nativeTitleStatus"] != float64(3) || answer["nativeTitleArgument"] != float64(1) ||
		answer["blockReason"] != "" || answer["blockEnd"] != "" {
		t.Fatalf("plain disable login = %v", answer)
	}
}
