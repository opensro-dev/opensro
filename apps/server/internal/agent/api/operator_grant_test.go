/*
===========================================================================

operator_grant_test.go - grants use the authenticated, durable operation lane

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
TestPlayerOperationsGrantAuthenticationAuditAndReplay
================
*/
func TestPlayerOperationsGrantAuthenticationAuditAndReplay(t *testing.T) {
	audit := filepath.Join(t.TempDir(), "audit.jsonl")
	calls := 0
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
		Read:   func(string) (any, error) { return map[string]any{"name": "Probe"}, nil },
		Rescue: func(PlayerOperation) (any, error) { t.Fatal("grant dispatched to rescue"); return nil, nil },
		GrantItems: func(request PlayerOperation) (any, error) {
			data, err := os.ReadFile(audit)
			if err != nil || !strings.Contains(string(data), request.ID) || !strings.Contains(string(data), "ITEM_CH_BOW_02_A_RARE") {
				t.Fatal("grant preceded durable item intent")
			}
			calls++
			return map[string]bool{"ok": true}, nil
		},
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	operation := PlayerOperation{ID: "unique-grant-request-1234", Operator: "operator", Character: "Probe", Reason: "Inspect seal visuals",
		Action: "grant-items", Items: []OperatorItemGrant{{Codename: "ITEM_CH_BOW_02_A_RARE", Count: 1}}}
	send := func(token, origin string, operation PlayerOperation) int {
		body, err := json.Marshal(operation)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "http://127.0.0.1:8791/internal/operations/player", strings.NewReader(string(body)))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
		request.Header.Set("Authorization", "Bearer "+token)
		request.Header.Set("Origin", origin)
		response := httptest.NewRecorder()
		api.operator.ServeHTTP(response, request)
		return response.Code
	}
	if send("wrong", "", operation) != 403 || send(config.Token, "https://evil.invalid", operation) != 403 || calls != 0 {
		t.Fatal("grant authentication bypass")
	}
	for _, invalid := range []PlayerOperation{
		{Action: "unknown"}, {Action: "grant-items"}, {Action: "grant-items", Town: 2, Items: operation.Items},
		{Action: "rescue", Items: operation.Items},
		{Action: "grant-items", Items: []OperatorItemGrant{{Codename: "ITEM", Count: 0}}},
	} {
		invalid.ID, invalid.Operator, invalid.Character, invalid.Reason = operation.ID, operation.Operator, operation.Character, operation.Reason
		if send(config.Token, "", invalid) != 400 || calls != 0 {
			t.Fatal("invalid operation admitted")
		}
	}
	if code := send(config.Token, "", operation); code != 200 || calls != 1 {
		t.Fatalf("grant %d / %d", code, calls)
	}
	if send(config.Token, "", operation) != 409 || calls != 1 {
		t.Fatal("grant replayed")
	}
	api = &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	if send(config.Token, "", operation) != 409 || calls != 1 {
		t.Fatal("grant replayed after restart")
	}
}
