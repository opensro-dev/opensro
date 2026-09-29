package agentapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFollowFixtureGateAuthenticationAndOwnership(t *testing.T) {
	api, _ := newTestAPI(t)
	calls := 0
	control := func(division, name, command string) (any, error) {
		calls++
		if division != testDivision || name != "FixtureHero" || command != "status" {
			t.Fatal("wrong authenticated fixture identity")
		}
		return map[string]any{"phase": "test"}, nil
	}
	post := func(h http.Handler) int {
		r := httptest.NewRequest("POST", FollowFixturePath, strings.NewReader(`{"characterName":"FixtureHero","command":"status"}`))
		declareBrowser(r)
		r.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	api.InstallFollowFixture(control)
	if got := post(authenticatedHandler(t, api, testAccount)); got != 404 {
		t.Fatalf("disabled route=%d", got)
	}
	api.benchmarkFixtureControl = true
	api.InstallFollowFixture(control)
	if got := post(api.Handler()); got != 401 {
		t.Fatalf("unauthenticated route=%d", got)
	}
	h := authenticatedHandler(t, api, testAccount)
	postJSON(t, h, "/character/create", createBody("FixtureHero"))
	if got := post(authenticatedHandler(t, api, "foreign-account")); got != 404 {
		t.Fatalf("foreign route=%d", got)
	}
	if calls != 0 {
		t.Fatal("unauthorized fixture executed")
	}
	if got := post(h); got != 200 || calls != 1 {
		t.Fatalf("owned fixture=%d calls=%d", got, calls)
	}
}
