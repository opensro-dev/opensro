/*
===========================================================================

operator_conduct_test.go - mute, unmute and kick: clause required, audited

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
TestConductActionsNeedAClauseAndReachTheirHook

#2671: every conduct action cites a rule clause; a mute carries its
minutes inside the bound; each action reaches its own hook only after the
intent audit; non-conduct actions refuse a clause.
================
*/
func TestConductActionsNeedAClauseAndReachTheirHook(t *testing.T) {
	audit := filepath.Join(t.TempDir(), "audit.jsonl")
	called := map[string]PlayerOperation{}
	hook := func(name string) func(PlayerOperation) (any, error) {
		return func(request PlayerOperation) (any, error) {
			data, err := os.ReadFile(audit)
			if err != nil || !strings.Contains(string(data), `"action":"`+name+`"`) || !strings.Contains(string(data), `"clause":"C2"`) {
				t.Fatalf("%s ran before its audited intent: %v", name, err)
			}
			called[name] = request
			return map[string]any{"ok": true}, nil
		}
	}
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
		Read:       func(string) (any, error) { return map[string]any{"bound": true}, nil },
		Rescue:     func(PlayerOperation) (any, error) { t.Fatal("conduct dispatched to rescue"); return nil, nil },
		ClearPK:    func(PlayerOperation) (any, error) { t.Fatal("conduct dispatched to clear-pk"); return nil, nil },
		MuteChat:   hook("mute-chat"),
		UnmuteChat: hook("unmute-chat"),
		Kick:       hook("kick"),
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	n := 0
	send := func(input PlayerOperation) int {
		n++
		input.ID = "conduct-request-" + strings.Repeat("x", n)
		input.Operator, input.Character = "staff", "Loud"
		if input.Reason == "" {
			input.Reason = "slur in all chat"
		}
		data, _ := json.Marshal(input)
		request := httptest.NewRequest("POST", "http://127.0.0.1:8791/internal/operations/player", strings.NewReader(string(data)))
		request.RemoteAddr = "127.0.0.1:1234"
		request.Header.Set("X-SRO-Local-Diagnostics", "1")
		request.Header.Set("Authorization", "Bearer "+config.Token)
		response := httptest.NewRecorder()
		api.operator.ServeHTTP(response, request)
		return response.Code
	}
	for _, bad := range []PlayerOperation{
		{Action: "mute-chat", Minutes: 60},                         // no clause
		{Action: "mute-chat", Minutes: 60, Clause: "C 2"},          // bad clause
		{Action: "mute-chat", Clause: "C2"},                        // no minutes
		{Action: "mute-chat", Clause: "C2", Minutes: 30*24*60 + 1}, // past 30 days
		{Action: "mute-chat", Clause: "C2", Minutes: 60, Silk: 5},
		{Action: "kick"},
		{Action: "kick", Clause: "C2", Minutes: 5},
		{Action: "unmute-chat", Clause: "C2", Town: 2},
		{Action: "clear-pk", Clause: "C2"}, // a clause on a non-conduct action
		{Action: "mute-chat", Clause: "C2", Minutes: 60, Reason: "bad"},
	} {
		if code := send(bad); code != 400 {
			t.Fatalf("%+v admitted with %d", bad, code)
		}
	}
	if len(called) != 0 {
		t.Fatal("a refused request reached a hook")
	}
	for _, good := range []PlayerOperation{
		{Action: "mute-chat", Clause: "C2", Minutes: 24 * 60},
		{Action: "unmute-chat", Clause: "C2"},
		{Action: "kick", Clause: "C2"},
	} {
		if code := send(good); code != 200 {
			t.Fatalf("%s refused with %d", good.Action, code)
		}
	}
	if called["mute-chat"].Minutes != 24*60 || called["kick"].Clause != "C2" || len(called) != 3 {
		t.Fatalf("hooks saw %+v", called)
	}
	api.operator.(*operatorEndpoint).config.Kick = nil
	if send(PlayerOperation{Action: "kick", Clause: "C2"}) != 400 {
		t.Fatal("kick admitted without a hook")
	}
}
