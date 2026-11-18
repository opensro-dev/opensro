/*
===========================================================================

main_test.go - the relay's route list, the panel's guard, and the calls
the panel makes for a form

===========================================================================
*/
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

/*
================
TestRelayForwardsOnlyThePanelRoutes
================
*/
func TestRelayForwardsOnlyThePanelRoutes(t *testing.T) {
	var seen []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Forwarded-For") != "" || r.Header.Get("Authorization") != "Bearer secret" {
			t.Errorf("relay rewrote the request: %v", r.Header)
		}
		seen = append(seen, r.Method+" "+r.URL.Path)
	}))
	defer upstream.Close()
	relay, err := newRelay(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		method, path string
		allowed      bool
	}{
		{"POST", "/v1/accounts", true},
		{"GET", "/v1/accounts/alice", true},
		{"PUT", "/v1/accounts/alice/password", true},
		{"PUT", "/v1/accounts/alice/disabled", true},
		{"GET", "/v1/accounts", false},
		{"DELETE", "/v1/accounts/alice", false},
		{"PUT", "/v1/accounts/alice/block", false},
		{"POST", "/v1/accounts/alice/verify", false},
		{"GET", "/v1/accounts/alice/../../internal", false},
		{"GET", "/readyz", false},
	} {
		request := httptest.NewRequest(row.method, "http://agent:9789"+row.path, nil)
		request.Header.Set("Authorization", "Bearer secret")
		response := httptest.NewRecorder()
		relay.ServeHTTP(response, request)
		if (response.Code == http.StatusOK) != row.allowed {
			t.Fatalf("%s %s answered %d", row.method, row.path, response.Code)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("upstream saw %v", seen)
	}
}

