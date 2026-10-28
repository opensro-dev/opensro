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

/*
================
TestKeepalivePingDueToleratesALateTick

The ticker's ticks land on its schedule, not a fixed gap after the last
one: a tick 300 ms late is followed by one only 1.7 s later. That tick
still owes the loading ping; the cadence must not double to 4 s. A tick
well short of the cadence still waits.
================
*/
func TestKeepalivePingDueToleratesALateTick(t *testing.T) {
	tick := sceneLoadingPingInterval
	if !keepalivePingDue(1700*time.Millisecond, sceneLoadingPingInterval, tick) {
		t.Fatal("a tick 1.7 s after a late ping skipped the 2 s loading cadence")
	}
	if !keepalivePingDue(sceneLoadingPingInterval, sceneLoadingPingInterval, tick) {
		t.Fatal("an on-time tick skipped the loading cadence")
	}
	if keepalivePingDue(500*time.Millisecond, sceneLoadingPingInterval, tick) {
		t.Fatal("a tick half a second after the last ping pinged again")
	}
	idle := 20 * time.Second
	if keepalivePingDue(10*time.Second, idle, tick) || !keepalivePingDue(19500*time.Millisecond, idle, tick) {
		t.Fatal("the idle cadence lost its bound")
	}
}
