/*
===========================================================================

notice_test.go - operator authorization, browser isolation and retry behavior

===========================================================================
*/
package agentapi

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opensro.online/server/internal/security/auth"
)

/*
================
TestNoticeAuthorizationAndRetries
================
*/
func TestNoticeAuthorizationAndRetries(t *testing.T) {
	api, _ := newTestAPI(t)
	private, err := auth.GenerateAgentSessionKeyRing(api.now())
	if err != nil {
		t.Fatal(err)
	}
	public, err := auth.PublicAgentSessionKeyRing(private)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	privatePath, publicPath := filepath.Join(directory, "private.json"), filepath.Join(directory, "public.json")
	if err := os.WriteFile(privatePath, private, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(publicPath, public, 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := auth.NewAgentSessionSigner(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	api.agentSessionVerifier, err = auth.NewAgentSessionVerifier(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.MintNotice(testDivision, "Maintenance in 5 minutes.", api.now())
	if err != nil {
		t.Fatal(err)
	}
	wrongShard, err := signer.MintNotice("different-shard", "Maintenance", api.now())
	if err != nil {
		t.Fatal(err)
	}
	var delivered []string
	api.InstallNoticePublisher(func(message string) { delivered = append(delivered, message) })
	handler := api.Handler()
	for _, scenario := range []struct {
		name, token, remote, origin, forwarded, method string
		status                                         int
	}{
		{"accepted", token, "127.0.0.1:2345", "", "", http.MethodPost, 200},
		{"retry", token, "127.0.0.1:2345", "", "", http.MethodPost, 200},
		{"remote", token, "198.51.100.2:2345", "", "", http.MethodPost, 403},
		{"browser", token, "127.0.0.1:2345", "https://invalid.example", "", http.MethodPost, 403},
		{"proxy", token, "127.0.0.1:2345", "", "127.0.0.1", http.MethodPost, 403},
		{"forged", "invalid", "127.0.0.1:2345", "", "", http.MethodPost, 401},
		{"wrong shard", wrongShard, "127.0.0.1:2345", "", "", http.MethodPost, 401},
		{"method", token, "127.0.0.1:2345", "", "", http.MethodGet, 405},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			request := httptest.NewRequest(scenario.method, "http://127.0.0.1:8791"+NoticePath, strings.NewReader(scenario.token))
			request.RemoteAddr = scenario.remote
			request.Header.Set("X-SRO-Local-Diagnostics", "1")
			request.Header.Set("Origin", scenario.origin)
			request.Header.Set("X-Forwarded-For", scenario.forwarded)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != scenario.status {
				t.Fatalf("status = %d; want %d: %s", response.Code, scenario.status, response.Body.String())
			}
		})
	}
	if len(delivered) != 1 || delivered[0] != "Maintenance in 5 minutes." {
		t.Fatalf("delivered: %v", delivered)
	}
}
