/*
===========================================================================

operator_silk_test.go - the grant-silk operation is authenticated, bounded,
audited and dispatched only to its own callback

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
TestPlayerOperationsGrantSilkAuthBoundsAndAudit
================
*/
func TestPlayerOperationsGrantSilkAuthBoundsAndAudit(t *testing.T) {
	audit := filepath.Join(t.TempDir(), "audit.jsonl")
	calls := 0
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
		Read:   func(string) (any, error) { return map[string]any{"silk": 0}, nil },
		Rescue: func(PlayerOperation) (any, error) { t.Fatal("silk grant dispatched to rescue"); return nil, nil },
		GrantSilk: func(request PlayerOperation) (any, error) {
			if request.Silk != 100000 || request.Character != "Tester" {
				t.Fatalf("grant %+v", request)
			}
			calls++
			return map[string]any{"silk": 100000}, nil
		},
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	operation := PlayerOperation{ID: "unique-grant-silk-1234", Operator: "operator", Character: "Tester",
		Reason: "Tester asked for silk to keep testing", Action: "grant-silk", Silk: 100000}
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
		t.Fatal("an unauthenticated silk grant was admitted")
	}
	for _, silk := range []uint32{0, operatorMaxSilkGrant + 1} {
		invalid := operation
		invalid.Silk = silk
		if send(config.Token, invalid) != 400 || calls != 0 {
			t.Fatalf("silk %d admitted", silk)
		}
	}
	rescue := operation
	rescue.Action = "rescue"
	if send(config.Token, rescue) != 400 {
		t.Fatal("a rescue carrying silk was admitted")
	}
	if got := send(config.Token, operation); got != 200 || calls != 1 {
		t.Fatalf("grant %d / calls %d", got, calls)
	}
	data, err := os.ReadFile(audit)
	if err != nil || !strings.Contains(string(data), `"action":"grant-silk"`) || !strings.Contains(string(data), `"silk":100000`) ||
		!strings.Contains(string(data), `"phase":"complete"`) {
		t.Fatal("silk grant audit missing", err)
	}
	if send(config.Token, operation) != 409 || calls != 1 {
		t.Fatal("a replayed silk grant ran twice")
	}
}
