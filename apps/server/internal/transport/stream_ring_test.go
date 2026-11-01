/*
===========================================================================

stream_ring_test.go - exactly-once ordered delivery across a resume (#1466)

A frame whose write failed after it reached the client is not sent again;
a frame written into a socket that died before delivery is sent again,
exactly once, ahead of the frames queued while detached; an ACK trims the
ring; a resume the ring cannot cover, or one with no position, starts a
fresh session instead of passing a gap silently.

===========================================================================
*/
package transport

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

// ringTestOp is an ordinary game opcode above the transport range.
const ringTestOp uint16 = 0x3126

/*
================
deliverThenFailConn

A connection whose writes reach the peer but report an error, as a
WebSocket write whose deadline fires after TCP delivered the bytes.
================
*/
type deliverThenFailConn struct {
	*fakeConn
	fail bool
}

func (c *deliverThenFailConn) WriteFrame(fr Frame) error {
	if err := c.fakeConn.WriteFrame(fr); err != nil {
		return err
	}
	if c.fail && fr.Opcode == ringTestOp {
		return errors.New("write deadline exceeded after delivery")
	}
	return nil
}

/*
================
dataFrames

The written frames above the transport range, in order.
================
*/
func dataFrames(frames []Frame) []Frame {
	var out []Frame
	for _, f := range frames {
		if CountsOnStream(f.Opcode) {
			out = append(out, f)
		}
	}
	return out
}

/*
================
detached

Waits until the session has detached.
================
*/
func detached(t *testing.T, sess *Session) {
	t.Helper()
	wait.Eventually(t, 3*time.Second, "session detach", func() bool { return sess.Kind() == "detached" })
}

func TestRingFrameDeliveredDespiteWriteErrorIsNotSentTwice(t *testing.T) {
	hub := newHub(testCfg())
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	first := &deliverThenFailConn{fakeConn: newFakeConn(false), fail: true}
	if err := sess.attach(first, false, 0); err != nil {
		t.Fatal(err)
	}
	if err := sess.Send(ringTestOp, []byte{1}); err != nil {
		t.Fatal(err)
	}
	detached(t, sess)
	if got := dataFrames(first.written()); len(got) != 1 {
		t.Fatalf("first connection received %d data frames, want 1", len(got))
	}
	// The client applied that frame: it resumes at position 1.
	second := newFakeConn(false)
	if err := sess.attach(second, true, 1); err != nil {
		t.Fatalf("resume at the delivered position: %v", err)
	}
	if err := sess.Send(ringTestOp, []byte{2}); err != nil {
		t.Fatal(err)
	}
	got := dataFrames(waitWritten(t, second, 2))
	if len(got) != 1 || got[0].Payload[0] != 2 {
		t.Fatalf("after resume the client received %v, want only the new frame", got)
	}
}

