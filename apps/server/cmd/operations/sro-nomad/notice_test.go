/*
===========================================================================

notice_test.go - exercise the signed local operator request over HTTP

===========================================================================
*/
package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	agentapi "opensro.online/server/internal/agent/api"
	"opensro.online/server/internal/security/auth"
)

/*
================
TestNoticeEndpointBoundary
================
*/
func TestNoticeEndpointBoundary(t *testing.T) {
	for _, address := range []string{"http://127.0.0.1:8791", "http://[::1]:8791/"} {
		if _, err := noticeEndpoint(address); err != nil {
			t.Fatalf("local endpoint %s: %v", address, err)
		}
	}
	for _, address := range []string{"https://127.0.0.1:8791", "http://example.com", "http://192.0.2.1", "http://user@127.0.0.1", "http://127.0.0.1/path", "http://127.0.0.1/?query=x", "http://127.0.0.1/#fragment"} {
		if _, err := noticeEndpoint(address); err == nil {
			t.Fatalf("unsafe endpoint accepted: %s", address)
		}
	}
}

/*
================
TestRunNoticeSignedRequestAndRedirectRefusal
================
*/
func TestRunNoticeSignedRequestAndRedirectRefusal(t *testing.T) {
	directory := t.TempDir()
	private, err := auth.GenerateAgentSessionKeyRing(time.Now())
	if err != nil {
		t.Fatal(err)
	}
	public, err := auth.PublicAgentSessionKeyRing(private)
	if err != nil {
		t.Fatal(err)
	}
	privatePath := filepath.Join(directory, auth.AgentSessionPrivateKeyRingFile)
	publicPath := filepath.Join(directory, "public.json")
	for path, data := range map[string][]byte{privatePath: private, publicPath: public} {
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	verifier, err := auth.NewAgentSessionVerifier(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range []int{http.StatusOK, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			requests := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests++
				if r.Method != http.MethodPost || r.URL.Path != agentapi.NoticePath || r.Header.Get("X-SRO-Local-Diagnostics") != "1" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
				}
				body, err := io.ReadAll(r.Body)
				if err != nil {
					t.Error(err)
				}
				claims, err := verifier.VerifyNotice(string(body), time.Now())
				if err != nil || claims.Message != "Restart in five minutes." || claims.ShardID != "global-official" {
					t.Errorf("signed notice = %+v, %v", claims, err)
				}
				w.Header().Set("Location", "/redirected")
				w.WriteHeader(status)
			}))
			defer server.Close()
			data, err := os.ReadFile(filepath.Join("..", "..", "..", "config", "shards.json"))
			if err != nil {
				t.Fatal(err)
			}
			var catalog struct {
				Shards []map[string]any `json:"shards"`
			}
			if err := json.Unmarshal(data, &catalog); err != nil {
				t.Fatal(err)
			}
			catalog.Shards[0]["controlUrl"] = server.URL
			data, err = json.Marshal(catalog)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(directory, "shards.json")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			err = runNotice(t.Context(), []string{"-state-dir", directory, "-catalog", path, "-message", "Restart in five minutes."})
			if (err == nil) != (status == http.StatusOK) {
				t.Fatalf("HTTP %d: %v", status, err)
			}
			server.Close()
			if requests != 1 {
				t.Fatalf("redirect followed: %d requests", requests)
			}
		})
	}
}
