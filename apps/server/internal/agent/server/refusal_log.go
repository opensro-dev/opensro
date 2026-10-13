/*
===========================================================================

refusal_log.go - one log line for every refused title or world-entry request

A player who cannot log in, select a character or enter the world sees a
message; before this, the operator saw nothing. Each refusal on /title/login
and on the routes the Agent forwards to a GameWorld (character select, area
entry, world and transport tokens) writes one structured warning naming the
route and a stable reason code, and a history event beside it.

A line never carries a password, a token, a request body or a GameWorld
answer's text: only codes, the claimed account (bounded) and a keyed client
tag. Lines are paced per client, so a password spray cannot fill the log.
Log-only: no wire bytes and no player-visible behaviour change.

===========================================================================
*/
package agentserver

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"opensro.online/server/internal/platform/history"

	log "github.com/sirupsen/logrus"
)

const (
	// loginPath is the title login route.
	loginPath = "/title/login"
	// clientKeyBytes is the size of the per-start key behind client tags.
	clientKeyBytes = 32
	// clientTagHexChars is how much of the keyed hash a line keeps: enough
	// to tell clients apart within one Agent's life, no more.
	clientTagHexChars = 12
	// maxRelayedCodeLength caps a GameWorld refusal code copied into a line.
	maxRelayedCodeLength = 64
	// requestRefusedKind is the history kind for refusals outside login,
	// which keeps its own "login_refused".
	requestRefusedKind = "request_refused"
	// notAnAccount stands for a typed login id that names no stored account.
	// It is never logged as typed: a password typed into the id box would
	// otherwise reach the log.
	notAnAccount = "(not an account)"
	// refusalLinesPerWindow bounds the refusal lines the whole Agent writes
	// per refusalWindow, whatever addresses they come from: the per-address
	// pacer alone lets rotating addresses write without bound.
	refusalLinesPerWindow = 120
	refusalWindow         = time.Minute
)

/*
================
refusalCeiling

The shard-wide bound on refusal lines. Past the ceiling a window's lines are
counted, not written, and the next window opens with one summary line.
================
*/
type refusalCeiling struct {
	mu         sync.Mutex
	start      time.Time
	written    int
	suppressed int
}

/*
================
refusalCeiling.admit

Whether one more line may be written now; suppressed is the count the
previous window dropped, reported once when a new window opens.
================
*/
func (c *refusalCeiling) admit(now time.Time) (allowed bool, suppressed int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.start.IsZero() || now.Sub(c.start) >= refusalWindow {
		suppressed = c.suppressed
		c.start, c.written, c.suppressed = now, 0, 0
	}
	if c.written >= refusalLinesPerWindow {
		c.suppressed++
		return false, suppressed
	}
	c.written++
	return true, suppressed
}

/*
================
loggableAccount

The stored account a typed login id names, or notAnAccount. Only a stored
id is ever logged, so nothing typed by mistake (a password in the id box)
reaches the log.
================
*/
func (server *Server) loggableAccount(typed string) string {
	if server.accounts == nil {
		return notAnAccount
	}
	if accountID, _, found := server.accounts.Credential(typed); found {
		return accountID
	}
	return notAnAccount
}

// relayedCodePattern admits the enum-style codes a GameWorld answers with
// ("UNKNOWN_CHARACTER", "CHARACTER_IN_PLAY"); anything else is not a code.
var relayedCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)

/*
================
refusal

One refused request. Account is the session's account when the route has
one, else the bounded name the client claimed.
================
*/
type refusal struct {
	Route   string
	Code    string
	Status  int
	Account string
	Shard   string
	// Kind is the history kind; empty records no history event (the caller
	// already recorded one).
	Kind string
	// Source says who refused: "agent", or "gameworld" for a relayed answer.
	Source string
}

/*
================
clientTag

A keyed hash of the client address. An unkeyed hash of an IPv4 address
falls to a brute force of the whole address space; this one needs the key,
which lives only in this process.
================
*/
func (server *Server) clientTag(r *http.Request) string {
	mac := hmac.New(sha256.New, server.clientKey)
	mac.Write([]byte(clientIP(r)))
	return hex.EncodeToString(mac.Sum(nil))[:clientTagHexChars]
}

/*
================
logRefusal

Writes the refusal's warning line and, when it names a history kind, its
history event. Paced per client address, then bounded shard-wide
(refusalCeiling): past either allowance the line and event are dropped,
never the refusal itself, and a dropped window is summed in one line.
================
*/
func (server *Server) logRefusal(r *http.Request, entry refusal) {
	if server.refusalLines != nil && !server.refusalLines.Allow("refusal/"+clientIP(r)) {
		return
	}
	allowed, suppressed := server.refusalCeiling.admit(server.now())
	if suppressed > 0 {
		log.WithFields(log.Fields{"suppressed": suppressed, "window": refusalWindow.String()}).
			Warn("agent: request refusal lines suppressed")
	}
	if !allowed {
		return
	}
	source := entry.Source
	if source == "" {
		source = "agent"
	}
	log.WithFields(log.Fields{
		"route":   entry.Route,
		"code":    entry.Code,
		"status":  entry.Status,
		"account": strings.ToValidUTF8(entry.Account, "?"),
		"shard":   strings.ToValidUTF8(entry.Shard, "?"),
		"client":  server.clientTag(r),
		"source":  source,
	}).Warn("agent: request refused")
	if entry.Kind == "" || server.history == nil {
		return
	}
	server.history.Record(history.Event{Kind: entry.Kind, Category: "expected", Code: strings.ToLower(entry.Code),
		Account: entry.Account, Shard: entry.Shard, Fields: map[string]string{"route": entry.Route, "source": source}})
}

/*
================
loggableShard

The configured shard a typed server id names, or "" for any other text.
================
*/
func (server *Server) loggableShard(typed string) string {
	if server.catalog == nil {
		return ""
	}
	if definition, ok := server.catalog.Resolve(typed); ok {
		return definition.ID
	}
	return ""
}

/*
================
relayedRefusalCode

The code of a GameWorld answer that refused the request: a JSON object with
"ok": false, or any answer of status 400 and above. Only a code shaped like
the GameWorld's enums is kept, cut to maxRelayedCodeLength; the answer's
other text never reaches the log. ok reports whether the answer refused.
================
*/
func relayedRefusalCode(status int, body []byte) (string, bool) {
	var answer struct {
		OK   *bool  `json:"ok"`
		Code string `json:"code"`
	}
	parsed := json.Unmarshal(body, &answer) == nil
	refused := status >= http.StatusBadRequest || parsed && answer.OK != nil && !*answer.OK
	if !refused {
		return "", false
	}
	code := answer.Code
	if len(code) > maxRelayedCodeLength {
		code = code[:maxRelayedCodeLength]
	}
	if !parsed || !relayedCodePattern.MatchString(code) {
		code = "UNCODED"
	}
	return code, true
}
