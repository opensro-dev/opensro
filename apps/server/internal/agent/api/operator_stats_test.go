/*
===========================================================================

operator_stats_test.go - the stat reset operation is authenticated, audited
and dispatched only to its own callback

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
TestPlayerOperationsResetStatsAuditAndReplay
================
*/
func TestPlayerOperationsResetStatsAuditAndReplay(t *testing.T) {
	audit := filepath.Join(t.TempDir(), "audit.jsonl")
	calls := 0
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
		Read:    func(string) (any, error) { return map[string]any{"strength": 160}, nil },
		Rescue:  func(PlayerOperation) (any, error) { t.Fatal("stat reset dispatched to rescue"); return nil, nil },
		ClearPK: func(PlayerOperation) (any, error) { t.Fatal("stat reset dispatched to PK clear"); return nil, nil },
		ResetStats: func(request PlayerOperation) (any, error) {
			data, err := os.ReadFile(audit)
			if err != nil || !strings.Contains(string(data), `"phase":"intent"`) ||
				!strings.Contains(string(data), `"action":"reset-stats"`) || !strings.Contains(string(data), `"strength":160`) {
				t.Fatal("stat reset preceded its audited intent", err)
			}
			calls++
			return map[string]any{"strength": 109}, nil
		},
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	operation := PlayerOperation{ID: "unique-reset-stats-1234", Operator: "operator", Character: "Probe",
		Reason: "Player asked for a stat reset", Action: "reset-stats"}
	send := func(token string, input PlayerOperation) int {
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "http://127.0.0.1:8791/internal/operations/player", strings.NewReader(string(data)))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		api.operator.ServeHTTP(response, request)
		return response.Code
	}
	if send("wrong", operation) != 403 || calls != 0 {
		t.Fatal("stat reset authentication bypass")
	}
	for _, invalid := range []PlayerOperation{{Town: 2}, {Items: []OperatorItemGrant{{Codename: "ITEM", Count: 1}}}} {
		invalid.ID, invalid.Operator, invalid.Character, invalid.Reason, invalid.Action = operation.ID, operation.Operator, operation.Character, operation.Reason, operation.Action
		if send(config.Token, invalid) != 400 || calls != 0 {
			t.Fatal("invalid stat reset admitted")
		}
	}
	api.operator.(*operatorEndpoint).config.ResetStats = nil
	if send(config.Token, operation) != 400 || calls != 0 {
		t.Fatal("a shard without the callback admitted a stat reset")
	}
	api.operator.(*operatorEndpoint).config.ResetStats = config.ResetStats
	if got := send(config.Token, operation); got != 200 || calls != 1 {
		t.Fatalf("reset %d / calls %d", got, calls)
	}
	if data, err := os.ReadFile(audit); err != nil || !strings.Contains(string(data), `"phase":"complete"`) {
		t.Fatal("missing outcome audit", err)
	}
	if send(config.Token, operation) != 409 || calls != 1 {
		t.Fatal("stat reset replayed")
	}
}
