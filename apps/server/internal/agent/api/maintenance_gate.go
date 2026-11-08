/*
===========================================================================

maintenance_gate.go - who may enter the world during a maintenance release

A maintenance release upgrades the database and
deploys the new server and client with players kept out until the release
is confirmed, so a failed release can restore the database without losing
anyone's play. The release tool writes a gate file naming the accounts that
may still enter (the release probe) and removes it on confirm or revert.
The gate is read at every EnterWorld token mint: nobody reaches the world
without one, so this one check keeps the whole shard closed. Port-only,
not native: the original server has no such window.

===========================================================================
*/
package agentapi

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"strings"
)

// EnvMaintenanceGate names the gate file (deploy.py MAINTENANCE_GATE, set by
// sro-nomad). Unset or empty: no gate, as on every development host.
const EnvMaintenanceGate = "SRO_MAINTENANCE_GATE_PATH"

// maxMaintenanceGateBytes bounds the file read on every mint.
const maxMaintenanceGateBytes = 4 << 10

/*
================
maintenanceAdmits

Whether an account may receive an EnterWorld token. No gate path, or no
file at it, admits everyone. A present file admits only the accounts it
lists (case-insensitively, as login names match); a file that cannot be
read or decoded admits nobody, so a broken gate fails closed.
================
*/
func maintenanceAdmits(path, accountID string) bool {
	if path == "" {
		return true
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil || len(data) > maxMaintenanceGateBytes {
		return false
	}
	var gate struct {
		Accounts []string `json:"accounts"`
	}
	if json.Unmarshal(data, &gate) != nil {
		return false
	}
	for _, account := range gate.Accounts {
		if account != "" && strings.EqualFold(account, accountID) {
			return true
		}
	}
	return false
}

/*
================
MaintenanceGatePresent

Whether a maintenance window is open right now: the gate file exists
at path. Every other gate reader keeps this one's fail-closed rule (a
file that cannot be read or decoded still counts as a window - only
existence is read here, never the listed accounts), so the Agent's
not-operating refusals name the maintenance only while the release
actually holds the gate shut (#2861). No path, or no file at it: no
window.
================
*/
func MaintenanceGatePresent(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}
