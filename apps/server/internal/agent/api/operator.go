/*
===========================================================================

operator.go - authenticated player diagnostics and audited mutations

Only the local console gateway holds this dedicated credential. The endpoint
rejects browser origins and forwarding headers independently of that secret.
An fsynced intent precedes every mutation. Request IDs remain consumed across
restarts, so a lost HTTP response cannot silently replay a relocation or item grant.

===========================================================================
*/
package agentapi

import (
	"bufio"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/game/staff"
)

const operatorBodyLimit = 4096
const operatorAuditLimit = 16 << 20
const operatorMaxGrantRows = 32
const operatorMaxItemCodeBytes = 128
const operatorMaxGrantCount = 1<<16 - 1

// operatorMaxSilkGrant bounds one silk grant (action.MaxSilkGrant).
const operatorMaxSilkGrant = 1_000_000

var operatorRequestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)

// operatorClause is a published rule's clause id ("C2", "chat.1"). Every
// conduct action cites one, so the audit names the rule, not only staff
// prose (#2671).
var operatorClause = regexp.MustCompile(`^[A-Za-z0-9.-]{1,16}$`)

// operatorMaxCapabilities bounds a staff grant's capability list.
const operatorMaxCapabilities = 64

// operatorConsoleRoles are the console roles a staff action may carry
// (the console's edge identity proves which one signed in).
var operatorConsoleRoles = map[string]bool{"operator": true, "approver": true}

// operatorMaxMuteMinutes bounds one chat mute at 30 days
// (moderation.MaxMuteDuration); longer is an account block.
const operatorMaxMuteMinutes = 30 * 24 * 60

