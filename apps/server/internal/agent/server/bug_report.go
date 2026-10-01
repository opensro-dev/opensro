/*
===========================================================================

bug_report.go - /title/bug-report, the in-game bug reporter's endpoint

GET answers what the client needs to decide whether to show the bug button
and record the replay (bugreport.Settings); it answers even when the
feature is off, so the client can stay quiet instead of failing.

POST accepts one report from a logged-in browser and posts it to Discord.
The account, division and character come from the session cookies, never
from the body. Pacing is checked before the body is read so a refused
player does not upload a video for nothing.

An upload can take longer than the Agent's server-wide read timeout (a
10 MiB clip on a slow uplink), so this route extends its own deadlines.

===========================================================================
*/
package agentserver

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/agent/bugreport"
)

const (
	bugReportPath          = "/title/bug-report"
	bugReportReadDeadline  = 3 * time.Minute
	bugReportWriteDeadline = 4 * time.Minute
)

/*
================
handleBugReport
================
*/
func (server *Server) handleBugReport(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	switch r.Method {
	case http.MethodGet:
		settings := bugreport.DisabledSettings()
		if server.bugReports != nil {
			settings = server.bugReports.Settings()
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "bugReports": settings})
	case http.MethodPost:
		server.submitBugReport(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

/*
================
submitBugReport
================
*/
func (server *Server) submitBugReport(w http.ResponseWriter, r *http.Request) {
	if server.bugReports == nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "code": "BUG_REPORTS_DISABLED"})
		return
	}
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") {
		http.Error(w, "multipart required", http.StatusUnsupportedMediaType)
		return
	}
	identity, _, ok := server.resolveBrowserIdentity(r)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "code": "UNAUTHORIZED"})
		return
	}
	ticket, refusal := server.bugReports.Admit(identity.accountID)
	if refusal != nil {
		seconds := int(math.Ceil(refusal.RetryAfter.Seconds()))
		w.Header().Set("Retry-After", strconv.Itoa(seconds))
		status, code := http.StatusServiceUnavailable, "BUSY"
		if refusal.RateLimited {
			status, code = http.StatusTooManyRequests, "RATE_LIMITED"
		}
		writeJSON(w, status, map[string]any{"ok": false, "code": code, "retryAfter": seconds})
		return
	}
	delivered := false
	defer func() { ticket.Done(delivered) }()

	controller := http.NewResponseController(w)
	_ = controller.SetReadDeadline(server.now().Add(bugReportReadDeadline))
	_ = controller.SetWriteDeadline(server.now().Add(bugReportWriteDeadline))
	maxBytes := server.bugReports.MaxBytes()
	r.Body = http.MaxBytesReader(w, r.Body, bugreport.BodyLimit(maxBytes))
	submission, err := bugreport.ReadSubmission(r, maxBytes)
	if errors.Is(err, bugreport.ErrTooLarge) {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]any{"ok": false, "code": "REPORT_TOO_LARGE", "maxBytes": maxBytes})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "INVALID_REPORT"})
		return
	}

	id, err := server.bugReports.Deliver(r.Context(), bugreport.Report{
		Account:    identity.accountID,
		Division:   identity.definition.Name,
		Character:  identity.character,
		Submission: submission,
	})
	if err != nil {
		log.Warnf("agent: bug report from %s not delivered: %v", identity.accountID, err)
		if errors.Is(err, bugreport.ErrBusy) {
			w.Header().Set("Retry-After", "30")
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"ok": false, "code": "BUSY", "retryAfter": 30})
			return
		}
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "code": "DELIVERY_FAILED"})
		return
	}
	delivered = true
	log.Infof("agent: bug report from %s delivered (message %s)", identity.accountID, id)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}
