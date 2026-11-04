/*
===========================================================================

conn_wt_test.go - the WebTransport datagram lane never blocks a caller

#2142: WriteUnreliable used to call the QUIC datagram send directly,
and that send blocks once the peer stops draining its queue, so a
monster leg publishing movement from inside a division lane parked
there for 4.7 s. These tests stand fake peers in for the QUIC session
through the sendDatagram field: a peer that never reads cannot make
WriteUnreliable block, a full queue drops and says so, frames keep
their order while the peer drains, and the drain goroutine exits when
the connection closes. No test sleeps; every wait is a channel with a
deadline, so a regression fails fast instead of hanging the suite.

===========================================================================
*/
package transport

import (
	"bytes"
	"errors"
	"testing"
	"time"
)

// testWTDeadline bounds every wait in this file. Nothing here should
// take anywhere near it; it exists so a blocking regression reports
// itself by name instead of stalling the package.
const testWTDeadline = 10 * time.Second

/*
================
newTestWTConn

A wtConn with no QUIC session behind it: send stands in for the
session's datagram send, the outbound queue holds queueLen encoded
frames, and the drain goroutine is already running, exactly as
newWTConn leaves one. Closing w.closed stands in for Close.
================
*/
func newTestWTConn(queueLen int, send func([]byte) error) *wtConn {
	w := &wtConn{
		closed:       make(chan struct{}),
		dgrams:       make(chan []byte, queueLen),
		dgramDone:    make(chan struct{}),
		sendDatagram: send,
	}
	go w.datagramDrain()
	return w
}

/*
================
writeUnreliableWithin

Runs one WriteUnreliable and returns its error, failing the test if
the call does not return within testWTDeadline.
================
*/
func writeUnreliableWithin(t *testing.T, w *wtConn, f Frame) error {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- w.WriteUnreliable(f) }()
	select {
	case err := <-done:
		return err
	case <-time.After(testWTDeadline):
		t.Fatal("WriteUnreliable blocked on its peer")
		return nil
	}
}

/*
================
TestWTWriteUnreliableNeverBlocksOnAStalledPeer

The #2142 regression test: the peer never reads, so the drain parks
inside the QUIC send. WriteUnreliable must still return at once for
every frame, and once the bounded queue is full it must report the
drop (errDatagramDropped) rather than wait for room.
================
*/
func TestWTWriteUnreliableNeverBlocksOnAStalledPeer(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	w := newTestWTConn(2, func([]byte) error {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release // the peer never reads; the send parks until released
		return nil
	})

	frame := Frame{Opcode: OpObjectSourceMove, Payload: []byte{0x01}}
	if err := writeUnreliableWithin(t, w, frame); err != nil {
		t.Fatalf("first write: %v", err)
	}
	// The drain has taken the first frame and is parked in the send;
	// from here the queue alone absorbs new frames.
	select {
	case <-started:
	case <-time.After(testWTDeadline):
		t.Fatal("drain never picked up the first frame")
	}
	for i := 0; i < 2; i++ {
		if err := writeUnreliableWithin(t, w, frame); err != nil {
			t.Fatalf("queued write %d: %v", i, err)
		}
	}
	if err := writeUnreliableWithin(t, w, frame); !errors.Is(err, errDatagramDropped) {
		t.Fatalf("full-queue write = %v, want errDatagramDropped", err)
	}

	close(release)
	close(w.closed)
	select {
	case <-w.dgramDone:
	case <-time.After(testWTDeadline):
		t.Fatal("drain did not exit on close")
	}
}

/*
================
TestWTWriteUnreliableKeepsOrderWhileThePeerDrains

While the peer keeps reading, every frame reaches the datagram send
in the order it was written.
================
*/
func TestWTWriteUnreliableKeepsOrderWhileThePeerDrains(t *testing.T) {
	sent := make(chan []byte, 16)
	w := newTestWTConn(8, func(buf []byte) error {
		sent <- append([]byte(nil), buf...)
		return nil
	})

	const frames = 8
	for i := 0; i < frames; i++ {
		if err := w.WriteUnreliable(Frame{Opcode: OpObjectSourceMove, Payload: []byte{byte(i)}}); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	for i := 0; i < frames; i++ {
		select {
		case buf := <-sent:
			f, err := DecodeFrame(buf)
			if err != nil {
				t.Fatalf("sent frame %d does not decode: %v", i, err)
			}
			if f.Opcode != OpObjectSourceMove || !bytes.Equal(f.Payload, []byte{byte(i)}) {
				t.Fatalf("sent frame %d = opcode 0x%04X payload %v, want sequence %d in order", i, f.Opcode, f.Payload, i)
			}
		case <-time.After(testWTDeadline):
			t.Fatalf("only %d of %d frames reached the peer", i, frames)
		}
	}

	close(w.closed)
	select {
	case <-w.dgramDone:
	case <-time.After(testWTDeadline):
		t.Fatal("drain did not exit on close")
	}
}

/*
================
TestWTWriteUnreliableAfterClose

A closed connection refuses the datagram lane at once and its drain
goroutine is gone: no goroutine leaks onto a dead peer.
================
*/
func TestWTWriteUnreliableAfterClose(t *testing.T) {
	w := newTestWTConn(2, func([]byte) error { return nil })
	close(w.closed)
	select {
	case <-w.dgramDone:
	case <-time.After(testWTDeadline):
		t.Fatal("drain did not exit on close")
	}
	if err := w.WriteUnreliable(Frame{Opcode: OpObjectSourceMove, Payload: []byte{0x01}}); !errors.Is(err, errWTConnClosed) {
		t.Fatalf("write after close = %v, want errWTConnClosed", err)
	}
}