func TestRingFrameLostInADeadSocketIsResentOnceBeforeTheDetachedQueue(t *testing.T) {
	hub, sess, first := attachedSession(t, false)
	_ = hub
	for _, b := range []byte{1, 2} {
		if err := sess.Send(ringTestOp, []byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	waitWritten(t, first, 3) // WELCOME + 2
	first.Close("socket died")
	detached(t, sess)
	if err := sess.Send(ringTestOp, []byte{3}); err != nil {
		t.Fatal(err)
	}
	// Only the first frame reached the client before the socket died.
	second := newFakeConn(false)
	if err := sess.attach(second, true, 1); err != nil {
		t.Fatalf("resume: %v", err)
	}
	all := waitWritten(t, second, 3)
	if all[0].Opcode != OpWelcome {
		t.Fatalf("first frame after resume = 0x%04X, want WELCOME", all[0].Opcode)
	}
	got := dataFrames(all)
	if len(got) != 2 || got[0].Payload[0] != 2 || got[1].Payload[0] != 3 {
		t.Fatalf("after resume the client received %v, want frame 2 (re-sent) then 3 (queued)", got)
	}
}

func TestRingAckDropsAppliedFrames(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	for _, b := range []byte{1, 2, 3} {
		if err := sess.Send(ringTestOp, []byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	waitWritten(t, fc, 4)
	ack := make([]byte, 4)
	binary.LittleEndian.PutUint32(ack, 2)
	fc.inbound <- Frame{Opcode: OpStreamAck, Payload: ack}
	wait.Eventually(t, 3*time.Second, "ring trimmed to one frame", func() bool {
		sess.mu.Lock()
		defer sess.mu.Unlock()
		return len(sess.ring) == 1 && sess.ring[0].pos == 3
	})
}

func TestRingOverflowRefusesTheResume(t *testing.T) {
	_, sess, _ := attachedSession(t, false)
	sess.mu.Lock()
	for i := 0; i <= streamRingMaxFrames; i++ {
		sess.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{1}}, true)
	}
	ok := sess.resumeFromLocked(0)
	floor := sess.ringFloor
	sess.mu.Unlock()
	if ok || floor == 0 {
		t.Fatalf("resume from 0 after overflow (floor %d) = %v, want refused", floor, ok)
	}
}

func TestRingLossyFramesAdvanceThePositionButAreNeverResent(t *testing.T) {
	_, sess, _ := attachedSession(t, false)
	sess.mu.Lock()
	sess.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{1}}, false)
	sess.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{2}}, true)
	pos, kept := sess.streamPos, len(sess.ring)
	sess.mu.Unlock()
	if pos != 2 || kept != 1 {
		t.Fatalf("position %d ring %d, want 2 and only the ordered frame kept", pos, kept)
	}
}

func TestRingResumeWithoutAPositionStartsAFreshSession(t *testing.T) {
	hub := newHub(testCfg())
	allowTestHelloAdmission(hub)
	first := newFakeConn(false)
	first.inbound <- Frame{Opcode: OpHello, Payload: EncodeHello(Hello{AdmissionToken: testHelloAdmissionToken})}
	hub.AcceptConn(first)
	welcome := waitWritten(t, first, 1)[0]
	w, err := DecodeWelcome(welcome.Payload)
	if err != nil {
		t.Fatal(err)
	}
	first.Close("socket died")
	second := newFakeConn(false)
	second.inbound <- Frame{Opcode: OpHello, Payload: EncodeHello(Hello{
		ResumeToken:    w.ResumeToken,
		AdmissionToken: testHelloAdmissionToken,
	})}
	hub.AcceptConn(second)
	again, err := DecodeWelcome(waitWritten(t, second, 1)[0].Payload)
	if err != nil {
		t.Fatal(err)
	}
	if again.Resumed || again.SessionID == w.SessionID {
		t.Fatalf("a resume with no position was accepted (session %d resumed %v)", again.SessionID, again.Resumed)
	}
}

func TestRingCountedOpcodesMatchTheClient(t *testing.T) {
	raw, err := os.ReadFile("testdata/stream_position_opcodes.json")
	if err != nil {
		t.Fatal(err)
	}
	var set struct {
		Uncounted []uint16 `json:"uncounted"`
		Counted   []uint16 `json:"counted"`
	}
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	for _, op := range set.Uncounted {
		if CountsOnStream(op) {
			t.Errorf("opcode 0x%04X counts on the stream; the client does not count it", op)
		}
	}
	for _, op := range set.Counted {
		if !CountsOnStream(op) {
			t.Errorf("opcode 0x%04X does not count on the stream; the client counts it", op)
		}
	}
}

