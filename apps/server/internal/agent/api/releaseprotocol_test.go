/*
===========================================================================

releaseprotocol_test.go - browser routes refuse a mismatched release

A browser built for another release protocol must be told to update before
any route acts, even with a valid session: the answer it would get is a
contract it cannot read.

===========================================================================
*/
package agentapi

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"opensro.online/server/internal/releaseprotocol"
	"opensro.online/server/internal/security/auth"
)

/*
================
TestBrowserRoutesRefuseAnotherReleaseProtocol
================
*/
func TestBrowserRoutesRefuseAnotherReleaseProtocol(t *testing.T) {
	api, _ := newTestAPI(t)
	token, err := auth.MintAgentSession(testAgentKeyID, testAgentPrivateKey, testAccount, testDivision,
		api.now().Add(auth.AgentSessionLifetime))
	if err != nil {
		t.Fatal(err)
	}
	routes := []struct{ method, path string }{
		{http.MethodGet, "/character/list"},
		{http.MethodPost, "/character/name-overlap"},
		{http.MethodPost, "/character/create"},
		{http.MethodPost, "/character/delete-action"},
		{http.MethodPost, "/character/enter-area"},
		{http.MethodPost, "/character/leave-area"},
		{http.MethodPost, "/agent/packet"},
		{http.MethodPost, "/auth/enterworld-token"},
		{http.MethodPost, "/auth/transport-token"},
	}
	for _, route := range routes {
		for _, declared := range []string{"", strconv.Itoa(releaseprotocol.Oldest - 1), strconv.Itoa(releaseprotocol.Current + 1)} {
			request := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			request.Header.Set("Authorization", "Bearer "+token)
			if declared != "" {
				request.Header.Set(releaseprotocol.Header, declared)
			}
			recorder := httptest.NewRecorder()
			api.Handler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusUpgradeRequired {
				t.Fatalf("%s %s declaring %q = %d, want 426", route.method, route.path, declared, recorder.Code)
			}
		}
	}
}
