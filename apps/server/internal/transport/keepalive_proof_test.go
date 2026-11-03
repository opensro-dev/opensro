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
The proof repeats while the silence lasts.
================
*/
func TestKeepaliveProvesLiveWireOnOutboundSilence(t *testing.T) {
	_, _, fc := attachedSession(t, false)
	wait.Eventually(t, 7*time.Second, "a liveness PING on a live outbound-silent wire", func() bool {
		return countPings(fc) >= 1
	})
	wait.Eventually(t, 9*time.Second, "a second liveness PING while the silence lasts", func() bool {
		return countPings(fc) >= 2
	})
}
