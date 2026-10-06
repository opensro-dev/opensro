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
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	log "github.com/sirupsen/logrus"
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
	e.Opcode = e.Fields["opcode"]
	if value, ok := entry.Data["stack"]; ok {
		e.Stack = fmt.Sprint(value)
	}
	if entry.Level <= log.FatalLevel {
		// The process may exit as soon as this hook returns.
		ctx := entry.Context
		if ctx == nil {
			ctx = context.TODO()
		}
		ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		return j.RecordConfirmed(ctx, e)
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
	bytes, err := os.ReadFile(tokenPath)
	if errors.Is(err, os.ErrNotExist) {
		return http.NotFoundHandler(), nil
	}
	if err != nil {
		return nil, err
	}
	token := strings.TrimSpace(string(bytes))
	if len(token) < 32 {
		return nil, fmt.Errorf("history: operator token must have at least 32 characters")
	}
	want := sha256.Sum256([]byte("Bearer " + token))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remote, _, _ := net.SplitHostPort(r.RemoteAddr)
		host := r.Host
		if value, _, err := net.SplitHostPort(host); err == nil {
			host = value
		}
		a, b := net.ParseIP(remote), net.ParseIP(strings.Trim(host, "[]"))
		got := sha256.Sum256([]byte(r.Header.Get("Authorization")))
		forbidden := a == nil || !a.IsLoopback() || b == nil || !b.IsLoopback() || r.Header.Get("X-SRO-Local-Diagnostics") != "1" || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare(want[:], got[:]) != 1
		for key := range r.Header {
			if strings.EqualFold(key, "Forwarded") || strings.HasPrefix(strings.ToLower(key), "x-forwarded-") {
				forbidden = true
			}
		}
		if forbidden {
			http.Error(w, "operator authentication required", http.StatusForbidden)
			return
		}
		j.ServeHTTP(w, r)
	}), nil
}
