/*
===========================================================================

operator_progress_test.go - the level and SP corrections are validated,
audited and dispatched only to their own callbacks

===========================================================================
*/
package agentapi

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestPlayerOperationsSetLevelAndSkillPoints
================
*/
func TestPlayerOperationsSetLevelAndSkillPoints(t *testing.T) {
	levels, points := []uint32{}, []uint32{}
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: filepath.Join(t.TempDir(), "audit.jsonl"),
		Read:   func(string) (any, error) { return map[string]any{}, nil },
		Rescue: func(PlayerOperation) (any, error) { t.Fatal("dispatched to rescue"); return nil, nil },
		SetLevel: func(request PlayerOperation) (any, error) {
			levels = append(levels, request.Level)
			return map[string]any{}, nil
		},
		SetSkillPoints: func(request PlayerOperation) (any, error) {
			points = append(points, *request.SkillPoints)
			return map[string]any{}, nil
		},
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	next := 0
	send := func(input PlayerOperation) int {
		next++
		input.ID = "unique-progress-" + strings.Repeat("x", next)
		input.Operator, input.Character, input.Reason = "operator", "Probe", "Player support ticket"
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest("POST", "http://127.0.0.1:8791/internal/operations/player", strings.NewReader(string(data)))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
		request.Header.Set("Authorization", "Bearer "+config.Token)
		response := httptest.NewRecorder()
		api.operator.ServeHTTP(response, request)
		return response.Code
	}
	zero, many, tooMany := uint32(0), uint32(5000), uint32(operatorMaxSkillPoints+1)
	for _, invalid := range []PlayerOperation{
		{Action: "set-level"},
		{Action: "set-level", Level: operatorMaxLevel + 1},
		{Action: "set-level", Level: 50, Town: 2},
		{Action: "set-level", Level: 50, SkillPoints: &many},
		{Action: "set-sp"},
		{Action: "set-sp", SkillPoints: &tooMany},
		{Action: "set-sp", SkillPoints: &many, Level: 50},
		{Action: "reset-stats", Level: 50},
		{Action: "clear-pk", SkillPoints: &zero},
	} {
		if got := send(invalid); got != 400 {
			t.Fatalf("%+v admitted with %d", invalid, got)
		}
	}
	if send(PlayerOperation{Action: "set-level", Level: 80}) != 200 || send(PlayerOperation{Action: "set-sp", SkillPoints: &zero}) != 200 ||
		send(PlayerOperation{Action: "set-sp", SkillPoints: &many}) != 200 {
		t.Fatal("valid correction refused")
	}
	if len(levels) != 1 || levels[0] != 80 || len(points) != 2 || points[0] != 0 || points[1] != 5000 {
		t.Fatalf("dispatched levels %v points %v", levels, points)
	}
	api.operator.(*operatorEndpoint).config.SetLevel = nil
	if send(PlayerOperation{Action: "set-level", Level: 80}) != 400 {
		t.Fatal("a shard without the callback admitted a level correction")
	}
}
