/*
===========================================================================

settingsapi_test.go - the settings read: shape, hidden variables, guard

===========================================================================
*/
package settingsapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/game/tunables"
)

const testToken = "settings-test-token-0123456789abcdef"

/*
================
request

A console request: loopback peer and host, the diagnostics header and,
with auth, the bearer token.
================
*/
func request(method string, auth bool) *http.Request {
	r := httptest.NewRequest(method, "http://127.0.0.1:8791"+Path, nil)
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-SRO-Local-Diagnostics", "1")
	if auth {
		r.Header.Set("Authorization", "Bearer "+testToken)
	}
	return r
}

/*
================
TestBuildListsTunablesAndReadableFlags

Every registry row appears with its value, bounds and class; a switch
reads as on or off; identity, paths, addresses and secrets never appear;
a tunable's own variable is not repeated in the flags.
================
*/
func TestBuildListsTunablesAndReadableFlags(t *testing.T) {
	s, err := tunables.Parse(func(name string) string {
		return map[string]string{"SRO_MALL_RIDE_SPEED": "on", "SRO_JOB_KILL_NOTICES_PER_MINUTE": "9"}[name]
	})
	if err != nil {
		t.Fatal(err)
	}
	got := Build(s, []string{
		"SRO_MOST_WANTED=on", "SRO_BETA_SILK=50", "SRO_PET_PACING=off", "SRO_MALL_RIDE_SPEED=on",
		"SRO_AUTHORITY_STATE_DIR=/srv/state", "SRO_PUBLIC_API_ADDR=127.0.0.1:9", "SRO_SOME_TOKEN=x",
		"SRO_GM_CHARACTERS=Admin", "PATH=/usr/bin",
	})
	if got.SchemaVersion != 1 || len(got.Settings) != len(tunables.Registry) {
		t.Fatalf("response %+v", got)
	}
	rows := map[string]SettingRow{}
	for _, row := range got.Settings {
		rows[row.Key] = row
	}
	ride, kills := rows[tunables.MallRideSpeed], rows[tunables.JobKillNoticesPerMinute]
	if ride.Kind != "factor" || ride.Value != 1.3 || !ride.Enabled || ride.Native == nil || ride.Class != "restart" {
		t.Fatalf("mall ride row %+v", ride)
	}
	if kills.Kind != "int" || kills.Value != 9 || kills.Default != 6 || kills.Source != "env" || kills.Class != "live" {
		t.Fatalf("job-kill cap row %+v", kills)
	}
	flags := map[string]FlagRow{}
	for _, flag := range got.Flags {
		flags[flag.Env] = flag
	}
	if len(flags) != 3 || flags["SRO_MOST_WANTED"].On == nil || !*flags["SRO_MOST_WANTED"].On ||
		flags["SRO_PET_PACING"].On == nil || *flags["SRO_PET_PACING"].On || flags["SRO_BETA_SILK"].On != nil ||
		flags["SRO_BETA_SILK"].Value != "50" {
		t.Fatalf("flags %+v", got.Flags)
	}
}

/*
================
TestHandlerAnswersOnlyTheGuardedConsole
================
*/
func TestHandlerAnswersOnlyTheGuardedConsole(t *testing.T) {
	dir := t.TempDir()
	missing, err := Handler(filepath.Join(dir, "absent"), os.Environ)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	missing.ServeHTTP(w, request(http.MethodGet, true))
	if w.Code != http.StatusNotFound {
		t.Fatalf("no token file answered %d, want 404", w.Code)
	}
	tokenPath := filepath.Join(dir, "operator-token")
	if err := os.WriteFile(tokenPath, []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := Handler(tokenPath, func() []string { return []string{"SRO_MOST_WANTED=on"} })
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		label string
		r     *http.Request
		want  int
	}{
		{"no token", request(http.MethodGet, false), http.StatusForbidden},
		{"a browser", func() *http.Request {
			r := request(http.MethodGet, true)
			r.Header.Set("Origin", "https://x")
			return r
		}(), http.StatusForbidden},
		{"a write", request(http.MethodPost, true), http.StatusMethodNotAllowed},
		{"the console", request(http.MethodGet, true), http.StatusOK},
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, tc.r)
		if w.Code != tc.want {
			t.Fatalf("%s: %d, want %d", tc.label, w.Code, tc.want)
		}
		if tc.want != http.StatusOK {
			continue
		}
		var body Response
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || body.SchemaVersion != 1 ||
			len(body.Flags) != 1 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("console body %q (%v)", w.Body.String(), err)
		}
	}
}
