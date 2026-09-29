// Package gametest provides shared black-box helpers for gameplay tests.
package gametest

import (
	"bytes"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

var barrierSequence atomic.Uint64

// ActivateWorld crosses the production character-bound -> world-ready edge
// and consumes its fixed core burst (clock, base stats, vitals). E2E
// scenarios must call it exactly once after the enter-world bootstrap,
// before sending gameplay.
//
// It returns BEFORE the game-ready handler finishes: the exclusive bind and
// the one-shot world-bound hooks run after the vitals frame is queued, and
// a hook may send its own frames (the 0x3AC5 camp seed). A scenario that
// reads bind state or peers' sessions next must first consume any hook
// frames and then call AssertQueueDrained.
func ActivateWorld(t *testing.T, conn *websocket.Conn, label string) {
	t.Helper()
	sendFrame(t, conn, enterworld.OpcodeGameReady, nil)
	expectOpcode(t, conn, enterworld.OpcodeGameTime, label+" clock")
	expectOpcode(t, conn, wire.OpBaseStats, label+" base stats")
	expectOpcode(t, conn, enterworld.OpcodeVitalsUpdate, label+" vitals")
}

// AssertQueueDrained is a non-mutating FIFO barrier. The transport handles
// PING in the same per-session read loop as gameplay and enqueues PONG after
// every response synchronously produced by earlier inputs. Unlike the old
// repeated-GameReady probe, this cannot replay world-entry side effects.
func AssertQueueDrained(t *testing.T, conn *websocket.Conn, label string) {
	t.Helper()
	sequence := barrierSequence.Add(1)
	token := []byte{
		byte(sequence), byte(sequence >> 8), byte(sequence >> 16), byte(sequence >> 24),
		byte(sequence >> 32), byte(sequence >> 40), byte(sequence >> 48), byte(sequence >> 56),
	}
	sendFrame(t, conn, transport.OpPing, token)
	defer func() {
		_ = conn.SetReadDeadline(time.Time{})
	}()
	for {
		frame := readFrame(t, conn, label)
		switch frame.Opcode {
		case transport.OpPing:
			sendFrame(t, conn, transport.OpPong, frame.Payload)
		case transport.OpPong:
			if bytes.Equal(frame.Payload, token) {
				return
			}
		default:
			t.Fatalf("%s: unexpected queued frame 0x%04X payload % X", label, frame.Opcode, frame.Payload)
		}
	}
}

func expectOpcode(t *testing.T, conn *websocket.Conn, opcode uint16, label string) []byte {
	t.Helper()
	for {
		frame := readFrame(t, conn, label)
		if frame.Opcode == transport.OpPing {
			sendFrame(t, conn, transport.OpPong, frame.Payload)
			continue
		}
		if frame.Opcode == transport.OpPong {
			continue
		}
		if frame.Opcode != opcode {
			t.Fatalf("%s: next frame = 0x%04X payload % X, want opcode 0x%04X", label, frame.Opcode, frame.Payload, opcode)
		}
		return frame.Payload
	}
}

func readFrame(t *testing.T, conn *websocket.Conn, label string) transport.Frame {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatalf("%s: setting read deadline: %v", label, err)
	}
	typ, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("%s: reading ws message (frame never arrived?): %v", label, err)
	}
	if typ != websocket.BinaryMessage {
		t.Fatalf("%s: ws message type = %d, want binary", label, typ)
	}
	frame, err := transport.DecodeFrame(data)
	if err != nil {
		t.Fatalf("%s: decoding ws frame: %v", label, err)
	}
	return frame
}

func sendFrame(t *testing.T, conn *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	frame := transport.Frame{Opcode: opcode, Payload: payload}
	if err := conn.WriteMessage(websocket.BinaryMessage, frame.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}
