/*
===========================================================================

session_ringpark_test.go - the bounded writer park on a full resume ring

A burst that outruns the client's applier parks the writer at the ring
bound (#2146). The park is now itself bounded: past RingParkBound with
no stream-ack progress the keepalive watchdog detaches the session, so
the grace window and a resume take over instead of the burst holding
the session forever. A client that keeps acking is never cut.

===========================================================================
*/
package transport

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

/*
================
parkBoundSession

An attached session on a hub whose park bound the test drives by
hand: the hub runs on a manual clock (#2445), so the watchdog sees
time pass only when the test advances it. The outbound queue is
oversized: a resume over a full ring re-queues the whole ring beside
what is still queued, and this test's own Sends should never be the
thing that trips the producer bound.
================
*/
func parkBoundSession(t *testing.T, bound time.Duration) (*Hub, *Session, *fakeConn, *manualClock) {
	t.Helper()
	cfg := testCfg()
	cfg.KeepaliveInterval = 20 * time.Millisecond
	cfg.RingParkBound = bound
	cfg.OutboundQueue = 16384
	hub, clock := manualClockHub(t, cfg)
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	fc := newFakeConn(false)
	if err := sess.attach(fc, false, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	wait.Eventually(t, 5*time.Second, "the keepalive loop holds its ticker", func() bool {
		return clock.tickerCount() == 1
	})
	return hub, sess, fc, clock
}

/*
================
bytePayloads

n one-byte burst payloads.
================
*/
func bytePayloads(n int) [][]byte {
	payloads := make([][]byte, n)
	for i := range payloads {
		payloads[i] = []byte{7}
	}
	return payloads
}

/*
================
sendStreamAck

Presents a stream ack at pos on the connection's inbound side, as the
client's network owner does once it has applied that far.
================
*/
func sendStreamAck(fc *fakeConn, pos uint32) {
	ack := make([]byte, 4)
	binary.LittleEndian.PutUint32(ack, pos)
	fc.inbound <- Frame{Opcode: OpStreamAck, Payload: ack}
}

/*
================
TestRingParkedWriterDetachesAtBoundAndResumeRuns

The #2146 stall, bounded: the client applies the first frames of a
ring-filling burst, then its applier wedges. The writer parks on the
refilled ring; past the park bound the session must detach into its
grace window (counted, not silent), and a resume inside the window
must be served the unapplied tail by the ordinary ring machinery.
================
*/
func TestRingParkedWriterDetachesAtBoundAndResumeRuns(t *testing.T) {
	// The bound is a second of manual time: the watchdog cannot
	// mistake scheduler lag for a wedged applier, because its clock
	// moves only when this test advances it (#2445).
	hub, sess, fc, clock := parkBoundSession(t, time.Second)
	const extra = 64
	const applied = streamRingMaxFrames / 8

	// The burst fills the ring and parks; the client applies the
	// first frames and the writer drains its queued tail.
	sendBurst(t, sess, fc, bytePayloads(streamRingMaxFrames+extra))
	writtenCount(t, fc, streamRingMaxFrames)
	sendStreamAck(fc, applied)
	wait.Eventually(t, 5*time.Second, "writer drains the queued tail after the ack", func() bool {
		return len(dataFrames(fc.written())) == streamRingMaxFrames+extra
	})

	// A second burst refills the ring and parks again - and this
	// time no ack ever comes. A full ring on top of the applied
	// prefix means applied+streamRingMaxFrames written in all.
	// Advancing the clock past the bound is what arms the watchdog.
	sendBurst(t, sess, fc, bytePayloads(applied))
	writtenCount(t, fc, applied+streamRingMaxFrames)
	clock.Advance(2 * time.Second)

	detached(t, sess)
	wait.Eventually(t, 3*time.Second, "the park detach is counted on its own counter", func() bool {
		return hub.metrics.detachesRingPark.Load() == 1
	})
	if got := hub.metrics.detaches.Load(); got != 1 {
		t.Fatalf("detaches = %d, want 1 (the park detach)", got)
	}

	// The grace window is running, not a close: a resume at the
	// applied position is served the whole unapplied tail - the
	// full ring plus what was still queued behind it.
	second := newFakeConn(false)
	if err := sess.attach(second, true, applied); err != nil {
		t.Fatalf("resume after the park detach: %v", err)
	}
	total := streamRingMaxFrames + extra
	for delivered := 0; delivered < total; {
		wait.Eventually(t, 5*time.Second, "re-send advances", func() bool {
			return len(dataFrames(second.written())) > delivered
		})
		delivered = len(dataFrames(second.written()))
		sendStreamAck(second, uint32(applied+delivered))
	}
	if got := len(dataFrames(second.written())); got != total {
		t.Fatalf("re-sent %d frames, want %d", got, total)
	}
	// The resumed writer, acked as it goes, is never park-detached.
	if got := hub.metrics.detachesRingPark.Load(); got != 1 {
		t.Fatalf("detaches_ring_park = %d, want still 1 after the resumed delivery", got)
	}
}

/*
================
TestRingParkAckingPeerIsNotCut

The other half of the contract: a client that keeps applying - acks
advancing again and again across several park bounds while the ring
stays at its bound under fresh sends - is never detached. Only the
absence of ack progress arms the bound.
================
*/
func TestRingParkAckingPeerIsNotCut(t *testing.T) {
	// Manual time below advances eight bounds in all; only the acks
	// landing before each advance keep the peer alive, so the span
	// needs no wall-clock headroom on any machine (#2445).
	hub, sess, fc, clock := parkBoundSession(t, 3*time.Second)
	const extra = 64
	sendBurst(t, sess, fc, bytePayloads(streamRingMaxFrames+extra))
	writtenCount(t, fc, streamRingMaxFrames)

	// Refill as fast as the acks free the ring, for longer than the
	// bound: the ring sits at its bound under pressure the whole
	// time, but ack progress never stops. Each cycle waits until
	// the ack has actually landed (the applied position reaches
	// it) before letting time pass, so the watchdog always judges
	// a fresh ack stamp, never scheduler lag.
	const cycles = 32
	const perCycle = 512
	acked := uint32(0)
	sent := streamRingMaxFrames + extra
	for i := 0; i < cycles; i++ {
		for j := 0; j < perCycle; j++ {
			if err := sess.Send(ringTestOp, []byte{7}); err != nil {
				t.Fatal(err)
			}
		}
		sent += perCycle
		acked += perCycle
		sendStreamAck(fc, acked)
		wait.Eventually(t, 5*time.Second, "the cycle's ack lands before time passes", func() bool {
			sess.mu.Lock()
			defer sess.mu.Unlock()
			return sess.streamPos-uint32(len(sess.ring)) >= acked
		})
		clock.Advance(750 * time.Millisecond)
	}
	// One more tick proves the last advance's checks have run.
	clock.Advance(0)

	sess.mu.Lock()
	attached := sess.attached
	sess.mu.Unlock()
	if !attached {
		t.Fatal("a peer still acking was detached at the park bound")
	}
	if got := hub.metrics.detaches.Load(); got != 0 {
		t.Fatalf("detaches = %d, want 0 while acks advance", got)
	}

	// Ack the rest: the whole stream must land.
	sess.mu.Lock()
	head := sess.streamPos
	sess.mu.Unlock()
	sendStreamAck(fc, head)
	wait.Eventually(t, 5*time.Second, "the acked stream fully delivered", func() bool {
		return len(dataFrames(fc.written())) == sent
	})
}
