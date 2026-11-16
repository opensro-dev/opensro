/*
===========================================================================

session_write_test.go - one transport write per batch, same frames

The writer collects what it may write now into one batch and hands it to
the connection in one write (session_write.go); the tick's write window
holds a session's writer until the tick's last push. These tests pin
what must not change: the bytes a WebSocket client reads, the frames a
session delivers and their order, and what a failed batch write leaves
for the resume.

===========================================================================
*/
package transport

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

/*
================
batchConn

A fakeConn that also takes WriteFrames, recording each transport write
as the frames it carried. failNext makes the next batch write deliver
its frames and then report an error, as a WebSocket write whose
deadline fires after TCP delivered the bytes.
================
*/
type batchConn struct {
	*fakeConn
	writes   [][]Frame
	failNext bool
}

/*
================
batchConn.WriteFrame
================
*/
func (c *batchConn) WriteFrame(fr Frame) error {
	return c.WriteFrames([]Frame{fr})
}

/*
================
batchConn.WriteFrames
================
*/
func (c *batchConn) WriteFrames(frames []Frame) error {
	for _, fr := range frames {
		if err := c.fakeConn.WriteFrame(fr); err != nil {
			return err
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.writes = append(c.writes, append([]Frame(nil), frames...))
	if c.failNext {
		c.failNext = false
		return errors.New("write deadline exceeded after delivery")
	}
	return nil
}

/*
================
batchConn.writeCount
================
*/
func (c *batchConn) writeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.writes)
}

/*
================
batchConn.write

The frames of the i-th transport write.
================
*/
func (c *batchConn) write(i int) []Frame {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.writes[i]
}

/*
================
payloadBytes

The first payload byte of each frame, the tests' frame labels.
================
*/
func payloadBytes(frames []Frame) []byte {
	out := make([]byte, 0, len(frames))
	for _, f := range frames {
		out = append(out, f.Payload[0])
	}
	return out
}

/*
================
batchSession
================
*/
func batchSession(t *testing.T) (*Hub, *Session, *batchConn) {
	t.Helper()
	hub := newHub(testCfg())
	sess, err := hub.createSession()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { hub.closeSession(sess, nil) })
	conn := &batchConn{fakeConn: newFakeConn(false)}
	if err := sess.attach(conn, false, 0); err != nil {
		t.Fatal(err)
	}
	wait.Eventually(t, 3*time.Second, "WELCOME written", func() bool { return conn.writeCount() == 1 })
	return hub, sess, conn
}

