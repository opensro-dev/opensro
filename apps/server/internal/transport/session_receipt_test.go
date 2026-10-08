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
	if err := session.attach(conn, false); err != nil {
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
	if err := session.attach(conn, false); err != nil {
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
			closed := false
			hub.OnSessionClose(func(s *Session, cause error) {
				// Reenter a queue-lock accessor from the synchronous close hook.
				if _, valid := s.SceneReceiptRevision(); valid || !errors.Is(cause, errSlowConsumer) {
					t.Errorf("overflow close valid=%t cause=%v", valid, cause)
				}
				closed = true
			})
			if err := session.SendSceneReceiptBatch(revision, []Frame{frame}); !errors.Is(err, errSlowConsumer) {
				t.Fatalf("overflow = %v", err)
			}
			if !closed {
				t.Fatal("overflow did not finish synchronous close hook")
			}
		})
	}
}
