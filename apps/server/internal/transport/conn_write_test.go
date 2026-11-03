/*
===========================================================================

conn_write_test.go - a slow reader finishes a record over one deadline

#2311: one 10 s write deadline across a whole record killed large
records whose peer was still reading, just slowly. The write now
re-arms the deadline on progress (writeAllWithinStall), so a record
may take arbitrarily long while bytes keep moving, and only a peer
that stops accepting bytes fails - within one stall bound.

The trickle writer runs on a manual clock: every Write call flushes
a few bytes over simulated seconds and fails a call that would end
past the armed deadline, exactly as a real stream's deadline does.
No test sleeps.

===========================================================================
*/
package transport

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

var errTrickleStall = errors.New("trickle writer: write deadline exceeded")

/*
================
stallClock

A manual clock for write-deadline tests, in simulated durations.
================
*/
type stallClock struct {
	now time.Duration
}

/*
================
trickleWriter

An io.Writer that accepts at most per bytes per call, each call
taking step of simulated time. A call that would end past the armed
deadline fails at the deadline having accepted nothing, as a real
stream write does when its write deadline expires mid-flush.
================
*/
type trickleWriter struct {
	clock    *stallClock
	deadline time.Duration
	per      int
	step     time.Duration
	sink     bytes.Buffer
}

/*
================
trickleWriter.Write
================
*/
func (w *trickleWriter) Write(p []byte) (int, error) {
	if w.clock.now+w.step > w.deadline {
		w.clock.now = w.deadline
		return 0, errTrickleStall
	}
	w.clock.now += w.step
	n := len(p)
	if n > w.per {
		n = w.per
	}
	w.sink.Write(p[:n])
	return n, nil
}

/*
================
TestWriteAllWithinStallOutlivesOneDeadline

A reader that accepts 4 KB every 3 s needs 72 s for a 96 KB record:
far past one 10 s deadline, yet every byte arrives because progress
re-arms the deadline.
================
*/
func TestWriteAllWithinStallOutlivesOneDeadline(t *testing.T) {
	clock := &stallClock{}
	w := &trickleWriter{clock: clock, per: 4096, step: 3 * time.Second}
	record := bytes.Repeat([]byte{0x5A}, 96<<10)
	arms := 0
	arm := func() {
		arms++
		w.deadline = clock.now + 10*time.Second
	}
	if err := writeAllWithinStall(w, record, arm); err != nil {
		t.Fatalf("slow reader failed: %v after %v", err, clock.now)
	}
	if clock.now <= 10*time.Second {
		t.Fatalf("the record did not outlast one deadline: %v", clock.now)
	}
	if arms < 2 {
		t.Fatal("the deadline was never re-armed on progress")
	}
	if !bytes.Equal(w.sink.Bytes(), record) {
		t.Fatal("the record bytes differ")
	}
}

/*
================
TestWriteAllWithinStallFailsWithoutProgress

A peer whose flush needs 11 s per call makes no progress inside a
10 s stall bound: the write fails and the record is not accepted.
================
*/
func TestWriteAllWithinStallFailsWithoutProgress(t *testing.T) {
	clock := &stallClock{}
	w := &trickleWriter{clock: clock, per: 4096, step: 11 * time.Second}
	record := bytes.Repeat([]byte{0x5A}, 96<<10)
	arm := func() { w.deadline = clock.now + 10*time.Second }
	if err := writeAllWithinStall(w, record, arm); err == nil {
		t.Fatal("a stalled peer wrote the whole record")
	}
	if w.sink.Len() >= len(record) {
		t.Fatal("a stalled peer accepted the whole record")
	}
}

/*
================
TestWSWriteFrameLargeMessageArrivesIntact

The WebSocket path now writes one message in pieces; the bytes a
reader receives must stay exactly Frame.Encode for a frame far
larger than one write piece.
================
*/
func TestWSWriteFrameLargeMessageArrivesIntact(t *testing.T) {
	upgrader := websocket.Upgrader{}
	serverSide := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(rw, r, nil)
		if err != nil {
			return
		}
		serverSide <- c
	}))
	defer server.Close()
	client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()
	conn := newWSConn(<-serverSide)
	defer conn.Close("test done")

	payload := bytes.Repeat([]byte{0x11, 0x22, 0x33, 0x44}, 50<<10)
	frame := Frame{Opcode: 0x3001, Payload: payload}
	writeDone := make(chan error, 1)
	go func() { writeDone <- conn.WriteFrame(frame) }()
	if err := client.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	_, data, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(data, frame.Encode()) {
		t.Fatal("the message bytes differ from one whole-message write")
	}
}
