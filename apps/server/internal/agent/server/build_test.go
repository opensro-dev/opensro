/*
===========================================================================

build_test.go - /title/build through the Agent's real handler

===========================================================================
*/
package agentserver

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

/*
================
TestBuildAnswersUptimeFromTheAgentClock

The uptime is the Agent clock's distance from construction, so a client
can add its own elapsed time; only GET is served.
================
*/
func TestBuildAnswersUptimeFromTheAgentClock(t *testing.T) {
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	worker := http.NotFoundHandler()
	f := newAgentFixture(t, worker, worker, func(config *Config) {
		config.Now = func() time.Time { return clock }
	})
	clock = clock.Add(90*time.Minute + 30*time.Second)
	recorder := performJSON(t, f.handler, http.MethodGet, buildPath, "", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET %s = %d %s", buildPath, recorder.Code, recorder.Body.String())
	}
	var body struct {
		OK    bool `json:"ok"`
		Build struct {
			Revision      *string `json:"revision"`
			Subject       *string `json:"subject"`
			UptimeSeconds int64   `json:"uptimeSeconds"`
		} `json:"build"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Build.Revision == nil || body.Build.Subject == nil || body.Build.UptimeSeconds != 5430 {
		t.Fatalf("build = %s, want ok, revision and subject fields and 5430 s", recorder.Body.String())
	}
	if *body.Build.Revision != buildRevision() {
		t.Fatalf("revision = %q, want the binary's %q", *body.Build.Revision, buildRevision())
	}
	if refused := performJSON(t, f.handler, http.MethodPost, buildPath, "{}", ""); refused.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s = %d, want 405", buildPath, refused.Code)
	}
}

/*
================
TestBuildSubjectDecodesTheLinkTimeStamp

The deployer stamps base64 so any subject survives -ldflags; a missing or
damaged stamp reads as no subject.
================
*/
func TestBuildSubjectDecodesTheLinkTimeStamp(t *testing.T) {
	saved := buildSubjectBase64
	t.Cleanup(func() { buildSubjectBase64 = saved })
	for stamp, want := range map[string]string{
		"": "",
		base64.StdEncoding.EncodeToString([]byte(`Fix "quotes" and 'more'`)): `Fix "quotes" and 'more'`,
		"not base64!": "",
	} {
		buildSubjectBase64 = stamp
		if got := buildSubject(); got != want {
			t.Fatalf("stamp %q: subject %q, want %q", stamp, got, want)
		}
	}
}
