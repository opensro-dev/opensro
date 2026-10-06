/*
===========================================================================
session_close.go - final session retirement, close metrics and observer hooks
===========================================================================
*/
package transport

import (
	"errors"
	"fmt"
	log "github.com/sirupsen/logrus"
)

// closeSession finishes a session exactly once: final teardown, registry
// removal, close hooks.
/*
================
closeSession
================
*/
func (h *Hub) closeSession(s *Session, cause error) {
	closed, cause := s.closeNow(cause)
	if !closed {
		return
	}
	h.mu.Lock()
	delete(h.sessions, s.ID)
	delete(h.byToken, s.resumeToken)
	if key, ok := h.bindingKeys[s.ID]; ok {
		delete(h.bindingKeys, s.ID)
		if h.bindings[key] == s {
			delete(h.bindings, key)
		}
	}
	if div, ok := h.sessionDiv[s.ID]; ok {
		delete(h.sessionDiv, s.ID)
		h.dropFromDivisionLocked(div, s.ID)
	}
	if account, ok := h.sessionAccount[s.ID]; ok {
		delete(h.sessionAccount, s.ID)
		delete(h.accountSessions[account], s.ID)
		if len(h.accountSessions[account]) == 0 {
			delete(h.accountSessions, account)
		}
	}
	h.mu.Unlock()

	h.metrics.sessClosed.Add(1)
	category, _ := classifyClose(cause)
	switch {
	case category == "expected":
		h.metrics.closedClean.Add(1)
	case errors.Is(cause, errGraceExpired):
		h.metrics.closedGrace.Add(1)
	case errors.Is(cause, errSlowConsumer):
		h.metrics.closedSlow.Add(1)
	case errors.Is(cause, errHandlerPanic):
		h.metrics.closedPanic.Add(1)
	default:
		h.metrics.closedOther.Add(1)
	}

	log.WithFields(log.Fields{"session": s.ID, "cause": fmt.Sprint(cause)}).
		Info("transport: session closed")
	for _, fn := range h.hooks.closeSnapshot() {
		func() {
			defer recoverHookPanic(s, "OnSessionClose")
			fn(s, cause)
		}()
	}
}
