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
