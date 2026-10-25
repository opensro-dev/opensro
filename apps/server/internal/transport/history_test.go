/*
===========================================================================
history_test.go - expected shutdowns remain distinct from software failures
===========================================================================
*/
package transport

import (
	"errors"
	"fmt"
	"testing"
)

/*
================
TestCloseClassificationRequiresEvidence
================
*/
func TestCloseClassificationRequiresEvidence(t *testing.T) {
	for _, row := range []struct {
		cause          error
		category, code string
	}{
		{nil, "unknown", "connection_closed"},
		{CloseReason{ByeReasonShutdown}, "expected", "server_shutdown"},
		{CloseReason{ByeReasonReplaced}, "expected", "session_replaced"},
		{fmt.Errorf("wrapped: %w", errHandlerPanic), "software", "handler_panic"},
		{errGraceExpired, "connection", "resume_grace_expired"},
	} {
		category, code := classifyClose(row.cause)
		if category != row.category || code != row.code {
			t.Fatalf("%v: %s/%s", row.cause, category, code)
		}
	}
}

/*
================
recordingHistory

A HistoryObserver that keeps every lifecycle event for inspection.
================
*/
type recordingHistory struct{ events []LifecycleEvent }

/*
================
SessionID
================
*/
func (r *recordingHistory) SessionID(id string) string { return "test:" + id }

/*
================
RecordLifecycle
================
*/
func (r *recordingHistory) RecordLifecycle(e LifecycleEvent) { r.events = append(r.events, e) }

/*
================
byKind
================
*/
func (r *recordingHistory) byKind(kind string) (LifecycleEvent, bool) {
	for _, e := range r.events {
		if e.Kind == kind {
			return e, true
		}
	}
	return LifecycleEvent{}, false
}

/*
================
TestLifecycleRecordsTransportAndFallback

An attach records the connection's transport, a client report adds the
fallback reason as its own row, and a later attach replaces both: the
history describes how the session is attached now, not how it once
arrived (#1449).
================
*/
func TestLifecycleRecordsTransportAndFallback(t *testing.T) {
	h := newHub(testCfg())
	history := &recordingHistory{}
	h.History = history
	s, err := h.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(false)
	fc.kind = "websocket"
	if err := s.attach(fc, false, 0); err != nil {
		t.Fatal(err)
	}
	connected, ok := history.byKind("connected")
	if !ok || connected.Transport != "websocket" || connected.Fallback != "" {
		t.Fatalf("connected event = %+v", connected)
	}
	s.noteTransportReport("timeout")
	reported, ok := history.byKind("transport_reported")
	if !ok || reported.Transport != "websocket" || reported.Fallback != "timeout" {
		t.Fatalf("transport_reported event = %+v", reported)
	}
	second := newFakeConn(false)
	second.kind = "webtransport"
	if err := s.attach(second, true, 0); err != nil {
		t.Fatal(err)
	}
	resumed, ok := history.byKind("resumed")
	if !ok || resumed.Transport != "webtransport" || resumed.Fallback != "" {
		t.Fatalf("resumed event = %+v", resumed)
	}
	h.closeSession(s, nil)
}

/*
================
TestDrainClosePreservesReasonForHooksAndMetrics
================
*/
func TestDrainClosePreservesReasonForHooksAndMetrics(t *testing.T) {
	h := newHub(testCfg())
	s, err := h.createSession()
	if err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	s.closeReason = &CloseReason{ByeReasonShutdown}
	s.mu.Unlock()
	var received error
	h.OnSessionClose(func(_ *Session, cause error) { received = cause })
	h.closeSession(s, nil)
	var reason CloseReason
	if !errors.As(received, &reason) || reason.Reason != ByeReasonShutdown {
		t.Fatalf("lost shutdown reason: %v", received)
	}
	if h.metrics.closedClean.Load() != 1 || h.metrics.closedOther.Load() != 0 {
		t.Fatal("expected shutdown counted as unknown")
	}
}

/*
================
TestShutdownClose

Only the hub's shutdown goodbye is a shutdown: a logout, a replaced
session, a dropped socket and a wrapped goodbye are told apart.
================
*/
func TestShutdownClose(t *testing.T) {
	cases := []struct {
		cause error
		want  bool
	}{
		{CloseReason{Reason: ByeReasonShutdown}, true},
		{fmt.Errorf("closing: %w", CloseReason{Reason: ByeReasonShutdown}), true},
		{CloseReason{Reason: ByeReasonNormal}, false},
		{CloseReason{Reason: ByeReasonReplaced}, false},
		{errors.New("read: connection reset"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := ShutdownClose(tc.cause); got != tc.want {
			t.Fatalf("ShutdownClose(%v) = %v", tc.cause, got)
		}
	}
}

/*
================
TestReplacedClose

Only a replacing session's goodbye is a replacement: a logout, a
shutdown, a dropped socket and a wrapped goodbye are told apart.
================
*/
func TestReplacedClose(t *testing.T) {
	cases := []struct {
		cause error
		want  bool
	}{
		{CloseReason{Reason: ByeReasonReplaced}, true},
		{fmt.Errorf("closing: %w", CloseReason{Reason: ByeReasonReplaced}), true},
		{CloseReason{Reason: ByeReasonNormal}, false},
		{CloseReason{Reason: ByeReasonShutdown}, false},
		{errors.New("read: connection reset"), false},
		{nil, false},
	}
	for _, tc := range cases {
		if got := ReplacedClose(tc.cause); got != tc.want {
			t.Fatalf("ReplacedClose(%v) = %v", tc.cause, got)
		}
	}
}
