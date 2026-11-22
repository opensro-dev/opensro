/*
===========================================================================

conn_wt_goodbye_test.go - a WebTransport goodbye reaches the browser

A session the server ends politely queues a BYE behind whatever it was
still sending. Live, the BYE never arrived: the WebTransport session
close reset the control stream under it, and every Exit, Restart,
replaced tab and server restart filed as a lost connection (#4115).
These tests end a session while a burst larger than QUIC's first
congestion window is still in flight, over real loopback sockets, and
require the client to read every frame and then the BYE.

===========================================================================
*/
package transport_test

import (
	"context"
	"testing"
	"time"

	"github.com/quic-go/webtransport-go"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

const (
	// goodbyeBurstOp asks the test lane for a burst and then a polite close.
	goodbyeBurstOp uint16 = 0x706E
	// goodbyeDataOp carries the burst.
	goodbyeDataOp uint16 = 0xB06E
	// goodbyeBurstFrames x goodbyeFrameBytes is 256 KB: several times
	// QUIC's initial congestion window, so most of it is still unsent
	// when the session closes.
	goodbyeBurstFrames = 16
	goodbyeFrameBytes  = 16 << 10
)

/*
================
startGoodbyeServer

A smoke server whose burst lane sends goodbyeBurstFrames frames and then
ends the session with reason, the way Exit and Restart do
(departure.go CloseWhenDrained(ByeReasonNormal)).
================
*/
func startGoodbyeServer(t *testing.T, reason uint8) *transport.Server {
	t.Helper()
	srv := startServer(t)
	payload := make([]byte, goodbyeFrameBytes)
	srv.Hub.Handle(goodbyeBurstOp, func(s *transport.Session, _ uint16, _ []byte) {
		for range goodbyeBurstFrames {
			s.Send(goodbyeDataOp, payload)
		}
		s.CloseWhenDrained(reason)
	})
	return srv
}

/*
================
readBurstThenBye

Reads the whole burst and returns the BYE's reason; fails the test if
the stream ends first.
================
*/
func readBurstThenBye(t *testing.T, str *webtransport.Stream) uint8 {
	t.Helper()
	got := 0
	for {
		f := readWTFrame(t, str)
		switch f.Opcode {
		case goodbyeDataOp:
			got++
		case transport.OpBye:
			if got != goodbyeBurstFrames {
				t.Fatalf("BYE after %d of %d burst frames", got, goodbyeBurstFrames)
			}
			if len(f.Payload) != 1 {
				t.Fatalf("BYE payload %x", f.Payload)
			}
			return f.Payload[0]
		}
	}
}

/*
================
TestWebTransportPoliteCloseDeliversTheBye
================
*/
func TestWebTransportPoliteCloseDeliversTheBye(t *testing.T) {
	srv := startGoodbyeServer(t, transport.ByeReasonNormal)
	sess, str := dialWT(t, srv)
	helloWT(t, str, nil)
	if err := transport.WriteStreamFrame(str, transport.Frame{Opcode: goodbyeBurstOp}); err != nil {
		t.Fatal(err)
	}
	if reason := readBurstThenBye(t, str); reason != transport.ByeReasonNormal {
		t.Fatalf("BYE reason %d, want %d", reason, transport.ByeReasonNormal)
	}
	// The browser closes on the BYE (network.ts end()); the server's
	// session must then end promptly, not sit out its linger.
	start := time.Now()
	_ = sess.CloseWithError(0, "bye read")
	select {
	case <-sess.Context().Done():
	case <-time.After(smokeIOTimeout):
		t.Fatal("session still open after the client closed it")
	}
	if waited := time.Since(start); waited > time.Second {
		t.Fatalf("client close took %v", waited)
	}
	// /transport/metrics names the cause, so a regression shows (#4115).
	wait.Eventually(t, smokeLifecycleTimeout, "ended.client_logout counted", func() bool {
		return srv.Hub.Metrics().CloseCauses["ended.client_logout"] == 1
	})
}

/*
================
TestWebTransportShutdownDeliversTheBye

A server restart drains every session with ByeReasonShutdown; the
listener must stay up until each BYE has reached its client.
================
*/
func TestWebTransportShutdownDeliversTheBye(t *testing.T) {
	srv := startServer(t)
	payload := make([]byte, goodbyeFrameBytes)
	queued := make(chan struct{})
	srv.Hub.Handle(goodbyeBurstOp, func(s *transport.Session, _ uint16, _ []byte) {
		for range goodbyeBurstFrames {
			s.Send(goodbyeDataOp, payload)
		}
		close(queued)
	})
	_, str := dialWT(t, srv)
	helloWT(t, str, nil)
	if err := transport.WriteStreamFrame(str, transport.Frame{Opcode: goodbyeBurstOp}); err != nil {
		t.Fatal(err)
	}
	<-queued
	ctx, cancel := context.WithTimeout(context.Background(), smokeLifecycleTimeout)
	defer cancel()
	shutdown := make(chan struct{})
	go func() {
		srv.Shutdown(ctx)
		close(shutdown)
	}()
	if reason := readBurstThenBye(t, str); reason != transport.ByeReasonShutdown {
		t.Fatalf("BYE reason %d, want %d", reason, transport.ByeReasonShutdown)
	}
	<-shutdown
}
