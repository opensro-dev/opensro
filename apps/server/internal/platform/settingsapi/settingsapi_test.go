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

	"opensro.online/server/internal/config/shardsettings"
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
TestBuildListsTunablesAndOnlyShardSettings

Every registry row appears with its value, bounds and class. The flags
are exactly the shard settings table minus the tunables' own variables,
with their raw values and on/off: a webhook, a URL, the GM list or any
other variable outside the table never appears, whatever the environment
holds.
================
*/
func TestBuildListsTunablesAndOnlyShardSettings(t *testing.T) {
	environment := map[string]string{
		"SRO_MALL_RIDE_SPEED": "on", "SRO_JOB_KILL_NOTICES_PER_MINUTE": "9", "SRO_BETA_SILK": "50",
		"SRO_PET_PACING": "off", "SRO_BETA_STARTER_KIT": "on",
		"SRO_X_WEBHOOK": "https://discord.example/api/webhooks/1/secret", "SRO_BUG_REPORT_DISCORD_WEBHOOK": "secret",
		"SRO_GM_CHARACTERS": "Admin", "SRO_AGENT_URL": "http://10.0.0.1:1", "SRO_AUTHORITY_STATE_DIR": "/srv/state",
	}
	getenv := func(name string) string { return environment[name] }
	s, err := tunables.Parse(getenv)
	if err != nil {
		t.Fatal(err)
	}
	got := Build(s, getenv)
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
	allowed := map[string]bool{}
	tunableEnv := map[string]bool{}
	for _, row := range tunables.Registry {
		tunableEnv[row.Env] = true
	}
	for _, setting := range shardsettings.Settings {
		if !tunableEnv[setting.Env] {
			allowed[setting.Env] = true
		}
	}
	if len(flags) != len(allowed) {
		t.Fatalf("%d flags, want the %d shard settings", len(flags), len(allowed))
	}
	for name := range flags {
		if !allowed[name] {
			t.Fatalf("%s is listed but is not a shard setting", name)
		}
	}
	for _, secret := range []string{"SRO_X_WEBHOOK", "SRO_BUG_REPORT_DISCORD_WEBHOOK", "SRO_GM_CHARACTERS", "SRO_AGENT_URL",
		"SRO_AUTHORITY_STATE_DIR", "SRO_MALL_RIDE_SPEED"} {
		if _, listed := flags[secret]; listed {
			t.Fatalf("%s is listed", secret)
		}
	}
	pet, silk := flags["SRO_PET_PACING"], flags["SRO_BETA_SILK"]
	if pet.On == nil || *pet.On || silk.On != nil || silk.Value != "50" ||
		flags["SRO_BETA_STARTER_KIT"].On == nil || !*flags["SRO_BETA_STARTER_KIT"].On {
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
	missing, err := Handler(filepath.Join(dir, "absent"), os.Getenv, nil)
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
	handler, err := Handler(tokenPath, func(name string) string { return map[string]string{"SRO_MOST_WANTED": "on"}[name] }, nil)
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
			len(body.Flags) == 0 || !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Fatalf("console body %q (%v)", w.Body.String(), err)
		}
	}
}

/*
================
TestEveryTunableIsAShardSetting

sro-nomad passes only shard settings to the job, so a tunable outside the
table could never be set in production. Each registry variable is a shard
setting whose deployment default the registry accepts.
================
*/
func TestEveryTunableIsAShardSetting(t *testing.T) {
	table := map[string]string{}
	for _, setting := range shardsettings.Settings {
		table[setting.Env] = setting.Default
	}
	for _, row := range tunables.Registry {
		value, listed := table[row.Env]
		if !listed {
			t.Fatalf("%s is a tunable but not a shard setting: production cannot set it", row.Env)
		}
		if _, err := tunables.Parse(func(name string) string {
			if name == row.Env {
				return value
			}
			return ""
		}); err != nil {
			t.Fatalf("%s: its deployment default %q is refused: %v", row.Env, value, err)
		}
	}
}
