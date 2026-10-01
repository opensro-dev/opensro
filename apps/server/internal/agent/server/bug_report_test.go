/*
===========================================================================

bug_report_test.go - /title/bug-report through the Agent's real handler

===========================================================================
*/
package agentserver

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"sync"
	"testing"

	"opensro.online/server/internal/agent/bugreport"
)

var testClip = append([]byte{0, 0, 0, 0x18, 'f', 't', 'y', 'p', 'i', 's', 'o', 'm'}, make([]byte, 256)...)

/*
================
discordRecorder

A stand-in Discord webhook that keeps every payload_json it receives.
================
*/
type discordRecorder struct {
	mu       sync.Mutex
	payloads []string
}

/*
================
discordRecorder.ServeHTTP
================
*/
func (recorder *discordRecorder) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	recorder.mu.Lock()
	recorder.payloads = append(recorder.payloads, r.FormValue("payload_json"))
	recorder.mu.Unlock()
	_, _ = io.WriteString(w, `{"id":"42"}`)
}

/*
================
newBugReportFixture
================
*/
func newBugReportFixture(t *testing.T, enabled bool) (agentFixture, *discordRecorder) {
	t.Helper()
	recorder := &discordRecorder{}
	discord := httptest.NewServer(recorder)
	t.Cleanup(discord.Close)
	worker := http.NotFoundHandler()
	fixture := newAgentFixture(t, worker, worker, func(config *Config) {
		if !enabled {
			return
		}
		service, err := bugreport.New(bugreport.Config{
			WebhookURL:    discord.URL + "/api/webhooks/1/token",
			ReplayDefault: false,
			MaxBytes:      1 << 20,
		}, discord.Client(), config.Now)
		if err != nil {
			t.Fatal(err)
		}
		config.BugReports = service
	})
	publishFixtureLease(t, fixture, "alpha", "worker-alpha", 1, 0)
	return fixture, recorder
}

/*
================
loginCookie
================
*/
func loginCookie(t *testing.T, fixture agentFixture) *http.Cookie {
	t.Helper()
	login := performJSON(t, fixture.handler, http.MethodPost, "/title/login", `{"id":"tester","password":"123123","serverId":"alpha"}`, "")
	cookies := login.Result().Cookies()
	if login.Code != http.StatusOK || len(cookies) == 0 {
		t.Fatalf("login failed: %d %s", login.Code, login.Body.String())
	}
	return cookies[0]
}

/*
================
postBugReport
================
*/
func postBugReport(t *testing.T, fixture agentFixture, cookie *http.Cookie, description string) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	if err := writer.WriteField("description", description); err != nil {
		t.Fatal(err)
	}
	if err := writer.WriteField("meta", `{"context":[{"name":"Build","value":"test"}]}`); err != nil {
		t.Fatal(err)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="clip"; filename="replay.mp4"`)
	header.Set("Content-Type", "video/mp4")
	part, err := writer.CreatePart(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(testClip); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/title/bug-report", &body)
	declareBrowser(request)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	return response
}

/*
================
getBugReportSettings
================
*/
func getBugReportSettings(t *testing.T, fixture agentFixture) bugreport.Settings {
	t.Helper()
	request := httptest.NewRequest(http.MethodGet, "/title/bug-report", nil)
	declareBrowser(request)
	response := httptest.NewRecorder()
	fixture.handler.ServeHTTP(response, request)
	var body struct {
		OK         bool               `json:"ok"`
		BugReports bugreport.Settings `json:"bugReports"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || !body.OK {
		t.Fatalf("settings: %d %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "webhooks") {
		t.Fatal("settings leak the webhook")
	}
	return body.BugReports
}

/*
================
TestBugReportsDisabledWithoutService
================
*/
func TestBugReportsDisabledWithoutService(t *testing.T) {
	fixture, _ := newBugReportFixture(t, false)
	if settings := getBugReportSettings(t, fixture); settings.Enabled {
		t.Fatal("bug reports enabled without a webhook")
	}
	response := postBugReport(t, fixture, loginCookie(t, fixture), "The bridge eats my character")
	if response.Code != http.StatusNotFound || !strings.Contains(response.Body.String(), "BUG_REPORTS_DISABLED") {
		t.Fatalf("got %d %s", response.Code, response.Body.String())
	}
}

/*
================
TestBugReportDeliversWithSessionIdentity
================
*/
func TestBugReportDeliversWithSessionIdentity(t *testing.T) {
	fixture, recorder := newBugReportFixture(t, true)
	settings := getBugReportSettings(t, fixture)
	if !settings.Enabled || settings.ReplayDefault || settings.MaxBytes != 1<<20 || settings.ReplaySeconds != bugreport.ReplaySeconds {
		t.Fatalf("settings %+v", settings)
	}

	if response := postBugReport(t, fixture, nil, "The bridge eats my character"); response.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous report: %d", response.Code)
	}

	cookie := loginCookie(t, fixture)
	response := postBugReport(t, fixture, cookie, "The bridge eats my character")
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"id":"42"`) {
		t.Fatalf("report: %d %s", response.Code, response.Body.String())
	}
	if len(recorder.payloads) != 1 {
		t.Fatalf("want one Discord message, got %d", len(recorder.payloads))
	}
	for _, want := range []string{`"name":"Account","value":"tester"`, `"name":"Server","value":"Alpha"`, `"allowed_mentions":{"parse":[]}`} {
		if !strings.Contains(recorder.payloads[0], want) {
			t.Fatalf("payload lacks %s: %s", want, recorder.payloads[0])
		}
	}

	again := postBugReport(t, fixture, cookie, "The bridge eats my character again")
	if again.Code != http.StatusTooManyRequests || again.Header().Get("Retry-After") != "60" {
		t.Fatalf("second report within a minute: %d %q", again.Code, again.Header().Get("Retry-After"))
	}

	invalid := postBugReport(t, fixture, cookie, "short")
	if invalid.Code != http.StatusTooManyRequests {
		t.Fatalf("pacing must be checked before the body: %d", invalid.Code)
	}
}
