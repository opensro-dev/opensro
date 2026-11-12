/*
===========================================================================

logs.go - structured server logs and authenticated operator history access

No tokens, cookies or raw packet payloads are copied into the log index.
The ordinary supervisor log remains intact. Hook errors never recursively log.

===========================================================================
*/
package history

import (
	"context"
	"fmt"
	"net/http"
	"runtime/debug"
	"time"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/operatorauth"
)

const Path = "/internal/operations/history"

/*
================
Build
================
*/
func Build() string {
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, s := range info.Settings {
			if s.Key == "vcs.revision" {
				return s.Value
			}
		}
	}
	return "development"
}

/*
================
Levels
================
*/
func (j *Journal) Levels() []log.Level {
	return []log.Level{log.InfoLevel, log.WarnLevel, log.ErrorLevel, log.FatalLevel, log.PanicLevel}
}

/*
================
Fire

Every line at warning or above is stored, and so is an info line that
names no session. A chattier line (info, or debug if a test fires one)
carrying a session is the transport lane's per-session chatter: the
supervisor log keeps it, but the journal does not store it as an event
(#3307) - one session looping three times a second otherwise writes
the whole raw window on its own. A session's lifecycle facts are
already rows in the sessions table, and explicit history events
(refusals, incidents) are recorded beside this hook, never through it.
================
*/
func (j *Journal) Fire(entry *log.Entry) error {
	e := Event{At: entry.Time.UnixMilli(), Kind: "log", Level: entry.Level.String(), Message: entry.Message, Category: "unknown", Fields: map[string]string{}}
	if entry.Level <= log.ErrorLevel {
		e.Category = "software"
	}
	for _, key := range []string{"session", "account", "character", "shard", "opcode", "cause", "panic", "hook", "build", "phase", "region", "reason"} {
		if value, ok := entry.Data[key]; ok {
			e.Fields[key] = bounded(fmt.Sprint(value), 512)
		}
	}
	e.Account = e.Fields["account"]
	e.Character = e.Fields["character"]
	e.Session = e.Fields["session"]
	if e.Session != "" {
		e.Session = j.SessionID(e.Session)
	}
	if e.Session != "" && entry.Level >= log.InfoLevel {
		return nil
	}
	e.Opcode = e.Fields["opcode"]
	if value, ok := entry.Data["stack"]; ok {
		e.Stack = fmt.Sprint(value)
	}
	if entry.Level <= log.FatalLevel {
		// The process may exit as soon as this hook returns. The
		// journal's answer stays here: a hook that returns an error
		// makes logrus print "Failed to fire hook" at the operator,
		// and a "history queue unavailable" from a journal the
		// shutdown already closed cost a clean stop its exit 0 - the
		// entry itself still reaches the console formatter (#3186).
		ctx := entry.Context
		if ctx == nil {
			ctx = context.TODO()
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		_ = j.RecordConfirmed(ctx, e)
		return nil
	}
	j.Record(e)
	return nil
}

/*
================
OperatorHandler

Even with a valid secret, browser-origin and forwarded requests are refused.
An absent token leaves collection enabled but the read endpoint inaccessible.
================
*/
func OperatorHandler(j *Journal, tokenPath string) (http.Handler, error) {
	guard, found, err := operatorauth.Load(tokenPath, "history")
	if err != nil {
		return nil, err
	}
	if !found {
		return http.NotFoundHandler(), nil
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !guard.Allows(r) {
			http.Error(w, "operator authentication required", http.StatusForbidden)
			return
		}
		j.ServeHTTP(w, r)
	}), nil
}
