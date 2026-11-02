/*
===========================================================================

session_receipt_test.go - private receipt delivery across scene loading

Private updates follow their bootstrap before game-ready, but cannot leak
into a replacement scene or bypass the reliable queue's failure policy.

===========================================================================
*/
package transport

import (
	"bytes"
	"errors"
	"testing"
)

/*
================
TestSceneReceiptLoadingKeepsVisibilityFence
================
*/
func TestSceneReceiptLoadingKeepsVisibilityFence(t *testing.T) {
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	const reset, receipt, visibility = 0x366a, 0xb06d, 0x3015
	if err := session.SendSceneReset([]Frame{{Opcode: reset}}); err != nil {
		t.Fatal(err)
	}
	revision, valid := session.SceneReceiptRevision()
	if _, active := session.SceneRevision(); active || !valid {
		t.Fatal("loading must admit receipts but not visibility")
	}
	if err := session.SendSceneBatch(revision, []Frame{{Opcode: visibility}}); err != nil {
		t.Fatal(err)
	}
	payload := []byte{1, 21, 3}
	if err := session.SendSceneReceiptBatch(revision, []Frame{{Opcode: receipt, Payload: payload}}); err != nil {
		t.Fatal(err)
	}
	payload[2] = 99
	if _, active := session.SceneRevision(); active {
		t.Fatal("receipt changed scene loading state")
	}
	if !session.FinishSceneReentry() {
		t.Fatal("scene was not loading")
	}
	if err := session.SendSceneBatch(revision, []Frame{{Opcode: visibility}}); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(false)
	if err := session.attach(conn, false, 0); err != nil {
		t.Fatal(err)
	}
	frames := waitWritten(t, conn, 4)
	if len(frames) != 4 || frames[0].Opcode != OpWelcome || frames[1].Opcode != reset ||
		frames[2].Opcode != receipt || !bytes.Equal(frames[2].Payload, []byte{1, 21, 3}) || frames[3].Opcode != visibility {
		t.Fatalf("unexpected bootstrap/receipt/ready visibility order: %+v", frames)
	}
}

/*
================
TestSceneReceiptRejectsReplacementAndClosure
================
*/
func TestSceneReceiptRejectsReplacementAndClosure(t *testing.T) {
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	old, _ := session.SceneReceiptRevision()
	const reset, stale, current = 0x366a, 0xb06d, 0xb5bd
	if err := session.SendSceneReset([]Frame{{Opcode: reset}}); err != nil {
		t.Fatal(err)
	}
	for _, ready := range []bool{false, true} {
		if ready {
			session.FinishSceneReentry()
		}
		if err := session.SendSceneReceiptBatch(old, []Frame{{Opcode: stale}}); err != nil {
			t.Fatal(err)
		}
	}
	revision, valid := session.SceneReceiptRevision()
	if !valid || revision == old {
		t.Fatal("reset did not replace capture revision")
	}
	if err := session.SendSceneReceiptBatch(revision, []Frame{{Opcode: current}}); err != nil {
		t.Fatal(err)
	}
	conn := newFakeConn(false)
	if err := session.attach(conn, false, 0); err != nil {
		t.Fatal(err)
	}
	frames := waitWritten(t, conn, 3)
	if len(frames) != 3 || frames[1].Opcode != reset || frames[2].Opcode != current {
		t.Fatalf("stale receipt escaped: %+v", frames)
	}
	hub.closeSession(session, nil)
	if _, valid := session.SceneReceiptRevision(); valid {
		t.Fatal("closed session remained eligible for receipt capture")
	}
	if err := session.SendSceneReceiptBatch(revision, []Frame{{Opcode: current}}); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("send after capture then closure = %v", err)
	}
}

