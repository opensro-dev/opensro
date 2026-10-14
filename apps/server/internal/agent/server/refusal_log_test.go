/*
===========================================================================

refusal_log_test.go - every refused title and world-entry request is logged

Login, the routes the Agent forwards to a GameWorld (character select, area
entry, tokens) and pre-login incident reports, through the Agent's real
handler. A line names its route and code, never a password, a token or a
GameWorld answer's text, and one client cannot fill the log.

===========================================================================
*/
package agentserver

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"opensro.online/server/internal/releaseprotocol"

	log "github.com/sirupsen/logrus"
	logtest "github.com/sirupsen/logrus/hooks/test"
)

// refusedLine is the message of every refusal line (refusal_log.go).
const refusedLine = "agent: request refused"

/*
================
refusalLines

The refusal lines the hook captured.
================
*/
func refusalLines(hook *logtest.Hook) []*log.Entry {
	var lines []*log.Entry
	for _, entry := range hook.AllEntries() {
		if entry.Message == refusedLine {
			lines = append(lines, entry)
		}
	}
	return lines
}

/*
================
requireNoSecret

No field of any captured line carries one of the secrets.
================
*/
func requireNoSecret(t *testing.T, hook *logtest.Hook, secrets ...string) {
	t.Helper()
	for _, entry := range hook.AllEntries() {
		text := entry.Message + fmt.Sprint(entry.Data)
		for _, secret := range secrets {
			if secret != "" && strings.Contains(text, secret) {
				t.Fatalf("a log line carries %q: %s %+v", secret, entry.Message, entry.Data)
			}
		}
	}
}

/*
================
TestLoginRefusalsAreLoggedWithTheirCode
================
*/
func TestLoginRefusalsAreLoggedWithTheirCode(t *testing.T) {
	for _, c := range []struct {
		name, body, code string
	}{
		{"wrong password", `{"id":"tester","password":"wrongpass9","serverId":"alpha"}`, "INVALID_CREDENTIALS"},
		{"unknown account", `{"id":"nobody","password":"wrongpass9","serverId":"alpha"}`, "INVALID_CREDENTIALS"},
		// A password typed into the id box names no account: never logged.
		{"password in the id box", `{"id":"hunter2-secret","password":"wrongpass9","serverId":"alpha"}`, "INVALID_CREDENTIALS"},
		{"unknown server", `{"id":"tester","password":"wrongpass9","serverId":"zeta"}`, "UNKNOWN_SHARD"},
		{"malformed", `{"id":`, "BAD_REQUEST"},
	} {
		t.Run(c.name, func(t *testing.T) {
			worker := http.NotFoundHandler()
			fixture := newAgentFixture(t, worker, worker)
			publishFixtureLease(t, fixture, "alpha", "worker-alpha", 1, 0)
			hook := logtest.NewGlobal()
			defer hook.Reset()
			performJSON(t, fixture.handler, http.MethodPost, loginPath, c.body, "")
			lines := refusalLines(hook)
			if len(lines) != 1 {
				t.Fatalf("%d refusal lines, want 1", len(lines))
			}
			line := lines[0]
			if line.Level != log.WarnLevel || line.Data["route"] != loginPath || line.Data["code"] != c.code ||
				line.Data["source"] != "agent" || line.Data["client"] == "" {
				t.Fatalf("line = %+v", line.Data)
			}
			requireNoSecret(t, hook, "wrongpass9", "hunter2-secret", "nobody")
			if want := map[bool]string{true: "tester", false: notAnAccount}[c.name == "wrong password"]; c.code != "BAD_REQUEST" &&
				c.name != "unknown server" && line.Data["account"] != want {
				t.Fatalf("account = %v, want %q", line.Data["account"], want)
			}
		})
	}
}

