/*
===========================================================================

routes.go - the settings endpoint's write and audit routes

Port-only, not native. Under the same operator guard as the read:

	GET  <Path>/audit?limit=&before=           newest first
	POST <Path>/proposals                      {"key","to","reason"} -> 201 {"id"}
	POST <Path>/proposals/<id>/approve         {} -> {"result":"applied"|"queued"}
	POST <Path>/proposals/<id>/reject          {"reason"}
	POST <Path>/proposals/<id>/withdraw        {}

A write without the console's staff identity (HeaderStaff) is refused with
412 before anything else is read. Errors are {"error":{"code","message"}}.

===========================================================================
*/
package settingsapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// maxBodyBytes bounds a write's JSON body.
const maxBodyBytes = 4 << 10

// The audit page's size limits.
const (
	defaultAuditLimit = 100
	maxAuditLimit     = 500
)

/*
================
serveWrite

A write route under the guard. service nil: the shard has no settings
store, and writes are refused.
================
*/
func serveWrite(w http.ResponseWriter, r *http.Request, service *Service, route string) {
	if service == nil {
		writeError(w, http.StatusServiceUnavailable, "no_store", "this shard keeps no settings store")
		return
	}
	who := Staff{ID: strings.TrimSpace(r.Header.Get(HeaderStaff)), Name: strings.TrimSpace(r.Header.Get(HeaderStaffName))}
	if who.ID == "" {
		writeError(w, http.StatusPreconditionFailed, "no_staff", "a signed-in staff identity is required")
		return
	}
	var body struct{ Key, To, Reason string }
	data, err := io.ReadAll(io.LimitReader(r.Body, maxBodyBytes+1))
	if err != nil || len(data) > maxBodyBytes || (len(data) > 0 && json.Unmarshal(data, &body) != nil) {
		writeError(w, http.StatusBadRequest, "bad_body", "the body must be a small JSON object")
		return
	}
	if route == "proposals" {
		p, err := service.Propose(who, body.Key, body.To, body.Reason)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		writeJSON(w, map[string]string{"id": p.ID})
		return
	}
	id, action, ok := strings.Cut(strings.TrimPrefix(route, "proposals/"), "/")
	if !ok || id == "" || !strings.HasPrefix(route, "proposals/") {
		writeError(w, http.StatusNotFound, "no_route", "no such route")
		return
	}
	switch action {
	case "approve":
		result, err := service.Approve(who, id)
		if err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, map[string]string{"result": result})
	case "reject":
		if err := service.Reject(who, id, body.Reason); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, map[string]string{"result": ResultRejected})
	case "withdraw":
		if err := service.Withdraw(who, id); err != nil {
			writeServiceError(w, err)
			return
		}
		writeJSON(w, map[string]string{"result": ResultRejected})
	default:
		writeError(w, http.StatusNotFound, "no_route", "no such route")
	}
}

/*
================
serveAudit
================
*/
func serveAudit(w http.ResponseWriter, r *http.Request, service *Service) {
	if service == nil {
		writeJSON(w, map[string][]AuditEntry{"entries": {}})
		return
	}
	limit, before := defaultAuditLimit, int64(0)
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, maxAuditLimit)
	}
	if v, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil && v > 0 {
		before = v
	}
	entries, err := service.Store.Audit(limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "store", "the audit could not be read")
		return
	}
	if entries == nil {
		entries = []AuditEntry{}
	}
	writeJSON(w, map[string][]AuditEntry{"entries": entries})
}

/*
================
writeServiceError

The HTTP status of an approval refusal.
================
*/
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, ErrConflict):
		writeError(w, http.StatusConflict, "pending", err.Error())
	case errors.Is(err, errSelfApproval), errors.Is(err, errNotProposer):
		writeError(w, http.StatusForbidden, "not_allowed", err.Error())
	default:
		writeError(w, http.StatusBadRequest, "refused", err.Error())
	}
}

/*
================
writeError
================
*/
func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]map[string]string{"error": {"code": code, "message": message}})
}

/*
================
writeJSON
================
*/
func writeJSON(w http.ResponseWriter, value any) {
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	_ = json.NewEncoder(w).Encode(value)
}