/*
================
TestSceneReceiptOverflowClosesOutsideQueueLock
================
*/
func TestSceneReceiptOverflowClosesOutsideQueueLock(t *testing.T) {
	for _, limit := range []string{"frames", "bytes"} {
		t.Run(limit, func(t *testing.T) {
			cfg := testCfg()
			frame := Frame{Opcode: 0xb06d, Payload: []byte{1}}
			if limit == "frames" {
				cfg.OutboundQueue = 2
			} else {
				cfg.OutboundQueueBytes = 2 * frame.EncodedLen()
			}
			hub := newHub(cfg)
			session, err := hub.createSession()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { hub.closeSession(session, nil) })
			session.BeginSceneAdmission()
			revision, _ := session.SceneReceiptRevision()
			if err := session.SendSceneReceiptBatch(revision, []Frame{frame, frame}); err != nil {
				t.Fatal(err)
			}
			closed := make(chan struct{})
			hub.OnSessionClose(func(s *Session, cause error) {
				// Reenter a queue-lock accessor from the close hook.
				if _, valid := s.SceneReceiptRevision(); valid || !errors.Is(cause, errSlowConsumer) {
					t.Errorf("overflow close valid=%t cause=%v", valid, cause)
				}
				close(closed)
			})
			if err := session.SendSceneReceiptBatch(revision, []Frame{frame}); !errors.Is(err, errSlowConsumer) {
				t.Fatalf("overflow = %v", err)
			}
			// The overflow's hooks run off the sender's stack
			// (closeSessionFromSend); shutdown waits for them.
			hub.closeHooks.Wait()
			select {
			case <-closed:
			default:
				t.Fatal("overflow did not run its close hook")
			}
		})
	}
}

/*
================
TestSceneReceiptParkedBehindItsBootstrap

A receipt accepted while its scene's bootstrap is not queued yet joins
the queue behind the bootstrap, never ahead of it (#2294): the entry
handler releases the publication gate before encoding now, so a tick
committed after the snapshot build can answer before the bootstrap
queues. The parked receipt still counts against the queue budget.
================
*/
func TestSceneReceiptParkedBehindItsBootstrap(t *testing.T) {
	hub := newHub(testCfg())
	session, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(session, nil) })
	const reset, early, late = 0x366a, 0xb06d, 0xb070
	queueOpcodes := func() []uint16 {
		session.mu.Lock()
		defer session.mu.Unlock()
		out := make([]uint16, len(session.queue))
		for i, frame := range session.queue {
			out[i] = frame.Opcode
		}
		return out
	}

	session.BeginSceneAdmission()
	session.MarkSceneSnapshotBuilt()
	revision, valid := session.SceneReceiptRevision()
	if !valid {
		t.Fatal("receipt revision must be valid while loading")
	}
	if err := session.SendSceneReceiptBatch(revision, []Frame{{Opcode: early, Payload: []byte{1}}}); err != nil {
		t.Fatal(err)
	}
	if got := queueOpcodes(); len(got) != 0 {
		t.Fatalf("parked receipt reached the queue: %v", got)
	}

	// A fresh admission drops whatever the previous scene parked.
	session.BeginSceneAdmission()
	session.MarkSceneSnapshotBuilt()
	revision, valid = session.SceneReceiptRevision()
	if !valid {
		t.Fatal("receipt revision must be valid while loading")
	}
	if err := session.SendSceneReceiptBatch(revision, []Frame{{Opcode: early, Payload: []byte{7}}}); err != nil {
		t.Fatal(err)
	}
	if err := session.SendSceneReset([]Frame{{Opcode: reset, Payload: []byte{9}}}); err != nil {
		t.Fatal(err)
	}
	// The reset batch advanced the revision; receipts capture it after.
	revision, _ = session.SceneReceiptRevision()
	if err := session.SendSceneReceiptBatch(revision, []Frame{{Opcode: late, Payload: []byte{8}}}); err != nil {
		t.Fatal(err)
	}
	want := []uint16{reset, early, late}
	got := queueOpcodes()
	if len(got) != len(want) {
		t.Fatalf("queue = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("queue = %v, want %v", got, want)
		}
	}
	// The parked receipt kept the payload captured at park time.
	session.mu.Lock()
	payload, parkedFrames, parkedBytes := session.queue[1].Payload, session.pendingFrames, session.pendingBytes
	session.mu.Unlock()
	if len(payload) != 1 || payload[0] != 7 || parkedFrames != 0 || parkedBytes != 0 {
		t.Fatalf("park not flushed cleanly: payload %v pending %d/%d", payload, parkedFrames, parkedBytes)
	}
}
