/*
===========================================================================

login_case_test.go - account ids at login ignore ASCII case

The website provisions ids in lowercase while players type them as they
registered ("Demiurgs"). Ids are unique case-insensitively, so the login
resolves any spelling to the stored id, mints the session for that stored
id (it owns the characters), and shares one failure budget across case.

===========================================================================
*/
package agentserver

import (
	"crypto/ed25519"
	"net/http"
	"testing"
	"time"

	"opensro.online/server/internal/security/auth"
)

/*
================
TestLoginResolvesTypedIDCaseToStoredAccount
================
*/
func TestLoginResolvesTypedIDCaseToStoredAccount(t *testing.T) {
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	for _, typed := range []string{"tester", "Tester", "TESTER"} {
		login := performJSON(
			t,
			fixture.handler,
			http.MethodPost,
			"/title/login",
			`{"id":"`+typed+`","password":"123123","serverId":"alpha","divisionId":"alpha","channelId":"normal"}`,
			"",
		)
		body := decodeObject(t, login)
		token, _ := body["sessionToken"].(string)
		if body["ok"] != true || token == "" {
			t.Fatalf("login %q = %s", typed, login.Body.String())
		}
		claims, err := auth.VerifyAgentSession(
			map[string]ed25519.PublicKey{testSessionKeyID: testSessionPublicKey},
			token,
			time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC),
		)
		if err != nil {
			t.Fatal(err)
		}
		if claims.AccountID != "tester" {
			t.Fatalf("login %q minted a session for %q, want the stored id", typed, claims.AccountID)
		}
	}
}

/*
================
TestPasswordFailureBudgetIgnoresAccountCase
================
*/
func TestPasswordFailureBudgetIgnoresAccountCase(t *testing.T) {
	var failures passwordFailures
	now := time.Date(2026, 9, 9, 0, 0, 0, 0, time.UTC)
	for _, typed := range []string{"tester", "Tester", "TESTER", "tEsTeR", "TESTer"} {
		failures.update("127.0.0.1:1", typed, now, true, false)
	}
	got, _ := failures.update("127.0.0.1:1", "Tester", now, false, false)
	if got&0xffff != passwordFailureLimit {
		t.Fatalf("case variants opened separate budgets: %x", got)
	}
}
