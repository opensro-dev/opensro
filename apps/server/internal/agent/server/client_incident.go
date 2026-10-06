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
)

// incidentKind names the failures a client may report.
var incidentKinds = map[string]bool{"packet": true, "transport": true, "runtime": true, "asset": true, "unsupported": true}

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
}

/*
================
valid

Rejects a report whose fields the log line could not carry faithfully.
================
*/
func (incident clientIncident) valid() bool {
	if incident.Category != "" && incident.Category != "expected" && incident.Category != "connection" && incident.Category != "software" && incident.Category != "unknown" {
		return false
	}
	if incident.ID != "" && !incidentIdentifier.MatchString(incident.ID) {
		return false
	}
	if incident.Session != "" && !incidentSessionIdentifier.MatchString(incident.Session) {
		return false
	}
	if len(incident.Code) > 64 || (incident.Code != "" && !incidentNamePattern.MatchString(incident.Code)) || len(incident.Stack) > 8192 {
		return false
	}
	if !incidentKinds[incident.Kind] || incident.Message == "" || len(incident.Message) > maxIncidentMessage {
		return false
	}
	if incident.Opcode != nil && (*incident.Opcode < 0 || *incident.Opcode > 0xFFFF) {
		return false
	}
	if len(incident.Payload) > 2*maxIncidentDumpBytes || len(incident.Payload)%2 != 0 {
		return false
	}
	if _, err := hex.DecodeString(incident.Payload); err != nil {
		return false
	}
	if incident.PayloadSize < 0 || incident.PayloadSize > 1<<20 || incident.Region < 0 || incident.Region > 0xFFFF {
		return false
	}
	for _, text := range []string{incident.Phase, incident.Character, incident.Build} {
		if len(text) > maxIncidentName || !incidentNamePattern.MatchString(text) {
			return false
		}
	}
	return len(incident.Phase) <= maxIncidentPhase
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
	if !server.incidentReports.Allow("account/" + claims.AccountID) {
		w.Header().Set("Retry-After", "6")
		writeJSON(w, http.StatusTooManyRequests, map[string]any{"ok": false, "code": "RATE_LIMITED"})
		return
	}
	var incident clientIncident
	if err := decodeJSON(r, &incident); err != nil || !incident.valid() {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "code": "INVALID_INCIDENT"})
		return
	}
	fields := log.Fields{
		"account":   claims.AccountID,
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
	code := incident.Code
	if code == "" {
		code = incident.Kind + "_failure"
	}
	event := history.Event{ID: id, Kind: "client_incident", Account: claims.AccountID, Shard: definition.ID,
		Character: incident.Character, Session: incident.Session, Category: category, Code: code, Message: incident.Message,
		Build: incident.Build, Evidence: "client_reported", Stack: incident.Stack, Fields: map[string]string{"phase": incident.Phase, "region": fmt.Sprint(incident.Region)}}
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
	log.WithFields(fields).Warn("agent: client incident")
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "id": id})
}

var incidentIdentifier = regexp.MustCompile(`^[a-f0-9]{32}$`)
var incidentSessionIdentifier = regexp.MustCompile(`^[a-f0-9]{32}:[0-9]{1,20}$`)
