/*
===========================================================================

client_incident_test.go - /client/incident through the Agent's real handler

===========================================================================
*/
package agentserver

import (
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/platform/history"

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
TestClientIncidentCarriesItsFrameJournal

A fatal report brings the frames before the failure (B1, "Unmatched COS
transfer result", had no other evidence): the log line a reader scans and
the history event both carry them, payloads never.
================
*/
func TestClientIncidentCarriesItsFrameJournal(t *testing.T) {
	fixture, token := incidentSession(t)
	hook := logtest.NewGlobal()
	defer hook.Reset()

	body := `{"kind":"packet","message":"Packet application failed: Error: Unmatched COS transfer result",` +
		`"opcode":45165,"payload":"011a270040020013","payloadSize":8,"phase":"world",` +
		`"journal":[-2048,2,0,12,-1520,1,28781,7,-1448,0,45165,8,0,0,45165,8]}`
	response := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token)
	if response.Code != http.StatusOK {
		t.Fatalf("incident = %d %s", response.Code, response.Body.String())
	}
	entry := hook.LastEntry()
	want := "-2048:g:0000:12 -1520:o:706D:7 -1448:i:B06D:8 0:i:B06D:8"
	if entry == nil || entry.Data["journal"] != want {
		t.Fatalf("journal = %v, want %q", entry, want)
	}
}

/*
================
TestClientIncidentKeepsItsReportWhenTheJournalIsMalformed

A journal is whole rows of a time at or before the failure, a known
direction, an opcode and a size. A bad one is dropped and named; the
incident it came with is still recorded (#788: a strict check on a new
client field must not discard the evidence it exists to collect).
================
*/
func TestClientIncidentKeepsItsReportWhenTheJournalIsMalformed(t *testing.T) {
	for journal, reason := range map[string]string{
		`[-1,0,1]`:        "partial row",
		`[5,0,1,1]`:       "row 0: age 5",
		`[-1,3,1,1]`:      "row 0: direction 3",
		`[-1,0,65536,1]`:  "row 0: opcode 65536",
		`[-1.5,0,1,1]`:    "row 0: age -1.5",
		`[-600001,0,1,1]`: "row 0: age -600001",
	} {
		fixture, token := incidentSession(t)
		hook := logtest.NewGlobal()
		body := `{"kind":"packet","message":"x","journal":` + journal + `}`
		if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code; code != http.StatusOK {
			t.Errorf("%s = %d, want the incident kept", journal, code)
		}
		entry := hook.LastEntry()
		if entry == nil || entry.Message != "agent: client incident" || entry.Data["journalInvalid"] != reason || entry.Data["journal"] != nil {
			t.Errorf("%s: logged %+v, want journalInvalid %q and no journal", journal, entry, reason)
		}
		hook.Reset()
	}
}

/*
================
TestClientIncidentRefusalIsLoggedWithItsReason

A refused report is evidence lost: the Agent names why instead of
dropping it silently.
================
*/
func TestClientIncidentRefusalIsLoggedWithItsReason(t *testing.T) {
	fixture, token := incidentSession(t)
	hook := logtest.NewGlobal()
	defer hook.Reset()
	for body, reason := range map[string]string{
		`{"kind":"other","message":"x"}`:            "unknown kind or bad message",
		`{"kind":"packet","message":"x","extra":1}`: "malformed body: ",
	} {
		if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code; code != http.StatusBadRequest {
			t.Fatalf("%s = %d", body, code)
		}
		entry := hook.LastEntry()
		got, _ := entry.Data["reason"].(string)
		if entry == nil || entry.Message != "agent: client incident refused" || !strings.HasPrefix(got, reason) {
			t.Errorf("%s: logged %+v, want reason %q", body, entry, reason)
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

/*
================
TestClientDegradedReportIsKeptLoggedAndGrouped

A non-fatal "degraded" report (the client skipped a cosmetic step) is kept,
logged as "agent: client degraded" with its site code (never the account),
and recorded in the degraded category the console groups by code. It must
name its site; a fatal kind cannot claim the category.
================
*/
func TestClientDegradedReportIsKeptLoggedAndGrouped(t *testing.T) {
	journal, err := history.Open(filepath.Join(t.TempDir(), "history.sqlite"), "agent", "", "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	fixture, token := incidentSession(t, func(config *Config) { config.History = journal })
	hook := logtest.NewGlobal()
	defer hook.Reset()

	body := `{"kind":"degraded","category":"degraded","code":"item-use-visual-unknown-ref",` +
		`"message":"Missing external item reference 3787","opcode":13385,"payload":"cb860100cb0e0000","payloadSize":8}`
	if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code; code != http.StatusOK {
		t.Fatalf("degraded report = %d", code)
	}
	entry := hook.LastEntry()
	if entry == nil || entry.Message != "agent: client degraded" || entry.Data["code"] != "item-use-visual-unknown-ref" {
		t.Fatalf("logged %+v", entry)
	}
	if _, named := entry.Data["account"]; named {
		t.Fatalf("degraded line names the account: %+v", entry.Data)
	}
	data, err := journal.Query(history.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	groups := data["degraded"].([]map[string]any)
	if len(groups) != 1 || groups[0]["code"] != "item-use-visual-unknown-ref" || groups[0]["count"] != int64(1) {
		t.Fatalf("degraded groups = %+v", groups)
	}
	example := groups[0]["example"].(map[string]any)
	if example["opcode"] != "0x3449" || example["payload"] != "cb860100cb0e0000" {
		t.Fatalf("example = %+v", example)
	}
	for body, reason := range map[string]string{
		`{"kind":"degraded","message":"x"}`:                                "degraded report without a code",
		`{"kind":"packet","category":"degraded","message":"x","code":"y"}`: "degraded category on a fatal report",
	} {
		if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code; code != http.StatusBadRequest {
			t.Errorf("%s = %d", body, code)
		}
		if got, _ := hook.LastEntry().Data["reason"].(string); got != reason {
			t.Errorf("%s refused for %q, want %q", body, got, reason)
		}
	}
}

/*
================
TestClientDegradedReportsNeverStarveAFatalOne

Degraded reports draw on their own allowance: once they use it up, the
account's session-ending failure is still accepted.
================
*/
func TestClientDegradedReportsNeverStarveAFatalOne(t *testing.T) {
	fixture, token := incidentSession(t)
	limited := false
	for i := 0; i < 40 && !limited; i++ {
		body := `{"kind":"degraded","code":"site-` + strconv.Itoa(i) + `","message":"x"}`
		limited = performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, token).Code == http.StatusTooManyRequests
	}
	if !limited {
		t.Fatal("degraded reports were never paced")
	}
	fatal := `{"kind":"packet","message":"Packet application failed: Error: x"}`
	if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, fatal, token).Code; code != http.StatusOK {
		t.Fatalf("a fatal report after the degraded allowance ran out = %d, want accepted", code)
	}
}
