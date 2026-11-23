/*
===========================================================================

operator_staff_test.go - staff grants through the operator endpoint: the
console role is required, make item needs an approver, every change is
audited

===========================================================================
*/
package agentapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opensro.online/server/internal/game/staff"
)

/*
================
TestStaffActionsReachTheBookWithTheConsoleRole

An operator who adds make item is refused 403 by the staff book itself
(not only the console's UI); an approver may; a staff action without a
console role, or a staff field on another action, is a bad request; a
revoke and a role edit reach their hooks; the list is served on GET.
================
*/
func TestStaffActionsReachTheBookWithTheConsoleRole(t *testing.T) {
	dir := t.TempDir()
	audit := filepath.Join(dir, "audit.jsonl")
	book, err := staff.OpenBook(filepath.Join(dir, staff.FileName), time.Now,
		func(path string, payload []byte) error { return os.WriteFile(path, payload, 0o600) })
	if err != nil {
		t.Fatal(err)
	}
	revoked, roles := 0, 0
	config := PlayerOperations{Token: strings.Repeat("k", 32), AuditPath: audit,
		Read:   func(string) (any, error) { return map[string]any{"bound": true}, nil },
		Rescue: func(PlayerOperation) (any, error) { t.Fatal("staff dispatched to rescue"); return nil, nil },
		Staff: &StaffOperations{
			Read: func() (any, error) { return map[string]any{"grants": book.Grants("d")}, nil },
			Grant: func(request PlayerOperation) (any, error) {
				grant, _, err := book.Grant(request.StaffRequest("d", request.Character))
				return grant, err
			},
			Revoke:  func(PlayerOperation) (any, error) { revoked++; return map[string]any{"ok": true}, nil },
			SetRole: func(PlayerOperation) (any, error) { roles++; return map[string]any{"ok": true}, nil },
		},
	}
	api := &API{}
	if err := api.InstallPlayerOperations(config); err != nil {
		t.Fatal(err)
	}
	n := 0
	send := func(input PlayerOperation) int {
		n++
		input.ID = "staff-request-" + strings.Repeat("x", n)
		input.Operator, input.Character, input.Reason = "staff-a", "Hundred", "new event GM"
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
		{Action: "staff-grant", Role: staff.GMRole},                                 // no console role
		{Action: "staff-grant", Role: staff.GMRole, ConsoleRole: "player"},          // not a console role
		{Action: "staff-grant", ConsoleRole: "operator"},                            // no role
		{Action: "staff-role", ConsoleRole: "operator"},                             // no role to edit
		{Action: "rescue", ConsoleRole: "operator"},                                 // console role elsewhere
		{Action: "rescue", Capabilities: []string{"ban"}},                           // staff field elsewhere
		{Action: "staff-revoke", ConsoleRole: "operator", Role: staff.GMRole},       // grant field on a revoke
		{Action: "staff-grant", ConsoleRole: "operator", Role: "gm", Silk: 5},       // foreign field
		{Action: "staff-revoke", ConsoleRole: "operator", StaffRole: &staff.Role{}}, // role on a revoke
	} {
		if code := send(bad); code != http.StatusBadRequest {
			t.Fatalf("%+v answered %d", bad, code)
		}
	}

	makeItem := PlayerOperation{Action: "staff-grant", Role: staff.GMRole, Capabilities: []string{"make_item", "teleport"}, ConsoleRole: "operator"}
	if code := send(makeItem); code != http.StatusForbidden {
		t.Fatalf("an operator adding make item answered %d", code)
	}
	if _, ok := book.Lookup("d", "Hundred"); ok {
		t.Fatal("the refused grant was stored")
	}
	makeItem.ConsoleRole = "approver"
	if code := send(makeItem); code != http.StatusOK {
		t.Fatalf("an approver adding make item answered %d", code)
	}
	if grant, ok := book.Lookup("d", "Hundred"); !ok || len(grant.Capabilities) != 2 {
		t.Fatalf("approved grant = %+v %v", grant, ok)
	}
	if code := send(PlayerOperation{Action: "staff-revoke", ConsoleRole: "operator"}); code != http.StatusOK || revoked != 1 {
		t.Fatalf("revoke answered %d, hook ran %d", code, revoked)
	}
	edit := PlayerOperation{Action: "staff-role", ConsoleRole: "operator",
		StaffRole: &staff.Role{ID: "mod", Tag: "[MOD]", NameColor: "#7AD8FF", ChatColor: "#C0F0FF"}}
	if code := send(edit); code != http.StatusOK || roles != 1 {
		t.Fatalf("role edit answered %d, hook ran %d", code, roles)
	}
	data, _ := os.ReadFile(audit)
	if !strings.Contains(string(data), `"consoleRole":"approver"`) || !strings.Contains(string(data), `"action":"staff-role"`) {
		t.Fatal("staff changes are missing from the audit")
	}

	request := httptest.NewRequest("GET", "http://127.0.0.1:8791/internal/operations/player?staff=1", nil)
	request.RemoteAddr = "127.0.0.1:1234"
	request.Header.Set("X-SRO-Local-Diagnostics", "1")
	request.Header.Set("Authorization", "Bearer "+config.Token)
	response := httptest.NewRecorder()
	api.operator.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"grants"`) {
		t.Fatalf("staff list = %d %s", response.Code, response.Body.String())
	}
}
