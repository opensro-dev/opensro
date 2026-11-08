// Package gametest provides shared black-box helpers for gameplay tests.
package gametest

import (
	"bytes"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/transport"
)

var barrierSequence atomic.Uint64

// streamed counts the stream-position frames each test client has read,
// across every reader: a resume presents that position, as the browser
// client does (#1466).
var (
	streamedMu sync.Mutex
	streamed   = map[*websocket.Conn]uint32{}
)

// NoteRead records a frame a test client read from conn. Readers outside
// this package call it for every frame they decode.
func NoteRead(conn *websocket.Conn, frame transport.Frame) {
	if !transport.CountsOnStream(frame.Opcode) {
		return
	}
	streamedMu.Lock()
	streamed[conn]++
	streamedMu.Unlock()
}

// StreamPosition is the stream position conn's test client has applied:
// the LastApplied a resume from it presents.
func StreamPosition(conn *websocket.Conn) uint32 {
	streamedMu.Lock()
	defer streamedMu.Unlock()
	return streamed[conn]
}

// ackBytes is the counted-stream batch an ack covers, and ackCadence
// the most a moved position may wait for one. The browser acks at its
// drain about once a second (network.ts STREAM_ACK_INTERVAL_MS); an
// ack per frame here would spend the session's inbound rate budget on
// acks and clamp real traffic, while a purely time-based batch could
// strand a parked writer mid-bootstrap (the harness has no tick to
// revisit it). The byte batch mirrors the byte-based ring gate: an
// ack goes out at least every ackBytes of counted stream, so the
// writer is never parked behind more than that, plus the oversized
// EnterWorld result acks at once whatever its size.
const (
	ackBytes   = 1 << 20
	ackCadence = time.Second
)

// ackSent is each test client's last acknowledged position, the counted
// bytes read since, and when the ack went out - across every reader
// (the batch is per client, not per funnel).
var (
	ackMu   sync.Mutex
	ackSent = map[*websocket.Conn]ackMark{}
)

type ackMark struct {
	pos   uint32
	bytes int
	at    time.Time
}

// AckRead records a frame a test client read and acknowledges the applied
// position, as the browser does at its drain (network.ts acknowledge,
// #1466), batched as above. The writer parks at the resume ring's bound
// until the position advances (#2146), so a bootstrap reader that never
// acks starves on any entry burst past the bound - the oversized
// EnterWorld result first of all. Every read funnel calls this for every
// frame it decodes.
func AckRead(t *testing.T, conn *websocket.Conn, frame transport.Frame) {
	t.Helper()
	if !transport.CountsOnStream(frame.Opcode) {
		return
	}
	NoteRead(conn, frame)
	pos := StreamPosition(conn)
	now := time.Now()
	ackMu.Lock()
	mark := ackSent[conn]
	mark.bytes += frame.EncodedLen()
	due := mark.bytes >= ackBytes || pos != mark.pos && now.Sub(mark.at) >= ackCadence
	if due {
		mark = ackMark{pos: pos, at: now}
	}
	ackSent[conn] = mark
	ackMu.Unlock()
	if !due {
		return
	}
	payload := make([]byte, 4)
	binary.LittleEndian.PutUint32(payload, pos)
	sendFrame(t, conn, transport.OpStreamAck, payload)
}

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
	AckRead(t, conn, frame)
	return frame
}

func sendFrame(t *testing.T, conn *websocket.Conn, opcode uint16, payload []byte) {
	t.Helper()
	frame := transport.Frame{Opcode: opcode, Payload: payload}
	if err := conn.WriteMessage(websocket.BinaryMessage, frame.Encode()); err != nil {
		t.Fatalf("writing frame 0x%04X: %v", opcode, err)
	}
}
