/*
===========================================================================

loading_scene_test.go - tests for hubHandler's loading-scene discard

===========================================================================
*/

package action

import (
	"sync/atomic"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/testsupport/wait"
	"opensro.online/server/internal/transport"
)

/*
================
TestRequestWhileTheSceneLoadsIsNotAnswered

A request read after the bootstrap but before the client's game-ready was
sent before the client saw the reset: it reaches no operation and gets no
answer. The same request after game-ready is answered.
================
*/
func TestRequestWhileTheSceneLoadsIsNotAnswered(t *testing.T) {
	h := newPublicationHarness(t, false, 50)
	var calls atomic.Int32
	h.server.Hub.Handle(wire.OpItemUseRequest, h.rt.hubHandler(h.server.Hub, func(string, *enterworld.Character, []byte) OpResult {
		calls.Add(1)
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: []byte{2, 0}}}}
	}))
	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	answered := func() bool {
		conn.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
		conn.in <- transport.Frame{Opcode: publicationFence}
		for _, f := range publicationThrough(t, conn, publicationFence) {
			if f.Opcode == wire.OpItemMoveResponse {
				return true
			}
		}
		return false
	}
	if answered() || calls.Load() != 0 {
		t.Fatal("a request sent before game-ready was answered into the loading scene")
	}
	h.ready(t, conn)
	if !answered() || calls.Load() != 1 {
		t.Fatal("a request after game-ready went unanswered")
	}
}

/*
================
TestRequestWaitingOnAResetIsNotAnswered

A reset another handler pushes (a recall, a relocation) holds the division
publication gate. A request that reached the gate before that reset began
must see it once the gate opens: it is discarded, not answered into the
new scene.
================
*/
func TestRequestWaitingOnAResetIsNotAnswered(t *testing.T) {
	h := newPublicationHarness(t, false, 50)
	var calls atomic.Int32
	h.server.Hub.Handle(wire.OpItemUseRequest, h.rt.hubHandler(h.server.Hub, func(string, *enterworld.Character, []byte) OpResult {
		calls.Add(1)
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: []byte{2, 0}}}}
	}))
	conn, welcome := h.connect(t, nil)
	h.enter(t, conn)
	h.ready(t, conn)
	session, _ := h.server.Hub.Session(welcome.SessionID)
	release := h.rt.LockPublication(testDivision, "test")
	conn.in <- transport.Frame{Opcode: wire.OpItemUseRequest, Payload: h.request}
	wait.Consistently(t, 50*time.Millisecond, "the request waits on the publication gate", func() bool {
		return len(conn.out) == 0
	})
	// The other handler's reset, under the gate it holds.
	session.BeginSceneAdmission()
	release()
	conn.in <- transport.Frame{Opcode: publicationFence}
	for _, f := range publicationThrough(t, conn, publicationFence) {
		if f.Opcode == wire.OpItemMoveResponse {
			t.Fatal("a request that waited on a reset was answered into the new scene")
		}
	}
	if calls.Load() != 0 {
		t.Fatal("the request reached its operation")
	}
}
