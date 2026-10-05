/*
===========================================================================
operator_test.go - authentication, audit durability and replay refusal
===========================================================================
*/
package agentapi

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestPlayerOperationsAuthenticateAndRecordBeforeRescue
================
*/
func TestPlayerOperationsAuthenticateAndRecordBeforeRescue(t *testing.T) {
	audit := filepath.Join(t.TempDir(), "audit.jsonl")
	calls := 0
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
		Read: func(string) (any, error) { return map[string]any{"name": "Viper"}, nil },
		Rescue: func(request PlayerOperation) (any, error) {
			bytes, err := os.ReadFile(audit)
			if err != nil || !strings.Contains(string(bytes), request.ID) {
				t.Fatal("rescue preceded durable intent")
			}
			calls++
			return map[string]bool{"ok": true}, nil
		},
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(PlayerOperation{ID: "unique-request-1234", Operator: "operator", Character: "Viper", Town: 2, Reason: "Stuck loading region"})
	send := func(token, origin string) int {
		request := httptest.NewRequest("POST", "http://127.0.0.1:8791/internal/operations/player", strings.NewReader(string(body)))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		api.operator.ServeHTTP(response, request)
		return response.Code
	}
	if send("wrong", "") != 403 || send(config.Token, "https://evil.invalid") != 403 || calls != 0 {
		t.Fatal("authentication bypass")
	}
	if code := send(config.Token, ""); code != 200 || calls != 1 {
		t.Fatalf("rescue %d / %d", code, calls)
	}
	if send(config.Token, "") != 409 || calls != 1 {
		t.Fatal("request replayed")
	}
	api = &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	if send(config.Token, "") != 409 || calls != 1 {
		t.Fatal("request replayed after restart")
	}
}