func TestRingResumeDuringAnInFlightWriteKeepsTheFrame(t *testing.T) {
	_, sess, first := attachedSession(t, true)
	first.openGate()
	waitWritten(t, first, 1) // WELCOME
	first.gate = make(chan struct{})
	if err := sess.Send(ringTestOp, []byte{1}); err != nil {
		t.Fatal(err)
	}
	// The writer has dequeued frame 1 and is inside the old socket's
	// write when the client resumes with nothing applied.
	wait.Eventually(t, 3*time.Second, "write in flight", func() bool { return first.inFlight.Load() == 1 })
	second := newFakeConn(false)
	if err := sess.attach(second, true, 0); err != nil {
		t.Fatalf("resume: %v", err)
	}
	if err := sess.Send(ringTestOp, []byte{2}); err != nil {
		t.Fatal(err)
	}
	got := dataFrames(waitWritten(t, second, 3))
	if len(got) != 2 || got[0].Payload[0] != 1 || got[1].Payload[0] != 2 {
		t.Fatalf("after the resume the client received %v, want frames 1 then 2", got)
	}
	sess.mu.Lock()
	pos := sess.streamPos
	sess.mu.Unlock()
	if pos != 2 {
		t.Fatalf("stream position %d after two frames, want 2", pos)
	}
}

func TestRingStaleFrameTakesBackItsPosition(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	stale := Frame{Opcode: ringTestOp, Payload: []byte{1}, Current: func() bool { return false }}
	sess.mu.Lock()
	sess.queue = append(sess.queue, stale)
	sess.queueBytes += stale.EncodedLen()
	sess.cond.Broadcast()
	sess.mu.Unlock()
	if err := sess.Send(ringTestOp, []byte{2}); err != nil {
		t.Fatal(err)
	}
	got := dataFrames(waitWritten(t, fc, 2))
	sess.mu.Lock()
	pos, ring := sess.streamPos, len(sess.ring)
	sess.mu.Unlock()
	if len(got) != 1 || pos != 1 || ring != 1 {
		t.Fatalf("written %v, position %d, ring %d; want only frame 2 at position 1", got, pos, ring)
	}
}

func TestRingResumeThatWouldOverflowTheQueueIsRefused(t *testing.T) {
	_, sess, _ := attachedSession(t, false)
	sess.mu.Lock()
	defer sess.mu.Unlock()
	for i := 0; i < sess.hub.cfg.OutboundQueue/2+1; i++ {
		sess.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{1}}, true)
	}
	for i := 0; i < sess.hub.cfg.OutboundQueue/2; i++ {
		sess.queue = append(sess.queue, Frame{Opcode: ringTestOp, Payload: []byte{2}})
	}
	if sess.resumeFromLocked(0) {
		t.Fatal("a re-sent block that overflows the outbound queue was accepted")
	}
}

/*
================
TestRingResumeRefusalNamesTheGap

Every refusal the ring can produce names itself against the ring's own
numbers (#2146): behind a floor the ring already dropped past, beyond
everything written, or covered but too big to re-send. The live refusal
log prints exactly this snapshot; a wrong classification here sends the
whole diagnosis after the wrong side of the contract.
================
*/
func TestRingResumeRefusalNamesTheGap(t *testing.T) {
	_, sess, _ := attachedSession(t, false)

	sess.mu.Lock()
	for i := 0; i <= streamRingMaxFrames; i++ {
		sess.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{1}}, true)
	}
	if sess.resumeFromLocked(0) {
		sess.mu.Unlock()
		t.Fatal("resume from 0 after overflow was accepted")
	}
	head := sess.streamPos
	sess.mu.Unlock()
	d := sess.ResumeRefusal(0)
	if d.Reason != "behind-floor" || d.Reported != 0 || d.Floor == 0 || d.Head != head || d.Oldest <= d.Floor {
		t.Fatalf("overflow refusal = %+v, want behind-floor at 0 with floor < oldest <= head %d", d, head)
	}

	d = sess.ResumeRefusal(head + 1)
	if d.Reason != "beyond-head" || d.Reported != head+1 {
		t.Fatalf("unwritten position refusal = %+v, want beyond-head at %d", d, head+1)
	}

	_, full, _ := attachedSession(t, false)
	full.mu.Lock()
	for i := 0; i < full.hub.cfg.OutboundQueue/2+1; i++ {
		full.noteWrittenLocked(Frame{Opcode: ringTestOp, Payload: []byte{1}}, true)
	}
	for i := 0; i < full.hub.cfg.OutboundQueue/2; i++ {
		full.queue = append(full.queue, Frame{Opcode: ringTestOp, Payload: []byte{2}})
	}
	if full.resumeFromLocked(0) {
		full.mu.Unlock()
		t.Fatal("a re-sent block that overflows the outbound queue was accepted")
	}
	full.mu.Unlock()
	d = full.ResumeRefusal(0)
	if d.Reason != "resend-overflow" || d.QueuedFrames == 0 || d.RingFrames == 0 {
		t.Fatalf("queue refusal = %+v, want resend-overflow with ring and queue populated", d)
	}
}

