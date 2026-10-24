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
