/*
===========================================================================

shard_relay_test.go - the Agent relays a GameWorld's Retry-After

A paced ticket mint (#3303) answers 429 RATE_LIMITED with Retry-After on
the GameWorld's control port; the Agent forwards the mint, so the browser
sees the delay only when the relay passes the header on.

===========================================================================
*/
package agentserver

import (
	"net/http"
	"testing"
)

/*
================
TestShardRelayPassesRetryAfter
================
*/
func TestShardRelayPassesRetryAfter(t *testing.T) {
	worker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Retry-After", "7")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"ok":false,"code":"RATE_LIMITED"}`))
	})
	gameworld := newAgentFixture(t, worker, worker)
	publishFixtureLease(t, gameworld, "alpha", "worker-alpha", 1, 0)
	login := performJSON(t, gameworld.handler, http.MethodPost, loginPath,
		`{"id":"tester","password":"123123","serverId":"alpha"}`, "")
	token, _ := decodeObject(t, login)["sessionToken"].(string)
	if token == "" {
		t.Fatalf("login = %s", login.Body.String())
	}

	answer := performJSON(t, gameworld.handler, http.MethodPost, "/auth/transport-token", `{}`, token)
	if answer.Code != http.StatusTooManyRequests {
		t.Fatalf("relayed mint = %d %s", answer.Code, answer.Body.String())
	}
	if got := answer.Header().Get("Retry-After"); got != "7" {
		t.Fatalf("Retry-After = %q, want 7", got)
	}
	if code, _ := decodeObject(t, answer)["code"].(string); code != "RATE_LIMITED" {
		t.Fatalf("code = %q", code)
	}
}

/*
================
TestRelayedRetryAfterKeepsOnlySeconds
================
*/
func TestRelayedRetryAfterKeepsOnlySeconds(t *testing.T) {
	for _, c := range []struct {
		value string
		want  string
		ok    bool
	}{
		{"10", "10", true},
		{"0", "0", true},
		{"", "", false},
		{"-1", "", false},
		{"Wed, 21 Oct 2015 07:28:00 GMT", "", false},
		{"5\r\nX-Injected: 1", "", false},
	} {
		got, ok := relayedRetryAfter(c.value)
		if got != c.want || ok != c.ok {
			t.Fatalf("relayedRetryAfter(%q) = %q, %v; want %q, %v", c.value, got, ok, c.want, c.ok)
		}
	}
}