/*
================
writtenCount

Counts one burst's written data frames until the count stands still
at want: the writer reached the point the test is about.
================
*/
func writtenCount(t *testing.T, fc *fakeConn, want int) {
	t.Helper()
	wait.Eventually(t, 5*time.Second, "written data frames at the bound", func() bool {
		return len(dataFrames(fc.written())) == want
	})
	// A writer that is merely slow, not gated, would pass the bound in
	// this window; a gated one is still standing at it.
	wait.Consistently(t, 150*time.Millisecond, "written data frames hold at the bound", func() bool {
		return len(dataFrames(fc.written())) == want
	})
}

/*
================
sendBurst

Sends the payloads in chunks, waiting after each one until the
writer has dequeued all but the tail of what was sent: the test's
own flood must never reach the queue bound on the way to the ring
bound (the state under test, where the queue legitimately holds
everything past it).
================
*/
func sendBurst(t *testing.T, sess *Session, fc *fakeConn, payloads [][]byte) {
	t.Helper()
	const chunk = 256
	for sent := 0; sent < len(payloads); {
		n := chunk
		if sent+n > len(payloads) {
			n = len(payloads) - sent
		}
		for i := 0; i < n; i++ {
			if err := sess.Send(ringTestOp, payloads[sent+i]); err != nil {
				t.Fatal(err)
			}
		}
		sent += n
		wait.Eventually(t, 5*time.Second, "queue drains between burst chunks", func() bool {
			sess.mu.Lock()
			defer sess.mu.Unlock()
			return len(sess.queue) <= 2048
		})
	}
}

/*
================
TestRingWriterWaitsAtTheFrameBoundInsteadOfDropping

The #2146 contract: a burst past the ring's frame bound paces the
writer instead of dropping the oldest unacknowledged frames. Nothing
is dropped (floor never moves, the rest stay queued), and the burst
completes once the client's ack frees the ring.
================
*/
func TestRingWriterWaitsAtTheFrameBoundInsteadOfDropping(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	const extra = 64
	payloads := make([][]byte, streamRingMaxFrames+extra)
	for i := range payloads {
		payloads[i] = []byte{7}
	}
	sendBurst(t, sess, fc, payloads)
	writtenCount(t, fc, streamRingMaxFrames)
	sess.mu.Lock()
	floor, ring, queued := sess.ringFloor, len(sess.ring), len(sess.queue)
	sess.mu.Unlock()
	if floor != 0 || ring != streamRingMaxFrames || queued != extra {
		t.Fatalf("at the frame bound: floor %d ring %d queued %d, want 0/%d/%d",
			floor, ring, queued, streamRingMaxFrames, extra)
	}

	ack := make([]byte, 4)
	binary.LittleEndian.PutUint32(ack, streamRingMaxFrames)
	fc.inbound <- Frame{Opcode: OpStreamAck, Payload: ack}
	if got := len(dataFrames(waitWritten(t, fc, streamRingMaxFrames+extra+1))); got != streamRingMaxFrames+extra {
		t.Fatalf("after the ack the burst delivered %d data frames, want %d", got, streamRingMaxFrames+extra)
	}
	sess.mu.Lock()
	floor = sess.ringFloor
	sess.mu.Unlock()
	if floor != 0 {
		t.Fatalf("floor moved to %d across a paced burst, want 0", floor)
	}
}

