/*
===========================================================================

login_source_test.go - a login event's source fields (#4815)

The keyed address and network hashes a login carries, from the
connection journal's own hash: equal for one address, equal network
for one /24, nothing with the census off or an unparsable peer.

===========================================================================
*/

package agentserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"opensro.online/server/internal/platform/connjournal"
	"opensro.online/server/internal/platform/history"
	"opensro.online/server/internal/security/auth"
	"opensro.online/server/internal/testsupport/wait"
)

var loginSourceTestKey = []byte("login-source-test-key-0123456789")

/*
================
loginSourceFor

The source fields a login from remote with userAgent records.
================
*/
func loginSourceFor(key []byte, remote, userAgent string) map[string]string {
	server := &Server{loginSourceKey: key}
	request := httptest.NewRequest("POST", "/title/login", nil)
	request.RemoteAddr = remote
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	return server.loginSourceFields(request, map[string]string{"claimedAccount": "numz1"})
}

/*
================
TestLoginSourceKeysOneAddressAndItsNetwork
================
*/
func TestLoginSourceKeysOneAddressAndItsNetwork(t *testing.T) {
	first := loginSourceFor(loginSourceTestKey, "203.0.113.9:5000", "Firefox/140")
	again := loginSourceFor(loginSourceTestKey, "203.0.113.9:6123", "")
	neighbour := loginSourceFor(loginSourceTestKey, "203.0.113.200:5000", "")
	elsewhere := loginSourceFor(loginSourceTestKey, "198.51.100.9:5000", "")

	if first["claimedAccount"] != "numz1" {
		t.Fatalf("the event's own fields were lost: %v", first)
	}
	if first["ip_key"] == "" || first["ip_key"] != again["ip_key"] || first["net_key"] != again["net_key"] {
		t.Fatalf("one address, two ports: %v vs %v", first, again)
	}
	if neighbour["ip_key"] == first["ip_key"] || neighbour["net_key"] != first["net_key"] {
		t.Fatalf("one /24, two addresses: %v vs %v", first, neighbour)
	}
	if elsewhere["net_key"] == first["net_key"] {
		t.Fatalf("two networks share a key: %v vs %v", first, elsewhere)
	}
	if first["user_agent"] != "Firefox/140" {
		t.Fatalf("user_agent = %q", first["user_agent"])
	}
	if strings.Contains(first["ip_key"]+first["net_key"], "203.0.113") {
		t.Fatalf("a raw address leaked into the keys: %v", first)
	}
	if want, _ := connjournal.AddressKeys(loginSourceTestKey, "203.0.113.9"); first["ip_key"] != want {
		t.Fatalf("ip_key is not the journal's hash: %q vs %q", first["ip_key"], want)
	}
}

/*
================
TestLoginSourceRecordsNothingWithoutAKeyOrAnAddress
================
*/
func TestLoginSourceRecordsNothingWithoutAKeyOrAnAddress(t *testing.T) {
	off := loginSourceFor(nil, "203.0.113.9:5000", "Firefox/140")
	if len(off) != 1 {
		t.Fatalf("the census off still recorded a source: %v", off)
	}
	unparsed := loginSourceFor(loginSourceTestKey, "pipe", "")
	if _, found := unparsed["ip_key"]; found {
		t.Fatalf("an unparsable peer got a guessed key: %v", unparsed)
	}
}

/*
================
TestLoginSourceBoundsTheUserAgent
================
*/
func TestLoginSourceBoundsTheUserAgent(t *testing.T) {
	long := strings.Repeat("x", connjournal.MaxUserAgentBytes+40)
	fields := loginSourceFor(loginSourceTestKey, "203.0.113.9:5000", long)
	if len(fields["user_agent"]) != connjournal.MaxUserAgentBytes {
		t.Fatalf("user_agent kept %d bytes", len(fields["user_agent"]))
	}
}

/*
================
TestLoginEventsCarryTheirSource

Through the real handler: a refused and a succeeded login both reach the
history journal with the source keys, so the wiring is covered, not only
the helper.
================
*/
func TestLoginEventsCarryTheirSource(t *testing.T) {
	ctx := context.Background()
	journal, err := history.Open(filepath.Join(t.TempDir(), "history.sqlite"), "agent", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	accounts, err := auth.OpenAccountsWith(filepath.Join(t.TempDir(), "accounts.db"), auth.AccountsOptions{BcryptCost: bcrypt.MinCost})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = accounts.Close() })
	fixture := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler(), func(config *Config) {
		config.Accounts, config.History, config.LoginSourceKey = accounts, journal, loginSourceTestKey
	})
	publishFixtureLease(t, fixture, "alpha", "worker-a", 1, 0)
	if _, err := accounts.Create(ctx, "numz1", "password1"); err != nil {
		t.Fatal(err)
	}
	performJSON(t, fixture.handler, http.MethodPost, "/title/login", `{"id":"numz1","password":"wrong-one","serverId":"alpha"}`, "")
	performJSON(t, fixture.handler, http.MethodPost, "/title/login", `{"id":"numz1","password":"password1","serverId":"alpha"}`, "")

	for _, kind := range []string{"login_refused", "login_succeeded"} {
		event := waitLoginEvent(t, journal, kind)
		if event.Fields["ip_key"] == "" || event.Fields["net_key"] == "" {
			t.Fatalf("%s carries no source: %v", kind, event.Fields)
		}
	}
}

/*
================
waitLoginEvent

The newest event of kind, once the journal's writer has stored it.
================
*/
func waitLoginEvent(t *testing.T, journal *history.Journal, kind string) history.Event {
	t.Helper()
	var event history.Event
	wait.Eventually(t, 5*time.Second, "a journaled "+kind+" event", func() bool {
		page, err := journal.Query(history.Filter{Kind: kind, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		rows, _ := page["events"].([]history.EventRow)
		if len(rows) == 0 {
			return false
		}
		event = rows[0].Event
		return true
	})
	return event
}