/*
================
TestTickHoldWritesTheHeldFramesInOneWrite

A frame queued before the hold leaves at once; everything queued under
the hold, on both lanes, leaves in one write at release, in queue order
with the lossy lane behind the ordered one as the writer drains them.
================
*/
func TestTickHoldWritesTheHeldFramesInOneWrite(t *testing.T) {
	hub, sess, conn := batchSession(t)
	if err := sess.Send(ringTestOp, []byte{1}); err != nil {
		t.Fatal(err)
	}
	wait.Eventually(t, 3*time.Second, "the frame sent outside the hold", func() bool { return conn.writeCount() == 2 })
	if !sess.HoldTickWrites() {
		t.Fatal("HoldTickWrites did not begin a hold")
	}
	if sess.HoldTickWrites() {
		t.Fatal("a second HoldTickWrites began another hold")
	}
	for _, b := range []byte{2, 3, 4} {
		if err := sess.Send(ringTestOp, []byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	move := make([]byte, 20)
	move[0] = 5
	if err := sess.SendUnreliableKeyed(OpObjectSourceMove, 7, move); err != nil {
		t.Fatal(err)
	}
	wait.Consistently(t, 50*time.Millisecond, "no write while held", func() bool { return conn.writeCount() == 2 })
	sess.ReleaseTickWrites()
	wait.Eventually(t, 3*time.Second, "the held frames written", func() bool { return len(conn.written()) == 6 })
	if got := conn.writeCount(); got != 3 {
		t.Fatalf("transport writes = %d, want 3 (WELCOME, the early frame, the held batch)", got)
	}
	if got := payloadBytes(conn.write(2)); !bytes.Equal(got, []byte{2, 3, 4, 5}) {
		t.Fatalf("held batch carried %v, want [2 3 4 5]", got)
	}
	if got := hub.Metrics().WritesOut; got != 3 {
		t.Fatalf("writes_out = %d, want 3", got)
	}
}

/*
================
TestBatchWriteErrorResumesWithoutLossOrDuplicate

A batch write that delivered its frames and then failed detaches the
session; the client's resume position says they arrived, so the resume
sends only what was queued after them. A client that saw none of them
gets each exactly once, in order.
================
*/
func TestBatchWriteErrorResumesWithoutLossOrDuplicate(t *testing.T) {
	for _, applied := range []uint32{0, 3} {
		_, sess, conn := batchSession(t)
		conn.mu.Lock()
		conn.failNext = true
		conn.mu.Unlock()
		sess.HoldTickWrites()
		for _, b := range []byte{1, 2, 3} {
			if err := sess.Send(ringTestOp, []byte{b}); err != nil {
				t.Fatal(err)
			}
		}
		sess.ReleaseTickWrites()
		detached(t, sess)
		if err := sess.Send(ringTestOp, []byte{4}); err != nil {
			t.Fatal(err)
		}
		second := &batchConn{fakeConn: newFakeConn(false)}
		if err := sess.attach(second, true, applied); err != nil {
			t.Fatalf("resume at %d: %v", applied, err)
		}
		want := []byte{1, 2, 3, 4}[applied:]
		all := waitWritten(t, second.fakeConn, 1+len(want))
		if all[0].Opcode != OpWelcome {
			t.Fatalf("first frame after resume = 0x%04X, want WELCOME", all[0].Opcode)
		}
		if got := payloadBytes(dataFrames(all)); !bytes.Equal(got, want) {
			t.Fatalf("resumed at %d, the client received %v, want %v", applied, got, want)
		}
	}
}

/*
================
TestResumeDuringTickHoldSendsWelcomeAndRingFirst

A resume that lands while the tick holds the writer gets its WELCOME
and the ring's re-sent frames at once; the tick's frames follow at
release, after them.
================
*/
func TestResumeDuringTickHoldSendsWelcomeAndRingFirst(t *testing.T) {
	_, sess, conn := batchSession(t)
	if err := sess.Send(ringTestOp, []byte{1}); err != nil {
		t.Fatal(err)
	}
	wait.Eventually(t, 3*time.Second, "frame 1 written", func() bool { return conn.writeCount() == 2 })
	conn.Close("socket died")
	detached(t, sess)
	sess.HoldTickWrites()
	if err := sess.Send(ringTestOp, []byte{2}); err != nil {
		t.Fatal(err)
	}
	second := &batchConn{fakeConn: newFakeConn(false)}
	if err := sess.attach(second, true, 0); err != nil {
		t.Fatalf("resume: %v", err)
	}
	wait.Eventually(t, 3*time.Second, "WELCOME and the re-sent frame", func() bool { return len(second.written()) == 2 })
	wait.Consistently(t, 30*time.Millisecond, "the tick's frame held", func() bool { return len(second.written()) == 2 })
	sess.ReleaseTickWrites()
	all := waitWritten(t, second.fakeConn, 3)
	if all[0].Opcode != OpWelcome {
		t.Fatalf("first frame after resume = 0x%04X, want WELCOME", all[0].Opcode)
	}
	if got := payloadBytes(dataFrames(all)); !bytes.Equal(got, []byte{1, 2}) {
		t.Fatalf("after resume the client received %v, want [1 2]", got)
	}
}

/*
================
TestDrainReleasesTheTickHold

A session closed with a BYE drains at once, the frames the tick held
ahead of the BYE, without waiting for the tick's release.
================
*/
func TestDrainReleasesTheTickHold(t *testing.T) {
	_, sess, conn := batchSession(t)
	sess.HoldTickWrites()
	if err := sess.Send(ringTestOp, []byte{1}); err != nil {
		t.Fatal(err)
	}
	sess.CloseWhenDrained(ByeReasonShutdown)
	all := waitWritten(t, conn.fakeConn, 3)
	if all[1].Payload[0] != 1 || all[2].Opcode != OpBye {
		t.Fatalf("drain wrote %v, want the held frame then the BYE", all[1:])
	}
	select {
	case <-sess.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("drained session did not close")
	}
}

/*
================
TestWSWriteFramesIsTheSameWire

WriteFrames must put on the wire exactly the bytes one WriteFrame per
frame puts there: small frames built here, a frame past gorilla's write
buffer (fragmented by gorilla) and a deflated one in between.
================
*/
func TestWSWriteFramesIsTheSameWire(t *testing.T) {
	frames := []Frame{
		{Opcode: 0x30E3, Payload: bytes.Repeat([]byte{1}, 20)},
		{Opcode: 0x3122, Payload: bytes.Repeat([]byte{2}, 200)},
		{Opcode: 0x3013, Payload: bytes.Repeat([]byte{3}, wsWriteBufferSize-2)},
		{Opcode: 0x3013, Payload: bytes.Repeat([]byte{4}, wsWriteBufferSize-1)},
		{Opcode: 0x30D7, Payload: bytes.Repeat([]byte{5, 6}, 3*wsWriteBufferSize)},
		{Opcode: 0x3014, Payload: bytes.Repeat([]byte{7}, 70_000)},
		{Opcode: 0xB738, Payload: bytes.Repeat([]byte{8}, 28)},
	}
	wire := func(batched bool) []byte {
		conn, client, recorder := dialRecordedWS(t, true)
		done := make(chan error, 1)
		go func() {
			if batched {
				done <- conn.WriteFrames(frames)
				return
			}
			for _, f := range frames {
				if err := conn.WriteFrame(f); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}()
		if err := client.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
			t.Fatal(err)
		}
		for i, f := range frames {
			_, data, err := client.ReadMessage()
			if err != nil {
				t.Fatalf("read message %d: %v", i, err)
			}
			if !bytes.Equal(data, f.Encode()) {
				t.Fatalf("message %d differs from Frame.Encode", i)
			}
		}
		if err := <-done; err != nil {
			t.Fatalf("write: %v", err)
		}
		raw := recorder.bytes()
		return raw[bytes.Index(raw, []byte("\r\n\r\n"))+4:]
	}
	single, batched := wire(false), wire(true)
	if !bytes.Equal(single, batched) {
		at := 0
		for at < len(single) && at < len(batched) && single[at] == batched[at] {
			at++
		}
		t.Fatalf("WriteFrames wire bytes differ from one WriteFrame per frame at byte %d (lengths %d, %d)", at, len(single), len(batched))
	}
}
