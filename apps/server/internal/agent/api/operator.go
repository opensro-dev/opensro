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
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

const operatorBodyLimit = 4096
const operatorAuditLimit = 16 << 20
const operatorMaxGrantRows = 32
const operatorMaxItemCodeBytes = 128
const operatorMaxGrantCount = 1<<16 - 1

var operatorRequestID = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,80}$`)

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
	operation := endpoint.config.Rescue
	switch request.Action {
	case "", "rescue":
		if len(request.Items) != 0 {
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
	case "reset-stats":
		if endpoint.config.ResetStats == nil || request.Town != 0 || len(request.Items) != 0 {
			http.Error(w, "invalid stat reset", http.StatusBadRequest)
			return
		}
		operation = endpoint.config.ResetStats
	default:
		http.Error(w, "unknown player operation", http.StatusBadRequest)
		return
	}
	if endpoint.seen[request.ID] {
		http.Error(w, "request already recorded; inspect player before submitting another operation", http.StatusConflict)
		return
	}
	before, err := endpoint.config.Read(request.Character)
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
