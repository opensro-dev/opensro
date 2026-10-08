/*
===========================================================================

session_dispatch_test.go - application dispatch across repeated attachments

A reader already admitted by its attachment must recheck its generation
after waiting behind an older handler's commit and publication.

===========================================================================
*/
package transport

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/testsupport/wait"
)

/*
================
dispatchReadConn

The second read proves the first frame finished dispatch, even when this
attachment was replaced while that dispatch waited for the session lock.
================
*/
type dispatchReadConn struct {
	*fakeConn
	reads      int
	dispatched chan struct{}
}

/*
================
ReadFrame
================
*/
func (c *dispatchReadConn) ReadFrame(ctx context.Context) (Frame, error) {
	c.reads++
	if c.reads == 2 {
		close(c.dispatched)
	}
	return c.fakeConn.ReadFrame(ctx)
}

/*
================
TestDispatchWaitingReaderSupersededAgain
================
*/
func TestDispatchWaitingReaderSupersededAgain(t *testing.T) {
	const (
		opcode  = 0x727a
		timeout = 3 * time.Second
	)
	hub, session, first := attachedSession(t, false)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var stale, newest atomic.Int32
	hub.Handle(opcode, func(_ *Session, _ uint16, payload []byte) {
		switch payload[0] {
		case 1:
			close(entered)
			<-release
		case 2:
			stale.Add(1)
		case 3:
			newest.Add(1)
		}
	})
	first.inbound <- Frame{Opcode: opcode, Payload: []byte{1}}
	select {
	case <-entered:
	case <-time.After(timeout):
		t.Fatal("first handler did not enter")
	}

	second := &dispatchReadConn{fakeConn: newFakeConn(false), dispatched: make(chan struct{})}
	if err := session.attach(second, true); err != nil {
		t.Fatal(err)
	}
	session.mu.Lock()
	session.lastRecv = time.Time{}
	session.mu.Unlock()
	second.inbound <- Frame{Opcode: opcode, Payload: []byte{2}}
	wait.Eventually(t, timeout, "second reader passes initial generation check", func() bool {
		session.mu.Lock()
		defer session.mu.Unlock()
		return !session.lastRecv.IsZero()
	})

	third := newFakeConn(false)
	if err := session.attach(third, true); err != nil {
		t.Fatal(err)
	}
	third.inbound <- Frame{Opcode: opcode, Payload: []byte{3}}
	unblock()
	select {
	case <-second.dispatched:
	case <-time.After(timeout):
		t.Fatal("superseded reader did not finish its queued dispatch")
	}
	wait.Eventually(t, timeout, "newest attachment dispatches", func() bool {
		return newest.Load() == 1
	})
	if got := stale.Load(); got != 0 {
		t.Fatalf("superseded generation handler ran %d times", got)
	}
}
