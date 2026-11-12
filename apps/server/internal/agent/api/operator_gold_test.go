/*
===========================================================================

operator_gold_test.go - the adjust-gold operation is authenticated,
bounded, audited and dispatched only to its own callback

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
TestPlayerOperationsAdjustGoldAuthBoundsAndAudit
================
*/
func TestPlayerOperationsAdjustGoldAuthBoundsAndAudit(t *testing.T) {
	audit := filepath.Join(t.TempDir(), "audit.jsonl")
	calls := 0
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
		Read:   func(string) (any, error) { return map[string]any{"gold": 5000}, nil },
		Rescue: func(PlayerOperation) (any, error) { t.Fatal("gold adjustment dispatched to rescue"); return nil, nil },
		AdjustGold: func(request PlayerOperation) (any, error) {
			if request.Gold != -1500 || request.Character != "Tester" {
				t.Fatalf("adjust %+v", request)
			}
			calls++
			return map[string]any{"gold": 3500}, nil
		},
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	operation := PlayerOperation{ID: "unique-adjust-gold-1234", Operator: "operator", Character: "Tester",
		Reason: "Exploit correction: duplicated gold removed", Action: "adjust-gold", Gold: -1500}
	send := func(token string, input PlayerOperation) int {
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "http://127.0.0.1:8791/internal/operations/player", strings.NewReader(string(data)))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
		if token != "" {
			request.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		api.operator.ServeHTTP(response, request)
		return response.Code
	}
	if send("", operation) != 403 || send("wrong", operation) != 403 || calls != 0 {
		t.Fatal("an unauthenticated gold adjustment was admitted")
	}
	zero := operation
	zero.Gold = 0
	if send(config.Token, zero) != 400 || calls != 0 {
		t.Fatal("a zero gold adjustment was admitted")
	}
	rescue := operation
	rescue.Action = "rescue"
	if send(config.Token, rescue) != 400 {
		t.Fatal("a rescue carrying gold was admitted")
	}
	withSilk := operation
	withSilk.Silk = 5
	if send(config.Token, withSilk) != 400 {
		t.Fatal("a gold adjustment carrying silk was admitted")
	}
	if got := send(config.Token, operation); got != 200 || calls != 1 {
		t.Fatalf("adjust %d / calls %d", got, calls)
	}
	data, err := os.ReadFile(audit)
	if err != nil || !strings.Contains(string(data), `"action":"adjust-gold"`) || !strings.Contains(string(data), `"gold":-1500`) ||
		!strings.Contains(string(data), `"phase":"complete"`) {
		t.Fatal("gold adjustment audit missing", err)
	}
	if send(config.Token, operation) != 409 || calls != 1 {
		t.Fatal("a replayed gold adjustment ran twice")
	}
}