/*
================
TestRefusalLinesArePacedPerClient

A password spray from one address writes at most the pacer's burst of
lines; the refusals themselves are all answered.
================
*/
func TestRefusalLinesArePacedPerClient(t *testing.T) {
	worker := http.NotFoundHandler()
	fixture := newAgentFixture(t, worker, worker)
	publishFixtureLease(t, fixture, "alpha", "worker-alpha", 1, 0)
	hook := logtest.NewGlobal()
	defer hook.Reset()
	const attempts = 4 * loginAttemptBurst
	for range attempts {
		response := performJSON(t, fixture.handler, http.MethodPost, loginPath,
			`{"id":"tester","password":"wrongpass9","serverId":"alpha"}`, "")
		if body := decodeObject(t, response); body["ok"] != false {
			t.Fatalf("a refused login answered %s", response.Body.String())
		}
	}
	if lines := len(refusalLines(hook)); lines == 0 || lines > loginAttemptBurst {
		t.Fatalf("%d refusal lines for %d refusals, want 1..%d", lines, attempts, loginAttemptBurst)
	}
}

/*
================
TestShardRouteRefusalsAreLogged

A refusal the Agent makes and one the GameWorld answers are both logged
with their codes. The GameWorld's message text never reaches the log, and
a code that is not enum-shaped is logged as UNCODED.
================
*/
func TestShardRouteRefusalsAreLogged(t *testing.T) {
	answer := `{"ok":false,"code":"UNKNOWN_CHARACTER","message":"gameworld private text"}`
	worker := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(answer))
	})
	gameworld := newAgentFixture(t, worker, worker)
	publishFixtureLease(t, gameworld, "alpha", "worker-alpha", 1, 0)
	login := performJSON(t, gameworld.handler, http.MethodPost, loginPath,
		`{"id":"tester","password":"123123","serverId":"alpha"}`, "")
	token, _ := decodeObject(t, login)["sessionToken"].(string)
	if token == "" {
		t.Fatalf("login = %s", login.Body.String())
	}
	hook := logtest.NewGlobal()
	defer hook.Reset()

	if code := performJSON(t, gameworld.handler, http.MethodPost, "/character/list", `{}`, "").Code; code != http.StatusUnauthorized {
		t.Fatalf("anonymous list = %d", code)
	}
	if code := performJSON(t, gameworld.handler, http.MethodPost, "/character/enter-area", `{}`, token).Code; code != http.StatusNotFound {
		t.Fatalf("relayed refusal = %d", code)
	}
	lines := refusalLines(hook)
	if len(lines) != 2 {
		t.Fatalf("%d refusal lines, want 2", len(lines))
	}
	if lines[0].Data["route"] != "/character/list" || lines[0].Data["code"] != "UNAUTHORIZED" || lines[0].Data["source"] != "agent" {
		t.Fatalf("agent refusal line = %+v", lines[0].Data)
	}
	if lines[1].Data["route"] != "/character/enter-area" || lines[1].Data["code"] != "UNKNOWN_CHARACTER" ||
		lines[1].Data["source"] != "gameworld" || lines[1].Data["account"] != "tester" || lines[1].Data["status"] != http.StatusNotFound {
		t.Fatalf("relayed refusal line = %+v", lines[1].Data)
	}
	requireNoSecret(t, hook, "gameworld private text", token)
}

/*
================
TestRelayedRefusalCodeKeepsOnlyAnEnum
================
*/
func TestRelayedRefusalCodeKeepsOnlyAnEnum(t *testing.T) {
	for _, c := range []struct {
		status  int
		body    string
		code    string
		refused bool
	}{
		{200, `{"ok":true,"code":"ANYTHING"}`, "", false},
		{200, `{"characters":[]}`, "", false},
		{200, `{"ok":false,"code":"CHARACTER_IN_PLAY"}`, "CHARACTER_IN_PLAY", true},
		{409, `{"ok":false,"code":"lower case text"}`, "UNCODED", true},
		{502, `not json`, "UNCODED", true},
		{400, `{"ok":false,"code":"` + strings.Repeat("A", 200) + `"}`, strings.Repeat("A", maxRelayedCodeLength), true},
	} {
		code, refused := relayedRefusalCode(c.status, []byte(c.body))
		if code != c.code || refused != c.refused {
			t.Errorf("%d %s = %q %v, want %q %v", c.status, c.body, code, refused, c.code, c.refused)
		}
	}
}

