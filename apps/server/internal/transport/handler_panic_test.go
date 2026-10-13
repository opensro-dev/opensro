/*
===========================================================================

handler_panic_test.go - a panicking handler leaves its stack in the log

A player's shop request (0x7338) closed the session with "handler panic …
nil pointer dereference" on production, and no stack reached the log. A
handler panic must leave the panicking frame in the log line that closes
the session.

===========================================================================
*/
package transport

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	log "github.com/sirupsen/logrus"
	"opensro.online/server/internal/platform/logging"
)

/*
================
lockedLog

The captured log output. logrus locks its own writes but not a reader, and
a goroutine of another test may still be logging while this one reads.
================
*/
type lockedLog struct {
	mu  sync.Mutex
	out bytes.Buffer
}

/*
================
lockedLog.Write
================
*/
func (l *lockedLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.out.Write(p)
}

/*
================
lockedLog.String
================
*/
func (l *lockedLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.out.String()
}

// panicProbe is the type whose nil method the probe handler calls.
type panicProbe struct{ value int }

/*
================
nilDereferencingHandler
================
*/
func nilDereferencingHandler(*Session, uint16, []byte) {
	var p *panicProbe
	_ = p.value
}

/*
================
TestHandlerPanicLogsItsStack
================
*/
func TestHandlerPanicLogsItsStack(t *testing.T) {
	var out lockedLog
	log.SetOutput(&out)
	log.SetFormatter(logging.Formatter())
	t.Cleanup(logging.InstallFormat)
	hub, s, _ := limitedSession(t, rlCfg(1000, 1000))
	hub.Handle(0x7338, nilDereferencingHandler)
	hub.dispatch(s, Frame{Opcode: 0x7338})
	text := out.String()
	if !strings.Contains(text, "handler panicked") || !strings.Contains(text, "nilDereferencingHandler") {
		t.Fatalf("the closing log line carries no stack of the panicking handler:\n%s", text)
	}
	// The close line alone names the site: production kept it when the
	// stack line was missing.
	closing := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "session closed") {
			closing = line
		}
	}
	if !strings.Contains(closing, "transport.nilDereferencingHandler") || !strings.Contains(closing, "handler_panic_test.go:") {
		t.Fatalf("the session-closed line does not name the panic site: %q", closing)
	}
	if _, ok := hub.Session(s.ID); ok {
		t.Fatal("the panicking session stayed open")
	}
}
