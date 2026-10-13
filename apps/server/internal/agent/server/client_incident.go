/*
===========================================================================

client_incident.go - /client/incident, a client's report of a fatal failure

A packet the client cannot apply ends its session on the client side: the
socket closes and the GameWorld sees an ordinary disconnect. Nothing on the
server would show why. Before it disconnects, the client posts what failed
(the error, the opcode and a bounded hex dump of the frame) here, and the
Agent writes one structured warning line to its log, which is where an
operator diagnoses it.

The account comes from the session bearer token, never from the body, and
must still be a stored account on an enabled shard (liveSession). The log
line names that identity, never the token. The character and region in the
body are what the client says and are logged as such. Reports are paced per account so a client stuck in a failure loop
cannot flood the log.

===========================================================================
*/
package agentserver

import (
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"opensro.online/server/internal/platform/history"
	"regexp"
	"strings"

	log "github.com/sirupsen/logrus"
)

const (
	clientIncidentPath = "/client/incident"
	// Bounds on what one report may carry. A frame larger than the dump
	// bound is cut there; the client reports its full length separately.
	maxIncidentMessage   = 500
	maxIncidentDumpBytes = 512
	maxIncidentName      = 64
	maxIncidentPhase     = 32
	// The frame journal a fatal report carries (contracts/network.ts
	// INCIDENT_JOURNAL_ROWS): per row ms before the failure, direction,
	// opcode and size, never a payload. A row older than the window is
	// not evidence of this failure.
	maxIncidentJournalRows = 200
	incidentJournalFields  = 4
	maxIncidentJournalAge  = 10 * 60 * 1000
	// A gap row's size is the count of frames the journal dropped.
	maxIncidentJournalBytes = 1 << 20
)

// incidentJournalDirections are the journal's row kinds, in the client's
// NET_JOURNAL_IN / OUT / GAP order.
var incidentJournalDirections = [...]string{"i", "o", "g"}

// incidentKind names the failures a client may report.
// "degraded" is the one non-fatal kind: the client skipped a cosmetic step
// (a missing effect, a visual it could not resolve) and kept the session.
var incidentKinds = map[string]bool{"packet": true, "transport": true, "runtime": true, "asset": true, "unsupported": true, "degraded": true}

// degradedCategory files every degraded report apart from failures, for the
// console's degraded groups (history degradedGroups).
const degradedCategory = "degraded"

var incidentNamePattern = regexp.MustCompile(`^[\p{L}\p{N}_\[\] .-]*$`)

/*
================
clientIncident

The JSON body of one report. Opcode and Payload describe the frame being
applied when it failed; both are absent for a failure outside packet
application.
================
*/
type clientIncident struct {
	Category    string `json:"category,omitempty"`
	ID          string `json:"id,omitempty"`
	Session     string `json:"session,omitempty"`
	Code        string `json:"code,omitempty"`
	Stack       string `json:"stack,omitempty"`
	Kind        string `json:"kind"`
	Message     string `json:"message"`
	Opcode      *int   `json:"opcode,omitempty"`
	Payload     string `json:"payload,omitempty"`
	PayloadSize int    `json:"payloadSize,omitempty"`
	Phase       string `json:"phase,omitempty"`
	Character   string `json:"character,omitempty"`
	Region      int    `json:"region,omitempty"`
	Build       string `json:"build,omitempty"`
	// Journal is the last frames before the failure, flattened rows of
	// incidentJournalFields numbers.
	Journal []float64 `json:"journal,omitempty"`
}

