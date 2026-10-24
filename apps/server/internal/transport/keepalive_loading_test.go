/*
===========================================================================

keepalive_loading_test.go - keepalive liveness while a scene is loading

World entry can sit silent for seconds (publication gate, bootstrap build)
while the client admits it under a stall deadline. The keepalive owns the
wire's liveness proof in that window: it must ping a loading session on the
short loading cadence even though the client is quiet, and stay on the
plain idle cadence for a live one.

===========================================================================
*/
package transport

import (
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

/*
================
countPings
================
*/
func countPings(fc *fakeConn) int {
	pings := 0
	for _, f := range fc.written() {
		if f.Opcode == OpPing {
			pings++
		}
	}
	return pings
}

/*
================
TestKeepalivePingsWhileSceneIsLoading

The keepalive interval is an hour, so the idle rule alone would never ping
inside an admission window: only the loading cadence can.
================
*/
func TestKeepalivePingsWhileSceneIsLoading(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	sess.BeginSceneAdmission()
	wait.Eventually(t, 4*time.Second, "a keepalive PING during scene loading", func() bool {
		return countPings(fc) >= 1
	})
	wait.Eventually(t, 4*time.Second, "a second keepalive PING during scene loading", func() bool {
		return countPings(fc) >= 2
	})
}

/*
================
TestKeepaliveStaysQuietWhileSceneIsLive

A live (not loading) quiet session is only pinged once it has been idle
for the keepalive interval; with the interval an hour out, the short
loading cadence must not leak into it.
================
*/
func TestKeepaliveStaysQuietWhileSceneIsLive(t *testing.T) {
	_, _, fc := attachedSession(t, false)
	wait.Consistently(t, 3*time.Second, "no keepalive PING for a live session", func() bool {
		return countPings(fc) == 0
	})
}

/*
================
TestKeepaliveReturnsToIdleCadenceAfterLoading

Once the scene is live the short loading cadence ends with it: the wire
goes back to the plain idle rule, however chatty it was during the entry.
One tick may already have read the loading flag when the flip lands, so
a single extra ping is tolerated; a second one is a leaked cadence.
================
*/
func TestKeepaliveReturnsToIdleCadenceAfterLoading(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	sess.BeginSceneAdmission()
	wait.Eventually(t, 4*time.Second, "a keepalive PING during scene loading", func() bool {
		return countPings(fc) >= 1
	})
	wait.Eventually(t, 4*time.Second, "a second keepalive PING during scene loading", func() bool {
		return countPings(fc) >= 2
	})
	if !sess.FinishSceneReentry() {
		t.Fatal("FinishSceneReentry refused a loading session")
	}
	before := countPings(fc)
	wait.Consistently(t, 4*time.Second, "no leaked loading cadence after scene load", func() bool {
		return countPings(fc) <= before+1
	})
}
