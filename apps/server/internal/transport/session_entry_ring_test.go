/*
===========================================================================

session_entry_ring_test.go - a slow client downloading its EnterWorld answer

The EnterWorld answer of a busy division is bigger than the whole resume
ring (4,925,575 bytes live, against streamRingMaxBytes). Held in the ring
with its bytes (#2338), it parked the writer behind it until the client
acked it, and a client still downloading it past RingParkBound was
detached as a wedged applier. The resume then dropped the unapplied answer
(#2363), the browser asked for a new entry, and the new answer parked the
writer again: a client slower than the bound could never enter, and one
that did had lost the frames behind the answer (the missing skill-bar,
buff and premium icons on live). These tests hold the entry the way it
worked before #2338 - the frames behind the answer flow and nothing
detaches - while a resume from before the answer is still served.

===========================================================================
*/
package transport

import (
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// entryAnswerBytes is a live-sized EnterWorld answer: past the ring's
// whole byte bound, inside MaxFrameBytes.
const entryAnswerBytes = streamRingMaxBytes + 731271

// slowEntryParkBound stands in for RingParkBound. A loaded machine only
// delays the watchdog past it, never fires it early, so three bounds of
// quiet are a safe "still attached" window in the passing direction.
const slowEntryParkBound = 200 * time.Millisecond

/*
================
enterSlowly

An attached session whose client applied and acked its first frame, then
was sent a live-sized EnterWorld answer and the three scene frames behind
it, and acks nothing more: it is still downloading the answer.
================
*/
func enterSlowly(t *testing.T, bound time.Duration) (*Hub, *Session, *fakeConn) {
	t.Helper()
	hub, sess, fc := parkBoundSession(t, bound)
	if err := sess.Send(ringTestOp, []byte{1}); err != nil {
		t.Fatal(err)
	}
	writtenCount(t, fc, 1)
	sendStreamAck(fc, 1)
	wait.Eventually(t, 5*time.Second, "the first frame is acked", func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return len(sess.ring) == 0
	})
	if err := sess.Send(OpEnterWorldResult, make([]byte, entryAnswerBytes)); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := sess.Send(ringTestOp, []byte{2}); err != nil {
			t.Fatal(err)
		}
	}
	return hub, sess, fc
}

/*
================
TestSlowEntryDownloadIsNotParkDetached

Past three park bounds without an ack, the client is downloading, not
wedged: it stays attached, and the scene behind the answer was written
behind it as it was before #2338.
================
*/
func TestSlowEntryDownloadIsNotParkDetached(t *testing.T) {
	t.Parallel()
	hub, sess, fc := enterSlowly(t, slowEntryParkBound)
	wait.Eventually(t, 5*time.Second, "the scene behind the answer is written", func() bool {
		return len(dataFrames(fc.written())) == 1+1+3
	})
	wait.Consistently(t, 3*slowEntryParkBound, "a downloading client stays attached", func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return sess.attached
	})
	if got := hub.metrics.detachesRingPark.Load(); got != 0 {
		t.Fatalf("detaches_ring_park = %d, want 0 for a client still downloading its entry", got)
	}
}

/*
================
TestResumeBeforeAnUnappliedEntryIsServed

A link that drops while the answer downloads resumes from the position
before it: the resume is served, not refused behind the floor, and it
re-sends neither the answer nor the scene behind it (the browser asks
for a new entry on every resume, supersededEntryLocked).
================
*/
func TestResumeBeforeAnUnappliedEntryIsServed(t *testing.T) {
	t.Parallel()
	_, sess, fc := enterSlowly(t, time.Hour)
	wait.Eventually(t, 5*time.Second, "the scene behind the answer is written", func() bool {
		return len(dataFrames(fc.written())) == 1+1+3
	})
	sess.detachGen(sess.gen, errRingParkBound)
	detached(t, sess)
	second := newFakeConn(false)
	if err := sess.attach(second, true, 1); err != nil {
		t.Fatalf("resume from before the unapplied answer: %v", err)
	}
	wait.Consistently(t, 300*time.Millisecond, "nothing of the superseded entry is re-sent", func() bool {
		return len(dataFrames(second.written())) == 0
	})
}