/*
================
TestClientTagIsKeyed

The tag is neither the bare address nor its unkeyed hash, and two Agents
(two keys) tag the same address differently.
================
*/
func TestClientTagIsKeyed(t *testing.T) {
	first := &Server{clientKey: []byte(strings.Repeat("a", clientKeyBytes))}
	second := &Server{clientKey: []byte(strings.Repeat("b", clientKeyBytes))}
	request := httptest.NewRequest(http.MethodPost, loginPath, nil)
	request.RemoteAddr = "203.0.113.7:4242"
	tag := first.clientTag(request)
	unkeyed := sha256.Sum256([]byte("203.0.113.7"))
	if len(tag) != clientTagHexChars || strings.Contains(tag, "203.0.113.7") ||
		strings.HasPrefix(hex.EncodeToString(unkeyed[:]), tag) {
		t.Fatalf("tag %q is not keyed", tag)
	}
	if tag == second.clientTag(request) {
		t.Fatal("two keys gave one tag")
	}
	if tag != first.clientTag(request) {
		t.Fatal("one key gave two tags")
	}
}

/*
================
TestClientIncidentBeforeLoginIsKeptAnonymously

A report with no session is logged as before-login with a client tag,
paced per address, and never takes an account from its body. A report that
presents a dead session is still refused, never downgraded.
================
*/
func TestClientIncidentBeforeLoginIsKeptAnonymously(t *testing.T) {
	fixture, _ := incidentSession(t)
	hook := logtest.NewGlobal()
	defer hook.Reset()

	body := `{"kind":"runtime","message":"Runtime failed: title scene","phase":"title","character":"tester"}`
	response := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, "")
	if response.Code != http.StatusOK {
		t.Fatalf("anonymous report = %d %s", response.Code, response.Body.String())
	}
	entry := hook.LastEntry()
	if entry == nil || entry.Message != "agent: client incident" || entry.Data["account"] != anonymousIncidentAccount ||
		entry.Data["client"] == "" || entry.Data["shard"] != "" || entry.Data["phase"] != "title" {
		t.Fatalf("anonymous line = %+v", entry)
	}
	if code := performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, "not-a-token").Code; code != http.StatusUnauthorized {
		t.Fatalf("a dead session = %d, want 401", code)
	}
	paced := false
	for range 4 * loginAttemptBurst {
		if performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, body, "").Code == http.StatusTooManyRequests {
			paced = true
			break
		}
	}
	if !paced {
		t.Fatal("anonymous reports from one address were never paced")
	}
}

