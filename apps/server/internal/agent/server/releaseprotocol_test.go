/*
===========================================================================

releaseprotocol_test.go - the title and proxied routes refuse another release

===========================================================================
*/
package agentserver

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/releaseprotocol"
)

/*
================
TestBrowserRoutesRefuseAnotherReleaseProtocol

Every browser route answers 426 before acting when the declaration is
missing or another protocol; cluster-internal routes do not ask for one.
================
*/
func TestBrowserRoutesRefuseAnotherReleaseProtocol(t *testing.T) {
	f := newAgentFixture(t, http.NotFoundHandler(), http.NotFoundHandler())
	routes := []struct{ method, path string }{
		{http.MethodGet, "/title/servers"},
		{http.MethodGet, onboardingPath},
		{http.MethodPost, "/title/login"},
		{http.MethodPost, "/title/session"},
		{http.MethodPost, "/title/logout"},
		{http.MethodPost, "/title/character-select"},
		{http.MethodGet, "/character/list"},
		{http.MethodPost, "/character/create"},
		{http.MethodPost, "/agent/packet"},
		{http.MethodPost, "/auth/enterworld-token"},
		{http.MethodPost, "/auth/transport-token"},
	}
	for _, route := range routes {
		for _, declared := range []string{"", strconv.Itoa(releaseprotocol.Current - 1)} {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			request.Header.Set("Content-Type", "application/json")
			if declared != "" {
				request.Header.Set(releaseprotocol.Header, declared)
			}
			recorder := httptest.NewRecorder()
			f.handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUpgradeRequired {
				t.Fatalf("%s %s declaring %q = %d, want 426", route.method, route.path, declared, recorder.Code)
			}
		}
	}
	internal := httptest.NewRequest(http.MethodGet, "/internal/accounts", nil)
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, internal)
	if recorder.Code == http.StatusUpgradeRequired {
		t.Fatal("a cluster-internal route asked for a browser release protocol")
	}
}

/*
================
TestPreflightAllowsTheProtocolHeader

A cross-origin browser may send the declaration only if the preflight
allows it; otherwise every request fails as a CORS error instead of 426.
================
*/
func TestPreflightAllowsTheProtocolHeader(t *testing.T) {
	server := &Server{allowedOrigins: map[string]bool{"https://game.example": true}}
	handler := server.cors(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	request := httptest.NewRequest(http.MethodOptions, "/title/login", nil)
	request.Header.Set("Origin", "https://game.example")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	allowed := recorder.Header().Get("Access-Control-Allow-Headers")
	if !strings.Contains(allowed, releaseprotocol.Header) {
		t.Fatalf("preflight = %d allowing %q, want %s allowed", recorder.Code, allowed, releaseprotocol.Header)
	}
}
