/*
===========================================================================

operator_pk_test.go - authenticated PK recovery, durable audit and replay

===========================================================================
*/
package agentapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestPlayerOperationsClearPKAuditAndReplay
================
*/
func TestPlayerOperationsClearPKAuditAndReplay(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			audit := filepath.Join(t.TempDir(), "audit.jsonl")
			calls := 0
			config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
				Read:   func(string) (any, error) { return map[string]any{"penalty": 1200}, nil },
				Rescue: func(PlayerOperation) (any, error) { t.Fatal("PK clear dispatched to rescue"); return nil, nil },
				ClearPK: func(request PlayerOperation) (any, error) {
					data, err := os.ReadFile(audit)
					if err != nil || !strings.Contains(string(data), `"phase":"intent"`) ||
						!strings.Contains(string(data), `"action":"clear-pk"`) || !strings.Contains(string(data), `"penalty":1200`) {
						t.Fatal("PK clear preceded audited intent", err)
					}
					calls++
					if fail {
						return nil, fmt.Errorf("persistence failed")
					}
					return map[string]any{"penalty": 0}, nil
				},
			}
			api := &API{}
			if err := api.InstallPlayerOperations(config); err != nil {
				t.Fatal(err)
			}
			operation := PlayerOperation{ID: "unique-clear-pk-1234", Operator: "operator", Character: "Probe", Reason: "Clear active PK", Action: "clear-pk"}
			send := func(token, origin string, input PlayerOperation) int {
				data, err := json.Marshal(input)
				if err != nil {
					t.Fatal(err)
				}
				request := httptest.NewRequest("POST", "http://127.0.0.1:8791/internal/operations/player", strings.NewReader(string(data)))
				request.RemoteAddr = "127.0.0.1:1234"
				request.Header.Set("X-SRO-Local-Diagnostics", "1")
				request.Header.Set("Authorization", "Bearer "+token)
				request.Header.Set("Origin", origin)
				response := httptest.NewRecorder()
				api.operator.ServeHTTP(response, request)
				return response.Code
			}
			if send("wrong", "", operation) != 403 || send(config.Token, "https://evil.invalid", operation) != 403 || calls != 0 {
				t.Fatal("PK clear authentication bypass")
			}
			for _, invalid := range []PlayerOperation{
				{Town: 2}, {Items: []OperatorItemGrant{{Codename: "ITEM", Count: 1}}},
			} {
				invalid.ID, invalid.Operator, invalid.Character, invalid.Reason, invalid.Action = operation.ID, operation.Operator, operation.Character, operation.Reason, operation.Action
				if send(config.Token, "", invalid) != 400 || calls != 0 {
					t.Fatal("invalid PK clear admitted")
				}
			}
			api.operator.(*operatorEndpoint).config.ClearPK = nil
			if send(config.Token, "", operation) != 400 {
				t.Fatal("missing callback admitted")
			}
			api.operator.(*operatorEndpoint).config.ClearPK = config.ClearPK
			want := 200
			if fail {
				want = 409
			}
			if got := send(config.Token, "", operation); got != want || calls != 1 {
				t.Fatalf("clear %d / calls %d", got, calls)
			}
			data, err := os.ReadFile(audit)
			if err != nil || !strings.Contains(string(data), `"phase":"complete"`) {
				t.Fatal("missing outcome audit", err)
			}
			if fail && !strings.Contains(string(data), "persistence failed") {
				t.Fatal("failure not audited")
			}
			if send(config.Token, "", operation) != 409 || calls != 1 {
				t.Fatal("PK clear replayed")
			}
			api = &API{}
			if err := api.InstallPlayerOperations(config); err != nil {
				t.Fatal(err)
			}
			if send(config.Token, "", operation) != 409 || calls != 1 {
				t.Fatal("PK clear replayed after restart")
			}
			if err := os.Remove(audit); err != nil {
				t.Fatal(err)
			}
			operation.ID = "another-clear-pk-1234"
			if send(config.Token, "", operation) != 503 || calls != 1 {
				t.Fatal("mutation admitted without intent audit")
			}
		})
	}
}
