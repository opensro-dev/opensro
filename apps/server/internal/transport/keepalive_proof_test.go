/*
===========================================================================

keepalive_proof_test.go - the live wire proves itself on outbound silence

A live session in a quiet world sends nothing for long stretches, and
the server's plain keepalive only speaks after the client itself has
been silent for the whole keepalive interval - far outside the
client's stall window, which fires after seconds of inbound silence
(#2148). The keepalive therefore also proves the wire when nothing
has been written for a while, whatever the client is sending.

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
TestKeepaliveProvesLiveWireOnOutboundSilence

The keepalive interval is an hour and the client says nothing, so the
idle rule alone would never ping: only the outbound-silence proof can.
The proof repeats while the silence lasts. The proof interval is scaled
down (testProofInterval) so the test fits the package's time budget; the
production default is pinned separately.
================
*/
func TestKeepaliveProvesLiveWireOnOutboundSilence(t *testing.T) {
	cfg := testCfg()
	cfg.WireProofInterval = testProofInterval
	hub := newHub(cfg)
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(false)
	if err := sess.attach(fc, false, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	wait.Eventually(t, 3*time.Second, "a liveness PING on a live outbound-silent wire", func() bool {
		return countPings(fc) >= 1
	})
	wait.Eventually(t, 3*time.Second, "a second liveness PING while the silence lasts", func() bool {
		return countPings(fc) >= 2
	})
}

// testProofInterval is the scaled-down wire proof interval of the test above.
const testProofInterval = 50 * time.Millisecond

/*
================
TestWireProofDefaultFitsTheClientStallWindow

The default proof interval must land a PING inside the client's stall
window (TRANSPORT_STALL_MS 7 s + TRANSPORT_STALL_CONFIRM_MS 1 s) even when
the keepalive's two-second tick rounds it up.
================
*/
func TestWireProofDefaultFitsTheClientStallWindow(t *testing.T) {
	const clientStallWindow = 8 * time.Second
	if got := DefaultConfig().WireProofInterval; got <= 0 || got+sceneLoadingPingInterval >= clientStallWindow {
		t.Fatalf("WireProofInterval = %v: a proof could land after the client's %v stall window", got, clientStallWindow)
	}
}
