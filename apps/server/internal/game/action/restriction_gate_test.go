/*
===========================================================================

restriction_gate_test.go - the command-restriction gates at the door

A session carrying the trade restriction loses the exchange request,
the stall create and visit, the Open Market opening and the item
move's drop arms at the message door: the operation never runs and
the one answer is the restriction notice. The chat kind does not gate
them, a bag move is not a drop, and a cleared restriction lets the
request through again. A restriction on the hub's by-character record
is on the session from the bind (the entry load).

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

var restrictionStamp = [8]uint16{2027, 3, 1, 9, 8, 30, 0, 0}

/*
================
restrictionGateHarness

The publication harness with the five gated operations registered on
counting answers: a call means the gate let the request through.
================
*/
func restrictionGateHarness(t *testing.T) (*publicationHarness, *atomic.Int32) {
	t.Helper()
	h := newPublicationHarness(t, false, 1)
	var calls atomic.Int32
	answer := func(string, *enterworld.Character, []byte) OpResult {
		calls.Add(1)
		return OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemMoveResponse, Payload: []byte{2, 0}}}}
	}
	hub := h.server.Hub
	hub.Handle(wire.OpExchangeRequest, h.rt.restrictedHandler(hub, transport.CommandRestrictionTrade, answer))
	hub.Handle(wire.OpStallCreate, h.rt.restrictedHandler(hub, transport.CommandRestrictionTrade, answer))
	hub.Handle(wire.OpStallVisit, h.rt.restrictedHandler(hub, transport.CommandRestrictionTrade, answer))
	hub.Handle(wire.OpStallNetworkSearch, h.rt.restrictedHandler(hub, transport.CommandRestrictionTrade, answer))
	hub.Handle(wire.OpItemMoveRequest, h.rt.dropRestrictedHandler(hub, answer))
	return h, &calls
}

/*
================
fire

One request and a fence behind it; everything the request produced.
================
*/
func (h *publicationHarness) fire(t *testing.T, conn *publicationConn, opcode uint16, payload []byte) []transport.Frame {
	t.Helper()
	conn.in <- transport.Frame{Opcode: opcode, Payload: payload}
	conn.in <- transport.Frame{Opcode: publicationFence}
	return publicationThrough(t, conn, publicationFence)
}

/*
================
assertGate

The request died at the door: the operation never ran and the one
answer is the trade notice carrying the restriction's stamp.
================
*/
func assertGate(t *testing.T, frames []transport.Frame, calls *atomic.Int32) {
	t.Helper()
	if calls.Load() != 0 {
		t.Fatal("a restricted request reached its operation")
	}
	var notices int
	for _, f := range frames {
		if f.Opcode == wire.OpItemMoveResponse {
			t.Fatal("a restricted request was answered by its operation")
		}
		if f.Opcode == wire.OpGMRestrictionNotice {
			notices++
			want := wire.EncodeGMRestrictionNotice(1, restrictionStamp)
			if string(f.Payload) != string(want) {
				t.Fatalf("notice payload = % X, want % X", f.Payload, want)
			}
		}
	}
	if notices != 1 {
		t.Fatalf("notices = %d, want exactly one", notices)
	}
}

/*
================
TestTradeRestrictionDropsTheGatedRequests
================
*/
func TestTradeRestrictionDropsTheGatedRequests(t *testing.T) {
	h, calls := restrictionGateHarness(t)
	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	h.ready(t, conn)
	sessions := h.server.Hub.CharacterSessions(testDivision, h.character.Name)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want the entered one", len(sessions))
	}
	session := sessions[0]
	if !session.SetCommandRestriction(transport.CommandRestrictionTrade, restrictionStamp) {
		t.Fatal("the trade restriction did not install")
	}
	assertGate(t, h.fire(t, conn, wire.OpExchangeRequest, []byte{1, 0, 0, 0}), calls)
	assertGate(t, h.fire(t, conn, wire.OpStallCreate, []byte{0}), calls)
	assertGate(t, h.fire(t, conn, wire.OpStallVisit, []byte{0}), calls)
	assertGate(t, h.fire(t, conn, wire.OpStallNetworkSearch, []byte{0}), calls)
	assertGate(t, h.fire(t, conn, wire.OpItemMoveRequest, []byte{wire.MoveTypeGroundDrop, 20}), calls)
	assertGate(t, h.fire(t, conn, wire.OpItemMoveRequest, []byte{wire.MoveTypeGoldDrop, 1, 0, 0, 0, 0, 0, 0, 0}), calls)

	// A bag move is not a drop: the same request stays open.
	frames := h.fire(t, conn, wire.OpItemMoveRequest, []byte{0, 20, 21})
	if calls.Load() != 1 {
		t.Fatal("a bag move was gated as a drop")
	}
	answered := false
	for _, f := range frames {
		if f.Opcode == wire.OpGMRestrictionNotice {
			t.Fatal("a bag move drew the restriction notice")
		}
		if f.Opcode == wire.OpItemMoveResponse {
			answered = true
		}
	}
	if !answered {
		t.Fatal("a bag move went unanswered")
	}

	// The chat kind does not gate trade, and a clear reopens it.
	if !session.SetCommandRestriction(transport.CommandRestrictionChat, restrictionStamp) ||
		!session.ClearCommandRestriction(transport.CommandRestrictionTrade) {
		t.Fatal("the restriction bookkeeping refused")
	}
	calls.Store(0)
	frames = h.fire(t, conn, wire.OpExchangeRequest, []byte{1, 0, 0, 0})
	if calls.Load() != 1 {
		t.Fatal("a cleared restriction still gated the exchange")
	}
	for _, f := range frames {
		if f.Opcode == wire.OpGMRestrictionNotice {
			t.Fatal("a cleared restriction still sent its notice")
		}
	}
}