/*
================
TestGuardRefusesForeignHostsAndCrossSitePosts
================
*/
func TestGuardRefusesForeignHostsAndCrossSitePosts(t *testing.T) {
	handler := guard(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	for _, row := range []struct {
		method, host, origin, site string
		want                       int
	}{
		{"GET", "localhost:9090", "", "", 200},
		{"GET", "127.0.0.1:9090", "", "", 200},
		{"GET", "[::1]:9090", "", "", 200},
		{"GET", "evil.example:9090", "", "", 403},
		{"GET", "10.0.0.5:9090", "", "", 403},
		{"POST", "localhost:9090", "http://localhost:9090", "same-origin", 200},
		{"POST", "localhost:9090", "", "", 200},
		{"POST", "localhost:9090", "https://evil.example", "", 403},
		{"POST", "localhost:9090", "", "cross-site", 403},
	} {
		request := httptest.NewRequest(row.method, "http://"+row.host+"/character", nil)
		request.Host = row.host
		if row.origin != "" {
			request.Header.Set("Origin", row.origin)
		}
		if row.site != "" {
			request.Header.Set("Sec-Fetch-Site", row.site)
		}
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != row.want {
			t.Fatalf("%+v answered %d", row, response.Code)
		}
	}
}

/*
================
testPanel

A panel wired to stand-ins for the GameWorld and the relay.
================
*/
func testPanel(t *testing.T, gameworld, accounts http.Handler) http.Handler {
	t.Helper()
	dir := t.TempDir()
	for name, token := range map[string]string{"operator-token": "op-token\n", "provisioning-token": "prov-token"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
	}
	gw, ac := httptest.NewServer(gameworld), httptest.NewServer(accounts)
	t.Cleanup(gw.Close)
	t.Cleanup(ac.Close)
	p, err := newPanel(panelConfig{OperatorURL: gw.URL, OperatorTokenFile: filepath.Join(dir, "operator-token"),
		AccountsURL: ac.URL, ProvisioningTokenFile: filepath.Join(dir, "provisioning-token"), OperatorName: "staff"})
	if err != nil {
		t.Fatal(err)
	}
	return p.Handler()
}

/*
================
panelRequest
================
*/
func panelRequest(handler http.Handler, method, target string, form url.Values) *httptest.ResponseRecorder {
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	request := httptest.NewRequest(method, "http://localhost:9090"+target, body)
	if form != nil {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}

/*
================
TestPanelReadsAndOperatesOnACharacter
================
*/
func TestPanelReadsAndOperatesOnACharacter(t *testing.T) {
	var posted []map[string]any
	gameworld := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/operations/player" || r.Header.Get("X-SRO-Local-Diagnostics") != "1" ||
			r.Header.Get("Authorization") != "Bearer op-token" || r.Header.Get("Origin") != "" {
			http.Error(w, "operator authentication required", http.StatusForbidden)
			return
		}
		if r.Method == http.MethodPost {
			var operation map[string]any
			if err := json.NewDecoder(r.Body).Decode(&operation); err != nil {
				t.Fatal(err)
			}
			posted = append(posted, operation)
			if operation["action"] == "set-level" {
				http.Error(w, "level must be between 1 and 90", http.StatusConflict)
				return
			}
		}
		w.Write([]byte(`{"player":{"name":"Probe","level":42,"skillPoints":1234},"towns":[{"id":7,"code":"JANGAN"}]}`))
	})
	panel := testPanel(t, gameworld, http.NotFoundHandler())

	page := panelRequest(panel, "GET", "/character?name=Probe", nil).Body.String()
	for _, want := range []string{"Probe", ">42<", "1234", `value="7">JANGAN`} {
		if !strings.Contains(page, want) {
			t.Fatalf("character page lacks %q", want)
		}
	}

	response := panelRequest(panel, "POST", "/character", url.Values{"name": {"Probe"}, "action": {"set-sp"},
		"skillPoints": {"0"}, "reason": {"support ticket"}})
	if response.Code != http.StatusSeeOther || !strings.Contains(response.Header().Get("Location"), "ok=") {
		t.Fatalf("set-sp answered %d %s", response.Code, response.Header().Get("Location"))
	}
	operation := posted[0]
	if operation["character"] != "Probe" || operation["operator"] != "staff" || operation["skillPoints"] != float64(0) ||
		!regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`).MatchString(operation["id"].(string)) {
		t.Fatalf("set-sp sent %v", operation)
	}

	response = panelRequest(panel, "POST", "/character", url.Values{"name": {"Probe"}, "action": {"set-level"},
		"level": {"120"}, "reason": {"support ticket"}})
	if location := response.Header().Get("Location"); !strings.Contains(location, "level+must+be+between") {
		t.Fatalf("the GameWorld's refusal was lost: %s", location)
	}
	if posted[1]["level"] != float64(120) || posted[1]["id"] == operation["id"] {
		t.Fatalf("set-level sent %v", posted[1])
	}

	response = panelRequest(panel, "POST", "/character", url.Values{"name": {"Probe"}, "action": {"set-level"},
		"level": {"lots"}, "reason": {"support ticket"}})
	if !strings.Contains(response.Header().Get("Location"), "err=") || len(posted) != 2 {
		t.Fatal("a malformed number reached the GameWorld")
	}
}

/*
================
TestPanelCreatesAccounts
================
*/
func TestPanelCreatesAccounts(t *testing.T) {
	var created []map[string]any
	accounts := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer prov-token" || r.Method != "POST" || r.URL.Path != "/v1/accounts" {
			http.Error(w, "unexpected", http.StatusTeapot)
			return
		}
		var document map[string]any
		json.NewDecoder(r.Body).Decode(&document)
		created = append(created, document)
		w.WriteHeader(http.StatusCreated)
	})
	panel := testPanel(t, http.NotFoundHandler(), accounts)

	response := panelRequest(panel, "POST", "/accounts", url.Values{"action": {"create"}, "id": {"alice"},
		"password": {"s3cret pass"}, "confirm": {"different"}})
	if !strings.Contains(response.Header().Get("Location"), "err=") || len(created) != 0 {
		t.Fatal("mismatched passwords created an account")
	}
	response = panelRequest(panel, "POST", "/accounts", url.Values{"action": {"create"}, "id": {"alice"},
		"password": {"s3cret pass"}, "confirm": {"s3cret pass"}})
	if !strings.Contains(response.Header().Get("Location"), "ok=") || len(created) != 1 ||
		created[0]["id"] != "alice" || created[0]["password"] != "s3cret pass" {
		t.Fatalf("create answered %s, sent %v", response.Header().Get("Location"), created)
	}
	if strings.Contains(response.Header().Get("Location"), "s3cret") {
		t.Fatal("the password leaked into the redirect")
	}
}