/*
================
PlayerOperation
================
*/
type PlayerOperation struct {
	ID        string              `json:"id"`
	Operator  string              `json:"operator"`
	Character string              `json:"character"`
	Action    string              `json:"action,omitempty"`
	Items     []OperatorItemGrant `json:"items,omitempty"`
	Town      uint32              `json:"town"`
	Reason    string              `json:"reason"`
	// Silk is a grant-silk operation's amount.
	Silk uint32 `json:"silk,omitempty"`
	// Gold is an adjust-gold operation's signed amount (#3309).
	Gold int64 `json:"gold,omitempty"`
	// Minutes is a mute-chat operation's length.
	Minutes uint32 `json:"minutes,omitempty"`
	// Clause is the rule clause a conduct action (mute-chat, unmute-chat,
	// kick) cites; required for those, refused on the others.
	Clause string `json:"clause,omitempty"`
	// Crest is a remove-crest operation's target: "guild" for the
	// named character's guild crest, "union" for the crest of the
	// union that guild stands in (#2282).
	Crest string `json:"crest,omitempty"`
	// Role and Capabilities are a staff-grant's role id and capability
	// ids (package staff). ConsoleRole is the signed-in staff member's
	// console role, which every staff action carries: only "approver"
	// may add an elevated capability, and the staff book enforces it.
	Role         string   `json:"role,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
	ConsoleRole  string   `json:"consoleRole,omitempty"`
	// StaffRole is a staff-role operation's role, added or edited.
	StaffRole *staff.Role `json:"staffRole,omitempty"`
}

/*
================
PlayerOperation.StaffRequest

The staff book's request for a staff-grant: the one place a console role
becomes the approver authority the book checks.
================
*/
func (request PlayerOperation) StaffRequest(division, character string) staff.Request {
	return staff.Request{Division: division, Character: character, Role: request.Role,
		Capabilities: request.Capabilities, Operator: request.Operator, Approver: request.ConsoleRole == "approver"}
}

/*
================
StaffOperations

The console's staff surface (port-only; owner decision 2026-11-23):
Read lists the roles, the capability table and the grants; Grant adds or
replaces one character's grant, Revoke ends it, SetRole adds or edits a
role. Each applies to the live world at once.
================
*/
type StaffOperations struct {
	Read    func() (any, error)
	Grant   func(PlayerOperation) (any, error)
	Revoke  func(PlayerOperation) (any, error)
	SetRole func(PlayerOperation) (any, error)
}

/*
================
OperatorItemGrant

Authored identities and counts only; the authority supplies item properties.
================
*/
type OperatorItemGrant struct {
	Codename string `json:"codename"`
	Count    uint32 `json:"count"`
}

/*
================
PlayerOperations
================
*/
type PlayerOperations struct {
	Token      string
	AuditPath  string
	Read       func(string) (any, error)
	Rescue     func(PlayerOperation) (any, error)
	GrantItems func(PlayerOperation) (any, error)
	ClearPK    func(PlayerOperation) (any, error)
	// ResetStats returns spent STR/INT points to the free pool (port-only).
	ResetStats func(PlayerOperation) (any, error)
	// GrantSilk credits silk to the character's account (operator tooling).
	GrantSilk func(PlayerOperation) (any, error)
	// AdjustGold applies a signed gold correction to the character
	// through its mutation door (#3309, operator tooling).
	AdjustGold func(PlayerOperation) (any, error)
	// MuteChat, UnmuteChat and Kick are the conduct actions (#2671): a
	// persisted chat mute on the character, its removal, and ending the
	// character's session the way a logout does.
	MuteChat   func(PlayerOperation) (any, error)
	UnmuteChat func(PlayerOperation) (any, error)
	Kick       func(PlayerOperation) (any, error)
	// RemoveCrest clears the named character's guild crest, or the
	// crest of the union that guild stands in, reverting it to none
	// (#2282); the game world audits the removal itself.
	RemoveCrest func(PlayerOperation) (any, error)
	// Staff is the staff-grant surface; nil refuses every staff action.
	Staff *StaffOperations
}

/*
================
operatorEndpoint
================
*/
type operatorEndpoint struct {
	mu     sync.Mutex
	config PlayerOperations
	seen   map[string]bool
}

/*
================
InstallPlayerOperations
================
*/
func (api *API) InstallPlayerOperations(config PlayerOperations) error {
	if len(config.Token) < 32 || config.Read == nil || config.Rescue == nil {
		return fmt.Errorf("invalid operator configuration")
	}
	endpoint := &operatorEndpoint{config: config, seen: make(map[string]bool)}
	file, err := os.OpenFile(config.AuditPath, os.O_CREATE|os.O_RDONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() > operatorAuditLimit {
		return fmt.Errorf("operator audit exceeds size limit")
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		var row struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &row); err != nil {
			return fmt.Errorf("invalid operator audit: %w", err)
		}
		if row.ID != "" {
			endpoint.seen[row.ID] = true
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	api.operator = endpoint
	return nil
}

/*
================
ServeHTTP
================
*/
func (endpoint *operatorEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	want := sha256.Sum256([]byte("Bearer " + endpoint.config.Token))
	got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
	if !localDiagnosticsRequest(r) || subtle.ConstantTimeCompare(want[:], got[:]) != 1 {
		http.Error(w, "operator authentication required", http.StatusForbidden)
		return
	}
	endpoint.mu.Lock()
	defer endpoint.mu.Unlock()
	if r.Method == http.MethodGet && r.URL.Query().Get("staff") != "" {
		if endpoint.config.Staff == nil {
			http.Error(w, "staff grants are not enabled", http.StatusNotFound)
			return
		}
		result, err := endpoint.config.Staff.Read()
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if r.Method == http.MethodGet {
		result, err := endpoint.config.Read(r.URL.Query().Get("character"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotFound)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var request PlayerOperation
	if err := decodeJSONRequest(http.MaxBytesReader(w, r.Body, operatorBodyLimit), &request); err != nil ||
		!operatorRequestID.MatchString(request.ID) || len(request.Operator) == 0 || len(request.Operator) > 80 ||
		len(request.Character) == 0 || len(request.Character) > 64 || len(strings.TrimSpace(request.Reason)) < 5 || len(request.Reason) > 500 {
		http.Error(w, "invalid player operation", http.StatusBadRequest)
		return
	}
	conduct := request.Action == "mute-chat" || request.Action == "unmute-chat" || request.Action == "kick"
	if conduct != (request.Clause != "") || (conduct && !operatorClause.MatchString(request.Clause)) {
		http.Error(w, "a conduct action needs a rule clause id; other actions take none", http.StatusBadRequest)
		return
	}
	if request.Action != "mute-chat" && request.Minutes != 0 {
		http.Error(w, "minutes belong to mute-chat only", http.StatusBadRequest)
		return
	}
	if request.Action != "remove-crest" && request.Crest != "" {
		http.Error(w, "a crest target belongs to remove-crest only", http.StatusBadRequest)
		return
	}
	if request.Action != "adjust-gold" && request.Gold != 0 {
		http.Error(w, "a gold amount belongs to adjust-gold only", http.StatusBadRequest)
		return
	}
	staffAction := strings.HasPrefix(request.Action, "staff-")
	if staffAction != (request.ConsoleRole != "") || (staffAction && !operatorConsoleRoles[request.ConsoleRole]) {
		http.Error(w, "a staff action needs the console role; other actions take none", http.StatusBadRequest)
		return
	}
	if (request.Action != "staff-grant" && (request.Role != "" || len(request.Capabilities) != 0)) ||
		(request.Action != "staff-role" && request.StaffRole != nil) || len(request.Capabilities) > operatorMaxCapabilities {
		http.Error(w, "staff fields belong to their staff action only", http.StatusBadRequest)
		return
	}
	operation := endpoint.config.Rescue
	switch request.Action {
	case "", "rescue":
		if len(request.Items) != 0 || request.Silk != 0 {
			http.Error(w, "rescue cannot grant items", http.StatusBadRequest)
			return
		}
	case "grant-items":
		if endpoint.config.GrantItems == nil || request.Town != 0 || len(request.Items) == 0 || len(request.Items) > operatorMaxGrantRows {
			http.Error(w, "invalid item grant", http.StatusBadRequest)
			return
		}
		for _, item := range request.Items {
			if len(item.Codename) == 0 || len(item.Codename) > operatorMaxItemCodeBytes || item.Count == 0 || item.Count > operatorMaxGrantCount {
				http.Error(w, "invalid item grant", http.StatusBadRequest)
				return
			}
		}
		operation = endpoint.config.GrantItems
	case "clear-pk":
		if endpoint.config.ClearPK == nil || request.Town != 0 || len(request.Items) != 0 {
			http.Error(w, "invalid PK clear", http.StatusBadRequest)
			return
		}
		operation = endpoint.config.ClearPK
	case "grant-silk":
		if endpoint.config.GrantSilk == nil || request.Town != 0 || len(request.Items) != 0 ||
			request.Silk == 0 || request.Silk > operatorMaxSilkGrant {
			http.Error(w, "invalid silk grant", http.StatusBadRequest)
			return
		}
		operation = endpoint.config.GrantSilk
	case "adjust-gold":
		if endpoint.config.AdjustGold == nil || request.Town != 0 || len(request.Items) != 0 ||
			request.Silk != 0 || request.Gold == 0 {
			http.Error(w, "invalid gold adjustment", http.StatusBadRequest)
			return
		}
		operation = endpoint.config.AdjustGold
	case "reset-stats":
		if endpoint.config.ResetStats == nil || request.Town != 0 || len(request.Items) != 0 {
			http.Error(w, "invalid stat reset", http.StatusBadRequest)
			return
		}
		operation = endpoint.config.ResetStats
	case "mute-chat":
		if endpoint.config.MuteChat == nil || request.Town != 0 || len(request.Items) != 0 || request.Silk != 0 ||
			request.Minutes == 0 || request.Minutes > operatorMaxMuteMinutes {
			http.Error(w, "invalid chat mute", http.StatusBadRequest)
			return
		}
		operation = endpoint.config.MuteChat
	case "unmute-chat", "kick":
		hook := endpoint.config.UnmuteChat
		if request.Action == "kick" {
			hook = endpoint.config.Kick
		}
		if hook == nil || request.Town != 0 || len(request.Items) != 0 || request.Silk != 0 {
			http.Error(w, "invalid "+request.Action, http.StatusBadRequest)
			return
		}
		operation = hook
	case "remove-crest":
		if endpoint.config.RemoveCrest == nil || request.Town != 0 || len(request.Items) != 0 || request.Silk != 0 ||
			(request.Crest != "guild" && request.Crest != "union") {
			http.Error(w, "invalid crest removal", http.StatusBadRequest)
			return
		}
		operation = endpoint.config.RemoveCrest
	case "staff-grant", "staff-revoke", "staff-role":
		hooks := endpoint.config.Staff
		if hooks == nil || request.Town != 0 || len(request.Items) != 0 || request.Silk != 0 {
			http.Error(w, "invalid "+request.Action, http.StatusBadRequest)
			return
		}
		switch request.Action {
		case "staff-grant":
			if request.Role == "" {
				http.Error(w, "a staff grant needs a role", http.StatusBadRequest)
				return
			}
			operation = hooks.Grant
		case "staff-revoke":
			operation = hooks.Revoke
		default:
			if request.StaffRole == nil {
				http.Error(w, "a staff-role action needs the role", http.StatusBadRequest)
				return
			}
			operation = hooks.SetRole
		}
	default:
		http.Error(w, "unknown player operation", http.StatusBadRequest)
		return
	}
	if endpoint.seen[request.ID] {
		http.Error(w, "request already recorded; inspect player before submitting another operation", http.StatusConflict)
		return
	}
	// A role edit names no character: its "before" is the staff list.
	read := func() (any, error) { return endpoint.config.Read(request.Character) }
	if request.Action == "staff-role" {
		read = endpoint.config.Staff.Read
	}
	before, err := read()
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err := endpoint.audit(map[string]any{"id": request.ID, "phase": "intent", "request": request, "before": before}); err != nil {
		http.Error(w, "cannot write operation audit", http.StatusServiceUnavailable)
		return
	}
	endpoint.seen[request.ID] = true
	result, operationErr := operation(request)
	outcome := map[string]any{"id": request.ID, "phase": "complete", "result": result}
	if operationErr != nil {
		outcome["error"] = operationErr.Error()
	}
	if err := endpoint.audit(outcome); err != nil {
		http.Error(w, "operation outcome audit failed; inspect player before retrying", http.StatusServiceUnavailable)
		return
	}
	if errors.Is(operationErr, staff.ErrElevated) {
		http.Error(w, operationErr.Error(), http.StatusForbidden)
		return
	}
	if operationErr != nil {
		http.Error(w, operationErr.Error(), http.StatusConflict)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

/*
================
audit
================
*/
func (endpoint *operatorEndpoint) audit(row map[string]any) error {
	row["at"] = time.Now().UTC().Format(time.RFC3339Nano)
	bytes, err := json.Marshal(row)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(endpoint.config.AuditPath, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size()+int64(len(bytes))+1 > operatorAuditLimit {
		return fmt.Errorf("operator audit full")
	}
	if _, err := file.Write(append(bytes, '\n')); err != nil {
		return err
	}
	return file.Sync()
}
