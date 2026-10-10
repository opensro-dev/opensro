package transport

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestSendBatchCarriesAdmissionBurstPastFormerFrameCap reproduces the live S2
// failure: the writer is occupied by a multi-megabyte EnterWorld result while
// more than the former 512-frame object stream is produced synchronously.
func TestSendBatchCarriesAdmissionBurstPastFormerFrameCap(t *testing.T) {
	cfg := testCfg()
	cfg.OutboundQueue = DefaultConfig().OutboundQueue
	hub := newHub(cfg)
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	connection := newFakeConn(true)
	if err := session.attach(connection, false); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })

	const objectRows = 600
	frames := make([]Frame, 0, objectRows+1)
	frames = append(frames, Frame{
		Opcode:  OpEnterWorldResult,
		Payload: bytes.Repeat([]byte{0xA5}, 6<<20),
	})
	for index := 0; index < objectRows; index++ {
		frames = append(frames, Frame{
			Opcode:  0x3417,
			Payload: []byte{byte(index), byte(index >> 8)},
		})
	}

	if err := session.SendBatch(frames); err != nil {
		t.Fatalf("atomic admission burst: %v", err)
	}
	if err := session.Send(0x3013, []byte{0x01}); err != nil {
		t.Fatalf("post-admission send: %v", err)
	}
	if got := hub.Metrics(); got.OutboundQueueHighWater < objectRows || got.OutboundQueueByteHighWater < 6<<20 {
		t.Fatalf("queue high water = %d frames/%d bytes, want admission burst recorded", got.OutboundQueueHighWater, got.OutboundQueueByteHighWater)
	}

	connection.openGate()
	written := waitWritten(t, connection, 1+len(frames)+1)
	if written[0].Opcode != OpWelcome || written[1].Opcode != OpEnterWorldResult {
		t.Fatalf("admission prefix = 0x%04X,0x%04X, want WELCOME,ENTERWORLDRESULT", written[0].Opcode, written[1].Opcode)
	}
	for index := 0; index < objectRows; index++ {
		if written[index+2].Opcode != 0x3417 || !bytes.Equal(written[index+2].Payload, frames[index+1].Payload) {
			t.Fatalf("object row %d opcode = 0x%04X, want 0x3417", index, written[index+2].Opcode)
		}
	}
	if got := written[len(written)-1].Opcode; got != 0x3013 {
		t.Fatalf("frame after atomic batch = 0x%04X, want 0x3013", got)
	}
}

func TestSendBatchRejectsServerBurstBeyondBoundWithoutHanging(t *testing.T) {
	cfg := testCfg()
	cfg.OutboundQueue = 4
	hub := newHub(cfg)
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	connection := newFakeConn(true)
	if err := session.attach(connection, false); err != nil {
		t.Fatal(err)
	}

	frames := make([]Frame, 5)
	for index := range frames {
		frames[index] = Frame{Opcode: 0x3417, Payload: []byte{byte(index)}}
	}
	if err := session.SendBatch(frames); !errors.Is(err, errOutboundBurstTooLarge) {
		t.Fatalf("oversized batch error = %v, want %v", err, errOutboundBurstTooLarge)
	}
	select {
	case <-session.Done():
	default:
		t.Fatal("oversized server burst did not close its session")
	}
}

func TestReliableQueueByteLimitRejectsBeforePartialBatch(t *testing.T) {
	cfg := testCfg()
	cfg.OutboundQueueBytes = 12
	hub := newHub(cfg)
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	// Detached: all writes remain queued, making byte pressure deterministic.
	if err := session.Send(0x3417, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if err := session.SendBatch([]Frame{{Opcode: 0x3417, Payload: []byte{3, 4}}, {Opcode: 0x3417, Payload: []byte{5, 6}}}); err != nil {
		t.Fatalf("exact byte capacity must fit: %v", err)
	}
	if got := hub.Metrics().OutboundQueueBytes; got != 12 {
		t.Fatalf("pending bytes = %d, want 12", got)
	}
	if err := session.SendBatch([]Frame{{Opcode: 0x3417}}); !errors.Is(err, errSlowConsumer) {
		t.Fatalf("byte overflow = %v, want slow consumer", err)
	}
	if got := hub.Metrics().OutboundQueueBytes; got != 0 {
		t.Fatalf("closed queue retains %d bytes", got)
	}
}

func TestBatchCopiesPayloadAndDrainsByteAccounting(t *testing.T) {
	hub, session, connection := attachedSession(t, true)
	payload := []byte{1, 2, 3}
	if err := session.SendBatch([]Frame{{Opcode: 0x3417, Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	payload[0] = 99
	connection.openGate()
	written := waitWritten(t, connection, 2)
	if !bytes.Equal(written[1].Payload, []byte{1, 2, 3}) {
		t.Fatalf("batch aliased producer payload: %v", written[1].Payload)
	}
	if got := hub.Metrics().OutboundQueueBytes; got != 0 {
		t.Fatalf("drained queue retains %d bytes", got)
	}
}

/*
================
TestOverflowCloseHooksRunOffTheSendersStack

A sender inside a store door overflows its session; the close hooks reach
the store again. They must not run on the sender's stack while it holds the
door: here the sender holds a lock the hook needs, which deadlocked when
the hooks ran inline.
================
*/
func TestOverflowCloseHooksRunOffTheSendersStack(t *testing.T) {
	cfg := testCfg()
	cfg.OutboundQueue = 1
	hub := newHub(cfg)
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	var door sync.Mutex
	hooked := 0
	ran := make(chan struct{})
	hub.OnSessionClose(func(*Session, error) {
		door.Lock()
		hooked++
		door.Unlock()
		close(ran)
	})
	sent := make(chan error, 1)
	go func() {
		door.Lock()
		defer door.Unlock()
		sent <- session.SendBatch([]Frame{{Opcode: 0x3417}, {Opcode: 0x3417}})
	}()
	select {
	case err := <-sent:
		if !errors.Is(err, errOutboundBurstTooLarge) {
			t.Fatalf("overflow = %v, want the burst refusal", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the sender deadlocked on its own close hook")
	}
	hub.closeHooks.Wait()
	select {
	case <-ran:
	default:
		t.Fatal("the close hook never ran")
	}
	door.Lock()
	defer door.Unlock()
	if hooked != 1 {
		t.Fatalf("close hooks ran %d times, want once", hooked)
	}
}
