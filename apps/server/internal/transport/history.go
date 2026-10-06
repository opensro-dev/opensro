/*
===========================================================================

history.go - transport lifecycle facts and preserved termination reasons

Records are queued under the session lock, in transition order. No disk IO
or caller callback occurs there. World visibility and reconnect are distinct.

===========================================================================
*/
package transport

import (
	"errors"
	"fmt"
	"strconv"
)

/*
================
LifecycleEvent

Transport facts cross the composition boundary without importing persistence.
================
*/
type LifecycleEvent struct {
	Session, Account, Shard, Character, Kind, Category, Code, Message string
	Attached, InWorld                                                 bool
}

/*
================
HistoryObserver

Installed before admission. RecordLifecycle must enqueue without blocking.
================
*/
type HistoryObserver interface {
	SessionID(string) string
	RecordLifecycle(LifecycleEvent)
}

/*
================
CloseReason
================
*/
type CloseReason struct{ Reason uint8 }

/*
================
Error
================
*/
func (r CloseReason) Error() string { return fmt.Sprintf("transport: explicit goodbye %d", r.Reason) }

/*
================
classifyClose
================
*/
func classifyClose(cause error) (string, string) {
	var bye CloseReason
	if errors.As(cause, &bye) {
		switch bye.Reason {
		case ByeReasonNormal:
			return "expected", "client_logout"
		case ByeReasonShutdown:
			return "expected", "server_shutdown"
		case ByeReasonReplaced:
			return "expected", "session_replaced"
		case ByeReasonUnauthorized:
			return "expected", "authorization_refused"
		case ByeReasonServerBusy:
			return "connection", "server_busy"
		case ByeReasonProtocolErr:
			return "unknown", "protocol_refused"
		default:
			return "unknown", "explicit_close"
		}
	}
	switch {
	case errors.Is(cause, errHandlerPanic):
		return "software", "handler_panic"
	case errors.Is(cause, errOutboundBurstTooLarge), errors.Is(cause, ErrFrameTooLarge):
		return "software", "server_output_invalid"
	case errors.Is(cause, errSlowConsumer):
		return "connection", "outbound_queue_overflow"
	case errors.Is(cause, errGraceExpired):
		return "connection", "resume_grace_expired"
	case errors.Is(cause, errIdle):
		return "connection", "keepalive_timeout"
	default:
		return "unknown", "connection_closed"
	}
}

/*
================
recordHistoryLocked
================
*/
func (s *Session) recordHistoryLocked(kind string, cause error) {
	j := s.hub.History
	if j == nil {
		return
	}
	account, shard, _ := s.AdmissionIdentity()
	_, character, _ := s.CharacterBinding()
	category, code := classifyClose(cause)
	if kind != "ended" && kind != "detached" {
		category = "expected"
		code = kind
	}
	j.RecordLifecycle(LifecycleEvent{Session: j.SessionID(strconv.FormatUint(s.ID, 10)), Account: account, Shard: shard, Character: character,
		Kind: kind, Category: category, Code: code, Message: fmt.Sprint(cause), Attached: s.attached, InWorld: s.WorldReady() && kind != "world_left"})
}

/*
================
DiagnosticSessionID
================
*/
func (s *Session) DiagnosticSessionID() string {
	if s.hub.History == nil {
		return ""
	}
	return s.hub.History.SessionID(strconv.FormatUint(s.ID, 10))
}
