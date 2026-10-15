/*
===========================================================================

routes_test.go - the write and audit routes over HTTP

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

/*
================
routedHandler

The settings handler with a token file and service over a memStore.
================
*/
func routedHandler(t *testing.T) (http.Handler, *memStore) {
	t.Helper()
	service, store := newService(t)
	tokenPath := filepath.Join(t.TempDir(), "operator-token")
	if err := os.WriteFile(tokenPath, []byte(testToken), 0o600); err != nil {
		t.Fatal(err)
	}
	handler, err := Handler(tokenPath, func(string) string { return "" }, service)
	if err != nil {
		t.Fatal(err)
	}
	return handler, store
}

/*
================
call

One guarded console request; staff empty sends no identity.
================
*/
func call(handler http.Handler, method, route, staff, body string) (int, map[string]any) {
	r := httptest.NewRequest(method, "http://127.0.0.1:8791"+Path+route, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:50000"
	r.Header.Set("X-SRO-Local-Diagnostics", "1")
	r.Header.Set("Authorization", "Bearer "+testToken)
	if staff != "" {
		r.Header.Set(HeaderStaff, staff)
		r.Header.Set(HeaderStaffName, strings.ToUpper(staff))
	}
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, r)
	var out map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w.Code, out
}

/*
================
TestWriteRoutesNeedAStaffIdentity
================
*/
func TestWriteRoutesNeedAStaffIdentity(t *testing.T) {
	handler, store := routedHandler(t)
	code, out := call(handler, http.MethodPost, "/proposals", "", `{"key":"job_kill_notices_per_minute","to":"3","reason":"x"}`)
	if code != http.StatusPreconditionFailed || out["error"] == nil || len(store.audit) != 0 {
		t.Fatalf("no identity: %d %v", code, out)
	}
}

/*
================
TestProposeApproveOverHTTP

Alice proposes (201), the read shows it pending, Alice cannot approve her
own (403), a second proposal for the key is refused (409), Bob approves
(applied), and the audit route lists every step newest first.
================
*/
func TestProposeApproveOverHTTP(t *testing.T) {
	handler, _ := routedHandler(t)
	code, out := call(handler, http.MethodPost, "/proposals", "discord:1",
		`{"key":"job_kill_notices_per_minute","to":"3","reason":"quieter"}`)
	if code != http.StatusCreated || out["id"] == nil {
		t.Fatalf("propose %d %v", code, out)
	}
	id := out["id"].(string)
	code, out = call(handler, http.MethodGet, "", "", "")
	settings, _ := out["settings"].([]any)
	pending := 0
	for _, row := range settings {
		if row.(map[string]any)["pending"] != nil {
			pending++
		}
	}
	if code != http.StatusOK || pending != 1 {
		t.Fatalf("read %d, %d pending", code, pending)
	}
	if code, _ = call(handler, http.MethodPost, "/proposals/"+id+"/approve", "discord:1", "{}"); code != http.StatusForbidden {
		t.Fatalf("self approval %d", code)
	}
	if code, _ = call(handler, http.MethodPost, "/proposals", "discord:2",
		`{"key":"job_kill_notices_per_minute","to":"4","reason":"mine"}`); code != http.StatusConflict {
		t.Fatalf("second proposal %d", code)
	}
	code, out = call(handler, http.MethodPost, "/proposals/"+id+"/approve", "discord:2", "")
	if code != http.StatusOK || out["result"] != ResultApplied || tunables.Current().Int(tunables.JobKillNoticesPerMinute) != 3 {
		t.Fatalf("approve %d %v", code, out)
	}
	code, out = call(handler, http.MethodGet, "/audit", "", "")
	entries, _ := out["entries"].([]any)
	if code != http.StatusOK || len(entries) != 4 {
		t.Fatalf("audit %d %v", code, out)
	}
	if code, _ = call(handler, http.MethodPost, "/proposals/"+id+"/approve", "discord:2", ""); code != http.StatusNotFound {
		t.Fatalf("a decided proposal answered %d", code)
	}
}
