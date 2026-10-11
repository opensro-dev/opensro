/*
===========================================================================

emptyframe_test.go - an opcode-0 frame never reaches the client

===========================================================================
*/
package transport

import (
	"errors"
	"strings"
	"testing"
)

/*
================
newEmptyFrameSession
================
*/
func newEmptyFrameSession(t *testing.T) (*Hub, *Session, *fakeConn) {
	t.Helper()
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	connection := newFakeConn(true)
	if err := session.attach(connection, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	return hub, session, connection
}

/*
================
TestSendRefusesEmptyFrame
================
*/
func TestSendRefusesEmptyFrame(t *testing.T) {
	hub, session, connection := newEmptyFrameSession(t)

	if err := session.Send(0, nil); !errors.Is(err, ErrEmptyFrame) {
		t.Fatalf("Send(0) = %v, want ErrEmptyFrame", err)
	}
	if err := session.Send(0x3013, []byte{1}); err != nil {
		t.Fatalf("session refused traffic after an empty frame: %v", err)
	}
	if got := hub.Metrics().EmptyFramesRefused; got != 1 {
		t.Fatalf("EmptyFramesRefused = %d, want 1", got)
	}

	connection.openGate()
	written := waitWritten(t, connection, 2)
	for _, frame := range written {
		if frame.Opcode == 0 {
			t.Fatal("an empty frame reached the connection")
		}
	}
	if written[1].Opcode != 0x3013 {
		t.Fatalf("frame after WELCOME = 0x%04X, want 0x3013", written[1].Opcode)
	}
}

/*
================
TestBatchDropsOnlyItsEmptyFrames

The #618 shape: a cast's burst with an empty vitals frame in the middle.
The cast's real frames still go out, in order; the empty one does not.
================
*/
func TestBatchDropsOnlyItsEmptyFrames(t *testing.T) {
	hub, session, connection := newEmptyFrameSession(t)

	batch := []Frame{
		{Opcode: 0xB070, Payload: []byte{1}},
		{},
		{Opcode: 0xB071, Payload: []byte{2}},
		{},
	}
	if err := session.SendBatch(batch); err != nil {
		t.Fatalf("SendBatch: %v", err)
	}
	if err := session.SendBatch([]Frame{{}}); err != nil {
		t.Fatalf("an all-empty batch failed the session: %v", err)
	}
	if got := hub.Metrics().EmptyFramesRefused; got != 3 {
		t.Fatalf("EmptyFramesRefused = %d, want 3", got)
	}

	connection.openGate()
	written := waitWritten(t, connection, 3)
	want := []uint16{OpWelcome, 0xB070, 0xB071}
	for index, opcode := range want {
		if written[index].Opcode != opcode {
			t.Fatalf("frame %d = 0x%04X, want 0x%04X", index, written[index].Opcode, opcode)
		}
	}
}

/*
================
TestEmptyFrameWarningNamesProducer
================
*/
func TestEmptyFrameWarningNamesProducer(t *testing.T) {
	chain := callerChain(0)
	if !strings.Contains(chain, "TestEmptyFrameWarningNamesProducer") {
		t.Fatalf("caller chain %q does not name the producer", chain)
	}
}
