/*
===========================================================================

stream_ring_entry_test.go - a resume never replays an unapplied entry answer

The browser asks for a new entry on every resumed WELCOME. Since #2338
the multi-megabyte EnterWorld answer stays in the ring until it is acked,
so a resume from before it re-sent it beside the new answer: the client
refused the second one ("Unexpected EnterWorld result") without acking
it, and every later resume replayed it again. These tests pin the rule
that ends the loop: the unapplied answer, the ring frames behind it and
the counted frames queued behind it are dropped, and what came before
it is still re-sent exactly once.

===========================================================================
*/
package transport

import (
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// entrySceneOp is a native frame of the answer's scene (0x3369 reset).
const entrySceneOp uint16 = 0x3369

/*
================
TestResumeBeforeAnUnappliedEntryAnswerDropsItAndItsScene

Frames 1 (applied) and 2 (not applied) precede the answer; the answer
and one scene frame are written; another scene frame is still queued.
A resume from position 1 re-sends frame 2 only.
================
*/
func TestResumeBeforeAnUnappliedEntryAnswerDropsItAndItsScene(t *testing.T) {
	_, sess, _ := attachedSession(t, false)
	sess.mu.Lock()
	sess.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{1}}, true)
	sess.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{2}}, true)
	sess.noteWrittenLocked(Frame{Opcode: OpEnterWorldResult, Payload: []byte{0xAA}}, true)
	sess.noteWrittenLocked(Frame{Opcode: entrySceneOp, Payload: []byte{0, 0}}, true)
	queued := Frame{Opcode: entrySceneOp, Payload: []byte{1, 1}}
	sess.queue = append(dataFrames(sess.queue), queued)
	sess.queueBytes = queued.EncodedLen()
	ok := sess.resumeFromLocked(1)
	queue := append([]Frame(nil), sess.queue...)
	queueBytes, resends, ringBytes, pos := sess.queueBytes, sess.resendFrames, sess.ringBytes, sess.streamPos
	sess.mu.Unlock()
	if !ok {
		t.Fatal("resume from 1 refused; the ring covers it")
	}
	if len(queue) != 1 || queue[0].Opcode != ringTestOp || queue[0].Payload[0] != 2 {
		t.Fatalf("re-sent %v, want frame 2 alone (no answer, no scene)", queue)
	}
	if resends != 1 || queueBytes != queue[0].EncodedLen() || ringBytes != 0 || pos != 1 {
		t.Fatalf("after the cut: resends %d queueBytes %d ringBytes %d pos %d, want 1/%d/0/1",
			resends, queueBytes, ringBytes, pos, queue[0].EncodedLen())
	}
}

/*
================
TestResumeAfterTheAppliedEntryAnswerStillResendsItsScene

The client applied the answer (position 1) but not the scene frame
behind it. That frame belongs to the world the client holds and is
re-sent, as before #2338.
================
*/
func TestResumeAfterTheAppliedEntryAnswerStillResendsItsScene(t *testing.T) {
	_, sess, _ := attachedSession(t, false)
	sess.mu.Lock()
	sess.noteWrittenLocked(Frame{Opcode: OpEnterWorldResult, Payload: []byte{1}}, true)
	sess.noteWrittenLocked(Frame{Opcode: entrySceneOp, Payload: []byte{0, 0}}, true)
	ok := sess.resumeFromLocked(1)
	queue := dataFrames(sess.queue)
	sess.mu.Unlock()
	if !ok || len(queue) != 1 || queue[0].Opcode != entrySceneOp {
		t.Fatalf("resume from past the answer: ok %v re-sent %v, want the scene frame", ok, queue)
	}
}

/*
================
TestResumedClientSeesOnlyTheNewEntryAnswer

End to end through the writer: the old answer reaches a socket that dies
before the client applies it, and the client resumes from before it and
asks again. The resumed link carries WELCOME and then exactly one
EnterWorld answer, the new one, and positions restart at the client's.
================
*/
func TestResumedClientSeesOnlyTheNewEntryAnswer(t *testing.T) {
	_, sess, first := attachedSession(t, false)
	if err := sess.Send(OpEnterWorldResult, []byte{0xAA}); err != nil {
		t.Fatal(err)
	}
	if err := sess.Send(entrySceneOp, []byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	waitWritten(t, first, 3) // WELCOME, old answer, scene
	first.Close("socket died before the answer applied")
	detached(t, sess)

	second := newFakeConn(false)
	if err := sess.attach(second, true, 0); err != nil {
		t.Fatalf("resume from before the answer refused: %v", err)
	}
	if err := sess.Send(OpEnterWorldResult, []byte{0xBB}); err != nil {
		t.Fatal(err)
	}
	// A wrongly re-sent block sits ahead of the new answer in the queue,
	// so once the new answer is written everything before it is visible.
	var all []Frame
	wait.Eventually(t, 3*time.Second, "the new answer written", func() bool {
		all = second.written()
		for _, f := range all {
			if f.Opcode == OpEnterWorldResult && f.Payload[0] == 0xBB {
				return true
			}
		}
		return false
	})
	if all[0].Opcode != OpWelcome {
		t.Fatalf("first frame after resume = 0x%04X, want WELCOME", all[0].Opcode)
	}
	got := dataFrames(all)
	if len(got) != 1 || got[0].Opcode != OpEnterWorldResult || got[0].Payload[0] != 0xBB {
		t.Fatalf("resumed link carried %v, want only the new answer", got)
	}
	sess.mu.Lock()
	pos := sess.streamPos
	sess.mu.Unlock()
	if pos != 1 {
		t.Fatalf("stream position after the new answer = %d, want 1 (counted from the client's 0)", pos)
	}
}