/*
================
TestRingWriterWaitsAtTheByteBoundInsteadOfDropping

The same pacing at the byte bound: kilobyte frames fill 4 MiB well
before the frame count matters, and the writer must hold there too.
================
*/
func TestRingWriterWaitsAtTheByteBoundInsteadOfDropping(t *testing.T) {
	_, sess, fc := attachedSession(t, false)
	const total = 6000
	bound := streamRingMaxBytes / (1024 + 2)
	payloads := make([][]byte, total)
	for i := range payloads {
		payloads[i] = make([]byte, 1024)
	}
	sendBurst(t, sess, fc, payloads)
	writtenCount(t, fc, bound)
	sess.mu.Lock()
	floor, ring, ringBytes := sess.ringFloor, len(sess.ring), sess.ringBytes
	sess.mu.Unlock()
	if floor != 0 || ring != bound || ringBytes > streamRingMaxBytes {
		t.Fatalf("at the byte bound: floor %d ring %d ringBytes %d, want 0/%d/<= %d",
			floor, ring, ringBytes, bound, streamRingMaxBytes)
	}

	ack := make([]byte, 4)
	binary.LittleEndian.PutUint32(ack, uint32(bound))
	fc.inbound <- Frame{Opcode: OpStreamAck, Payload: ack}
	wait.Eventually(t, 5*time.Second, "burst completes after the ack", func() bool {
		return len(dataFrames(fc.written())) == total
	})
}

/*
================
TestRingResumeServedAfterAnEntryBurstOutranTheClient

The live #2146 shape end to end: a multi-megabyte entry burst, a
client that has applied only the first hundred frames when the link
dies, and a resume seconds later. The ring must still cover the
position, and the re-send must be exactly the unapplied tail.
================
*/
func TestRingResumeServedAfterAnEntryBurstOutranTheClient(t *testing.T) {
	// Production queue sizing: the re-send of a full ring against the
	// frames still queued must fit the same bound resumeFromLocked
	// checks, or the refusal this test hunts just changes its name.
	cfg := testCfg()
	cfg.OutboundQueue = DefaultConfig().OutboundQueue
	hub := newHub(cfg)
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	fc := newFakeConn(false)
	if err := sess.attach(fc, false, 0); err != nil {
		t.Fatal(err)
	}
	const total = 6000
	payloads := make([][]byte, total)
	for i := range payloads {
		payloads[i] = make([]byte, 1024)
		binary.LittleEndian.PutUint32(payloads[i], uint32(i))
	}
	sendBurst(t, sess, fc, payloads)
	// The burst outran the client: it applied the first hundred frames,
	// the writer paced at the byte bound, the rest are still queued.
	fc.Close("link died")
	detached(t, sess)

	second := newFakeConn(false)
	if err := sess.attach(second, true, 100); err != nil {
		t.Fatalf("resume after the burst: %v", err)
	}
	// The re-send is paced by the same ring: the client acknowledges
	// what it has applied until the whole tail has arrived.
	for delivered := 0; delivered < total-100; {
		wait.Eventually(t, 5*time.Second, "re-send advances", func() bool {
			return len(dataFrames(second.written())) > delivered
		})
		delivered = len(dataFrames(second.written()))
		ack := make([]byte, 4)
		binary.LittleEndian.PutUint32(ack, uint32(100+delivered))
		second.inbound <- Frame{Opcode: OpStreamAck, Payload: ack}
	}
	got := dataFrames(second.written())
	if len(got) != total-100 {
		t.Fatalf("re-sent %d frames, want %d", len(got), total-100)
	}
	for i, f := range got {
		if idx := binary.LittleEndian.Uint32(f.Payload); idx != uint32(100+i) {
			t.Fatalf("re-sent frame %d carries burst index %d, want %d", i, idx, 100+i)
		}
	}
}
