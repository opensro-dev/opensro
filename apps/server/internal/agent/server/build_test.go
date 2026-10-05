/*
===========================================================================

build_test.go - /title/build through the Agent's real handler

===========================================================================
*/
package agentserver

import (
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
			UptimeSeconds int64   `json:"uptimeSeconds"`
		} `json:"build"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if !body.OK || body.Build.Revision == nil || body.Build.UptimeSeconds != 5430 {
		t.Fatalf("build = %s, want ok, a revision field and 5430 s", recorder.Body.String())
	}
	if *body.Build.Revision != buildRevision() {
		t.Fatalf("revision = %q, want the binary's %q", *body.Build.Revision, buildRevision())
	}
	if refused := performJSON(t, f.handler, http.MethodPost, buildPath, "{}", ""); refused.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST %s = %d, want 405", buildPath, refused.Code)
	}
}
