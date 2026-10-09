/*
===========================================================================

maintenance_gate_test.go - a maintenance release keeps everyone but the
probe out of the world

===========================================================================
*/
package agentapi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

/*
================
TestMaintenanceGateAdmitsOnlyListedAccounts

No path or no file is an open shard; a file admits only its accounts
(case-insensitively); an unreadable, undecodable or oversized file closes
it to everyone.
================
*/
func TestMaintenanceGateAdmitsOnlyListedAccounts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "maintenance-gate.json")
	if !maintenanceAdmits("", "player") || !maintenanceAdmits(path, "player") {
		t.Fatal("no gate kept a player out")
	}
	if err := os.WriteFile(path, []byte(`{"accounts":["Probe"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if !maintenanceAdmits(path, "probe") || maintenanceAdmits(path, "player") || maintenanceAdmits(path, "") {
		t.Fatal("gate admitted the wrong accounts")
	}
	for name, body := range map[string]string{
		"malformed": `{"accounts":`,
		"empty":     `{"accounts":[""]}`,
		"oversized": `{"accounts":["probe"],"pad":"` + strings.Repeat("x", maxMaintenanceGateBytes) + `"}`,
	} {
		if err := os.WriteFile(path, []byte(body), 0o640); err != nil {
			t.Fatal(err)
		}
		if maintenanceAdmits(path, "probe") || maintenanceAdmits(path, "player") {
			t.Fatalf("%s gate admitted an account", name)
		}
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if !maintenanceAdmits(path, "player") {
		t.Fatal("a removed gate kept a player out")
	}
}

/*
================
TestEnterWorldTokenRefusedWhileGated

The token route answers MAINTENANCE to an account the gate does not list,
mints for one it does, and mints for everyone once the gate is removed.
================
*/
func TestEnterWorldTokenRefusedWhileGated(t *testing.T) {
	api, _ := newTestAPI(t)
	api.maintenanceGatePath = filepath.Join(t.TempDir(), "maintenance-gate.json")
	handler := authenticatedHandler(t, api, testAccount)
	postJSON(t, handler, "/character/create", createBody("GateHero"))
	mint := func() map[string]any {
		return postJSON(t, handler, "/auth/enterworld-token", map[string]any{"characterName": "GateHero"})
	}
	if err := os.WriteFile(api.maintenanceGatePath, []byte(`{"accounts":["release-probe"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if minted := mint(); minted["ok"] != false || minted["code"] != "MAINTENANCE" {
		t.Fatalf("gated mint = %v", minted)
	}
	if err := os.WriteFile(api.maintenanceGatePath, []byte(`{"accounts":["`+testAccount+`"]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if minted := mint(); minted["ok"] != true {
		t.Fatalf("listed account mint = %v", minted)
	}
	if err := os.Remove(api.maintenanceGatePath); err != nil {
		t.Fatal(err)
	}
	if minted := mint(); minted["ok"] != true {
		t.Fatalf("ungated mint = %v", minted)
	}
}
