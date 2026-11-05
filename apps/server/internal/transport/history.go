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

	"github.com/gorilla/websocket"
	"github.com/quic-go/webtransport-go"
)

/*
================
LifecycleEvent

Transport facts cross the composition boundary without importing persistence.
================
*/
type LifecycleEvent struct {
	Session, Account, Shard, Character, Kind, Category, Code, Message string
	// Transport is the kind of the session's latest attachment
	// ("webtransport"/"websocket"); Fallback is the reason the client
	// reported for not using its first-choice transport ("" when none
	// was reported). Telemetry only (#1449).
	Transport, Fallback string
	Attached, InWorld   bool
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
ShutdownClose

Whether a session's close cause is the hub's shutdown (Hub.shutdown closes
every session with ByeReasonShutdown), not anything the player did.
================
*/
func ShutdownClose(cause error) bool {
	var bye CloseReason
	return errors.As(cause, &bye) && bye.Reason == ByeReasonShutdown
}

/*
================
ReplacedClose

Whether a session closed because a newer session of the same character
replaced it (a reload or reconnect: ByeReasonReplaced). The character
carries on in the new session; nobody left the game.
================
*/
func ReplacedClose(cause error) bool {
	var bye CloseReason
	return errors.As(cause, &bye) && bye.Reason == ByeReasonReplaced
}

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
	// A remote WebTransport session or stream error is the peer's close
	// capsule arriving: the client deliberately ended the session or its
	// control stream (reload, navigation, tab close). A dead path cannot
	// produce it - silence surfaces as the idle timeout instead - so it
	// is a goodbye, not a loss (#2148). Its message is empty for a plain
	// close, which is why these used to land in connection_closed with
	// no text at all. The WebSocket clean-close codes (1000, 1001) are
	// the same goodbye on the fallback lane; abnormal codes stay losses.
	var sessionErr *webtransport.SessionError
	if errors.As(cause, &sessionErr) && sessionErr.Remote {
		return "expected", "client_closed"
	}
	var streamErr *webtransport.StreamError
	if errors.As(cause, &streamErr) && streamErr.Remote {
		return "expected", "client_closed"
	}
	var closeErr *websocket.CloseError
	if errors.As(cause, &closeErr) &&
		(closeErr.Code == websocket.CloseNormalClosure || closeErr.Code == websocket.CloseGoingAway) {
		return "expected", "client_closed"
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
		Kind: kind, Category: category, Code: code, Message: fmt.Sprint(cause), Transport: s.transport, Fallback: s.fallback,
		Attached: s.attached, InWorld: s.WorldReady() && kind != "world_left"})
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
