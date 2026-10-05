/*
===========================================================================

client_incident_test.go - /client/incident through the Agent's real handler

===========================================================================
*/
package agentserver

import (
	"net/http"
	"testing"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

/*
================
incidentSession

Logs in through the real handler and returns the session bearer token.
================
*/
func incidentSession(t *testing.T, configure ...func(*Config)) (agentFixture, string) {
	t.Helper()
	worker := http.NotFoundHandler()
	fixture := newAgentFixture(t, worker, worker, configure...)
	publishFixtureLease(t, fixture, "alpha", "worker-alpha", 1, 0)
	login := performJSON(t, fixture.handler, http.MethodPost, "/title/login",
		`{"id":"tester","password":"123123","serverId":"alpha"}`, "")
	token, _ := decodeObject(t, login)["sessionToken"].(string)
	if token == "" {
		t.Fatalf("login = %s", login.Body.String())
	}
	return fixture, token
}

/*
================
TestClientIncidentIsLoggedWithTheFailingFrame
================
*/
func TestClientIncidentIsLoggedWithTheFailingFrame(t *testing.T) {
	fixture, token := incidentSession(t)
	hook := logtest.NewGlobal()
	defer hook.Reset()

	body := `{"kind":"packet","message":"Packet application failed: Error: Invalid movement speed channels",` +
		`"opcode":14191,"payload":"2a000000000000000000","payloadSize":12,"phase":"world",` +
		`"character":"asd3","region":23960,"build":"abc123"}`
	response := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token)
	if response.Code != http.StatusOK {
		t.Fatalf("incident = %d %s", response.Code, response.Body.String())
	}
	entry := hook.LastEntry()
	if entry == nil || entry.Level != log.WarnLevel || entry.Message != "agent: client incident" {
		t.Fatalf("no incident log line: %+v", entry)
	}
	want := map[string]any{
		"account": "tester", "shard": "alpha", "kind": "packet", "opcode": "0x376F", "payload": "2a000000000000000000",
		"payloadSize": 12, "character": "asd3", "region": 23960, "phase": "world", "build": "abc123",
	}
	for key, value := range want {
		if entry.Data[key] != value {
			t.Errorf("%s = %v, want %v", key, entry.Data[key], value)
		}
	}
}

/*
================
TestClientIncidentRefusesAnonymousAndMalformedReports
================
*/
func TestClientIncidentRefusesAnonymousAndMalformedReports(t *testing.T) {
	fixture, token := incidentSession(t)
	hook := logtest.NewGlobal()
	defer hook.Reset()

	valid := `{"kind":"packet","message":"x"}`
	if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, valid, "").Code; code != http.StatusUnauthorized {
		t.Fatalf("anonymous = %d", code)
	}
	for _, body := range []string{
		`{"kind":"other","message":"x"}`,
		`{"kind":"packet","message":""}`,
		`{"kind":"packet","message":"x","opcode":70000}`,
		`{"kind":"packet","message":"x","payload":"zz"}`,
		`{"kind":"packet","message":"x","character":"a\nb"}`,
		`{"kind":"packet","message":"x","extra":1}`,
	} {
		if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code; code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, code)
		}
	}
	for _, entry := range hook.AllEntries() {
		if entry.Message == "agent: client incident" {
			t.Fatalf("a refused report was logged: %+v", entry.Data)
		}
	}
}

/*
================
TestClientIncidentIsPacedPerAccount
================
*/
func TestClientIncidentIsPacedPerAccount(t *testing.T) {
	fixture, token := incidentSession(t)
	body := `{"kind":"packet","message":"x"}`
	limited := false
	for range loginAttemptBurst + 1 {
		if performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code == http.StatusTooManyRequests {
			limited = true
		}
	}
	if !limited {
		t.Fatal("a burst past the bound was never paced")
	}
}

/*
================
revocableAccounts

The test accounts with one account that can be deleted after login.
================
*/
type revocableAccounts struct {
	AccountAuthority
	revoked string
}

/*
================
revocableAccounts.PasswordHash
================
*/
func (accounts *revocableAccounts) PasswordHash(accountID string) ([]byte, bool) {
	if accountID == accounts.revoked {
		return nil, false
	}
	return accounts.AccountAuthority.PasswordHash(accountID)
}

/*
================
TestClientIncidentRefusesADeletedAccountsSession

A token stays correctly signed after its account is deleted; the report is
refused and nothing is logged for it.
================
*/
func TestClientIncidentRefusesADeletedAccountsSession(t *testing.T) {
	accounts := &revocableAccounts{}
	fixture, token := incidentSession(t, func(config *Config) {
		accounts.AccountAuthority = config.Accounts
		config.Accounts = accounts
	})
	hook := logtest.NewGlobal()
	defer hook.Reset()
	accounts.revoked = "tester"
	body := `{"kind":"packet","message":"x"}`
	if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code; code != http.StatusUnauthorized {
		t.Fatalf("deleted account = %d", code)
	}
	for _, entry := range hook.AllEntries() {
		if entry.Message == "agent: client incident" {
			t.Fatalf("a deleted account's report was logged: %+v", entry.Data)
		}
	}
}