/*
================
problem

Why the log line could not carry a report faithfully, or "" for one it can.
The journal is not judged here: a bad journal never costs its incident
(incidentJournalProblem).
================
*/
func (incident clientIncident) problem() string {
	if incident.Category != "" && incident.Category != "expected" && incident.Category != "connection" && incident.Category != "software" && incident.Category != "unknown" && incident.Category != degradedCategory {
		return "unknown category"
	}
	// A degraded report names its site by a stable code: the console groups
	// by it and an agent finds the throw site from it.
	if incident.Kind == "degraded" && incident.Code == "" {
		return "degraded report without a code"
	}
	if incident.Kind != "degraded" && incident.Category == degradedCategory {
		return "degraded category on a fatal report"
	}
	if incident.ID != "" && !incidentIdentifier.MatchString(incident.ID) {
		return "malformed id"
	}
	if incident.Session != "" && !incidentSessionIdentifier.MatchString(incident.Session) {
		return "malformed session"
	}
	if len(incident.Code) > 64 || (incident.Code != "" && !incidentNamePattern.MatchString(incident.Code)) || len(incident.Stack) > 8192 {
		return "malformed code or stack"
	}
	if !incidentKinds[incident.Kind] || incident.Message == "" || len(incident.Message) > maxIncidentMessage {
		return "unknown kind or bad message"
	}
	if incident.Opcode != nil && (*incident.Opcode < 0 || *incident.Opcode > 0xFFFF) {
		return "opcode out of range"
	}
	if len(incident.Payload) > 2*maxIncidentDumpBytes || len(incident.Payload)%2 != 0 {
		return "payload dump too long or odd"
	}
	if _, err := hex.DecodeString(incident.Payload); err != nil {
		return "payload dump not hex"
	}
	if incident.PayloadSize < 0 || incident.PayloadSize > 1<<20 || incident.Region < 0 || incident.Region > 0xFFFF {
		return "payload size or region out of range"
	}
	for _, text := range []string{incident.Phase, incident.Character, incident.Build} {
		if len(text) > maxIncidentName || !incidentNamePattern.MatchString(text) {
			return "malformed phase, character or build"
		}
	}
	if len(incident.Phase) > maxIncidentPhase {
		return "phase too long"
	}
	return ""
}

/*
================
incidentJournalProblem

Why a journal cannot be rendered faithfully, or "" for one that can:
whole rows only, at most maxIncidentJournalRows, each a time at or before
the failure within the window, a known direction, an opcode and a size.
The caller drops a bad journal and keeps the incident (#788: a strict check
on a new client field must not discard the evidence it exists to collect).
================
*/
func incidentJournalProblem(journal []float64) string {
	if len(journal)%incidentJournalFields != 0 {
		return "partial row"
	}
	if len(journal) > maxIncidentJournalRows*incidentJournalFields {
		return "too many rows"
	}
	for at := 0; at < len(journal); at += incidentJournalFields {
		age, direction, opcode, size := journal[at], journal[at+1], journal[at+2], journal[at+3]
		if age != math.Trunc(age) || age > 0 || age < -maxIncidentJournalAge {
			return fmt.Sprintf("row %d: age %v", at/incidentJournalFields, age)
		}
		if direction != math.Trunc(direction) || direction < 0 || int(direction) >= len(incidentJournalDirections) {
			return fmt.Sprintf("row %d: direction %v", at/incidentJournalFields, direction)
		}
		if opcode != math.Trunc(opcode) || opcode < 0 || opcode > 0xFFFF {
			return fmt.Sprintf("row %d: opcode %v", at/incidentJournalFields, opcode)
		}
		if size != math.Trunc(size) || size < 0 || size > maxIncidentJournalBytes {
			return fmt.Sprintf("row %d: size %v", at/incidentJournalFields, size)
		}
	}
	return ""
}

/*
================
renderIncidentJournal

One line a log reader can scan: "age:dir:OPCD:size" per frame, oldest
first, e.g. "-1520:o:706D:7 -1448:i:B06D:8".
================
*/
func renderIncidentJournal(journal []float64) string {
	rows := make([]string, 0, len(journal)/incidentJournalFields)
	for at := 0; at < len(journal); at += incidentJournalFields {
		rows = append(rows, fmt.Sprintf("%d:%s:%04X:%d", int64(journal[at]), incidentJournalDirections[int(journal[at+1])],
			int(journal[at+2]), int64(journal[at+3])))
	}
	return strings.Join(rows, " ")
}

