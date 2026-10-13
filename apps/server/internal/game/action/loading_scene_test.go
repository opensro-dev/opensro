/*
===========================================================================

loading_scene_test.go - tests for hubHandler's loading-scene discard

===========================================================================
*/

package action

import (
	"sync/atomic"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
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