/*
================
TestRegisteredTradeGatesDropAtTheDoor

The production registration itself: each gated opcode, fired by a
trade-restricted session through Register's own wiring, draws exactly
one trade notice and nothing else; a bag move draws none.
================
*/
func TestRegisteredTradeGatesDropAtTheDoor(t *testing.T) {
	h := newPublicationHarness(t, false, 1)
	h.rt.Register(h.server.Hub)
	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	h.ready(t, conn)
	sessions := h.server.Hub.CharacterSessions(testDivision, h.character.Name)
	if len(sessions) != 1 {
		t.Fatalf("sessions = %d, want the entered one", len(sessions))
	}
	if !sessions[0].SetCommandRestriction(transport.CommandRestrictionTrade, restrictionStamp) {
		t.Fatal("the trade restriction did not install")
	}
	notices := func(frames []transport.Frame) int {
		count := 0
		for _, f := range frames {
			if f.Opcode == wire.OpGMRestrictionNotice {
				count++
				if string(f.Payload) != string(wire.EncodeGMRestrictionNotice(1, restrictionStamp)) {
					t.Fatalf("notice payload = % X, want the trade channel with the stamp", f.Payload)
				}
			}
		}
		return count
	}
	for _, tc := range []struct {
		opcode  uint16
		payload []byte
	}{
		{wire.OpExchangeRequest, []byte{1, 0, 0, 0}},
		{wire.OpStallCreate, []byte{0}},
		{wire.OpStallVisit, []byte{0}},
		{wire.OpStallNetworkSearch, []byte{0}},
		{wire.OpItemMoveRequest, []byte{wire.MoveTypeGroundDrop, 20}},
		{wire.OpItemMoveRequest, []byte{wire.MoveTypeGoldDrop, 1, 0, 0, 0, 0, 0, 0, 0}},
	} {
		if got := notices(h.fire(t, conn, tc.opcode, tc.payload)); got != 1 {
			t.Fatalf("opcode 0x%04X drew %d notices, want one", tc.opcode, got)
		}
	}
	// A bag move through the same registration draws none.
	frames := h.fire(t, conn, wire.OpItemMoveRequest, []byte{0, 20, 21})
	if got := notices(frames); got != 0 {
		t.Fatalf("a bag move drew %d notices, want none", got)
	}
}

/*
================
TestTradeRestrictionOnTheHubRecordGatesFromTheBind

The entry load: a restriction recorded against the character before
the session exists is on the session from its bind, so the first
request of the entry already dies at the door.
================
*/
func TestTradeRestrictionOnTheHubRecordGatesFromTheBind(t *testing.T) {
	h, calls := restrictionGateHarness(t)
	if !h.server.Hub.SetCharacterRestriction(testDivision, h.character.Name, transport.CommandRestrictionTrade, restrictionStamp) {
		t.Fatal("the hub record did not install")
	}
	conn, _ := h.connect(t, nil)
	h.enter(t, conn)
	h.ready(t, conn)
	assertGate(t, h.fire(t, conn, wire.OpExchangeRequest, []byte{1, 0, 0, 0}), calls)
	assertGate(t, h.fire(t, conn, wire.OpItemMoveRequest, []byte{wire.MoveTypeGroundDrop, 20}), calls)
}