/*
================
handleClientIncident
================
*/
func (server *Server) handleClientIncident(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims, ok := server.bearerClaims(r)
	definition, live := server.liveSession(claims)
	if !ok || !live {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "code": "UNAUTHORIZED"})
		return
	}
	// The body is bounded (decodeJSON), so it is read before pacing: a
	// degraded report draws on its own allowance, and the cosmetic reports
	// can never use up the one a session-ending failure needs.
	var incident clientIncident
	reason := ""
	if err := decodeJSON(r, &incident); err != nil {
		reason = "malformed body: " + err.Error()
	} else {
		reason = incident.problem()
	}
	pace := server.incidentReports
	if reason == "" && incident.Kind == "degraded" {
		pace = server.degradedReports
	}
	if !pace.Allow("account/" + claims.AccountID) {
		w.Header().Set("Retry-After", "6")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "code": "RATE_LIMITED"})
		return
	}
	if reason != "" {
		// A refused report is evidence lost: name why, so a client change that
		// trips it shows in the log instead of vanishing (#788).
		// The reason only: the caller's identity comes from its bearer header,
		// which stays out of a refusal line (CodeQL go/clear-text-logging).
		log.WithField("reason", reason).Warn("agent: client incident refused")
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "INVALID_INCIDENT"})
		return
	}
	fields := log.Fields{
		"shard":     definition.ID,
		"kind":      incident.Kind,
		"message":   strings.ToValidUTF8(incident.Message, "?"),
		"phase":     incident.Phase,
		"character": incident.Character,
		"region":    incident.Region,
		"build":     incident.Build,
	}
	if incident.Opcode != nil {
		fields["opcode"] = "0x" + strings.ToUpper(hex.EncodeToString([]byte{byte(*incident.Opcode >> 8), byte(*incident.Opcode)}))
		fields["payload"] = incident.Payload
		fields["payloadSize"] = incident.PayloadSize
	}
	eventFields := map[string]string{"phase": incident.Phase, "region": fmt.Sprint(incident.Region)}
	if incident.Payload != "" {
		// The console shows a group's example frame from its newest event.
		eventFields["payload"] = incident.Payload
	}
	if problem := incidentJournalProblem(incident.Journal); problem != "" {
		fields["journalInvalid"] = problem
		eventFields["journalInvalid"] = problem
	} else if len(incident.Journal) != 0 {
		journal := renderIncidentJournal(incident.Journal)
		fields["journal"] = journal
		eventFields["journal"] = journal
	}
	id := incident.ID
	if id == "" {
		var err error
		id, err = history.NewID()
		if err != nil {
			http.Error(w, "incident reference unavailable", http.StatusServiceUnavailable)
			return
		}
	}
	category := "software"
	if incident.Kind == "transport" {
		category = "unknown"
	}
	if incident.Category != "" {
		category = incident.Category
	}
	if incident.Kind == "degraded" {
		category = degradedCategory
	}
	code := incident.Code
	if code == "" {
		code = incident.Kind + "_failure"
	}
	event := history.Event{ID: id, Kind: "client_incident", Account: claims.AccountID, Shard: definition.ID,
		Character: incident.Character, Session: incident.Session, Category: category, Code: code, Message: incident.Message,
		Build: incident.Build, Evidence: "client_reported", Stack: incident.Stack, Fields: eventFields}
	if incident.Opcode != nil {
		event.Opcode = fmt.Sprintf("0x%04X", *incident.Opcode)
	}
	if server.history != nil {
		if err := server.history.RecordConfirmed(r.Context(), event); err != nil {
			http.Error(w, "incident storage unavailable; retry with the same id", http.StatusServiceUnavailable)
			return
		}
	}
	fields["incident"] = id
	fields["diagnosticSession"] = incident.Session
	fields["code"] = code
	// A degraded line names no account: the history event keeps it, and
	// a cosmetic skip is triaged by site code, not by player.
	if incident.Kind == "degraded" {
		log.WithFields(fields).Warn("agent: client degraded")
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
		return
	}
	fields["account"] = claims.AccountID
	log.WithFields(fields).Warn("agent: client incident")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

var incidentIdentifier = regexp.MustCompile(`^[a-f0-9]{32}$`)
var incidentSessionIdentifier = regexp.MustCompile(`^[a-f0-9]{32}:[0-9]{1,20}$`)