/*
================
TestClientIncidentBeforeLoginKeepsTheReleaseProtocol

/client/incident stays behind releaseprotocol.Require: an anonymous report
from a tab of another release is answered 426 and is not an incident.
================
*/
func TestClientIncidentBeforeLoginKeepsTheReleaseProtocol(t *testing.T) {
	fixture, _ := incidentSession(t)
	hook := logtest.NewGlobal()
	defer hook.Reset()

	request := httptest.NewRequest(http.MethodPost, clientIncidentPath,
		strings.NewReader(`{"kind":"runtime","message":"x","phase":"title"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(releaseprotocol.Header, fmt.Sprint(releaseprotocol.Current-1))
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	if response.Code != http.StatusUpgradeRequired {
		t.Fatalf("wrong-protocol report = %d, want 426", response.Code)
	}
	for _, entry := range hook.AllEntries() {
		if entry.Message == "agent: client incident" {
			t.Fatalf("a wrong-protocol report was logged: %+v", entry.Data)
		}
	}
}

/*
================
TestRefusalLinesHaveAShardWideCeiling

Rotating addresses each get the per-address allowance, so the Agent also
bounds the lines it writes per window. Past the ceiling the lines are
counted, and the next window opens with one summary naming the count.
================
*/
func TestRefusalLinesHaveAShardWideCeiling(t *testing.T) {
	worker := http.NotFoundHandler()
	fixture := newAgentFixture(t, worker, worker)
	publishFixtureLease(t, fixture, "alpha", "worker-alpha", 1, 0)
	hook := logtest.NewGlobal()
	defer hook.Reset()
	const extra = 30
	for i := range refusalLinesPerWindow + extra {
		request := httptest.NewRequest(http.MethodPost, loginPath,
			strings.NewReader(`{"id":"tester","password":"wrongpass9","serverId":"zeta"}`))
		request.Header.Set("Content-Type", "application/json")
		declareBrowser(request)
		// A distinct address per attempt: the per-address pacer never trips.
		request.RemoteAddr = fmt.Sprintf("198.51.%d.%d:4242", i/250, i%250+1)
		fixture.handler.ServeHTTP(httptest.NewRecorder(), request)
	}
	if lines := len(refusalLines(hook)); lines != refusalLinesPerWindow {
		t.Fatalf("%d refusal lines, want the ceiling %d", lines, refusalLinesPerWindow)
	}
	*fixture.now = fixture.now.Add(refusalWindow)
	performJSON(t, fixture.handler, http.MethodPost, loginPath, `{"id":"tester","password":"x","serverId":"zeta"}`, "")
	summaries := 0
	for _, entry := range hook.AllEntries() {
		if entry.Message == "agent: request refusal lines suppressed" {
			summaries++
			if entry.Data["suppressed"] != extra {
				t.Fatalf("summary = %+v, want %d suppressed", entry.Data, extra)
			}
		}
	}
	if summaries != 1 {
		t.Fatalf("%d summary lines, want 1", summaries)
	}
}

/*
================
TestAnonymousIncidentsHaveAnAgentWideCeiling

Pre-login reports from rotating addresses each pass the per-address pacer;
the Agent keeps at most its own budget per window, answers the rest 429, and
opens the next window with one summary naming the count.
================
*/
func TestAnonymousIncidentsHaveAnAgentWideCeiling(t *testing.T) {
	fixture, _ := incidentSession(t)
	hook := logtest.NewGlobal()
	defer hook.Reset()
	const extra = 15
	refused := 0
	for i := range anonymousIncidentsPerWindow + extra {
		request := httptest.NewRequest(http.MethodPost, clientIncidentPath,
			strings.NewReader(`{"kind":"runtime","message":"Runtime failed","phase":"title"}`))
		request.Header.Set("Content-Type", "application/json")
		declareBrowser(request)
		request.RemoteAddr = fmt.Sprintf("203.0.%d.%d:4242", i/250, i%250+1)
		response := httptest.NewRecorder()
		fixture.handler.ServeHTTP(response, request)
		if response.Code == http.StatusTooManyRequests {
			refused++
		}
	}
	kept := 0
	for _, entry := range hook.AllEntries() {
		if entry.Message == "agent: client incident" {
			kept++
		}
	}
	if kept != anonymousIncidentsPerWindow || refused != extra {
		t.Fatalf("kept %d, refused %d; want %d and %d", kept, refused, anonymousIncidentsPerWindow, extra)
	}
	*fixture.now = fixture.now.Add(refusalWindow)
	performJSON(t, fixture.handler, http.MethodPost, clientIncidentPath, `{"kind":"runtime","message":"x","phase":"title"}`, "")
	summaries := 0
	for _, entry := range hook.AllEntries() {
		if entry.Message == "agent: anonymous client incidents suppressed" {
			summaries++
			if entry.Data["suppressed"] != extra {
				t.Fatalf("summary = %+v, want %d suppressed", entry.Data, extra)
			}
		}
	}
	if summaries != 1 {
		t.Fatalf("%d summary lines, want 1", summaries)
	}
}
