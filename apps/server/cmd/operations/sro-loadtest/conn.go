/*
===========================================================================

conn.go - one bot's game connection: WebSocket frames and stream acks

The bot speaks the production transport: HELLO with the Agent-minted
admission ticket, WELCOME, then ordered native frames. Like the browser
client, it counts a frame toward its stream position only when it has
applied (read) it, and acknowledges that position about once a second,
so the server's resume ring behaves under load exactly as it does for
real clients.

===========================================================================
*/

package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"opensro.online/server/internal/transport"
)

// gameConn is one bot's WebSocket session. Frames arrive on the channel
// readLoop fills; writes are serialized; applied counts the counted
// opcodes read so far (the stream position the resume ring covers). The
// counters are atomic: readLoop writes them while the ack loop and the
// owning bot read them on other goroutines. Whether an opcode counts is
// the transport's own rule (transport.CountsOnStream), so the bot's ack
// position can never drift from the server's ring if the rule moves.
type gameConn struct {
	ws      *websocket.Conn
	writeMu sync.Mutex
	frames  chan transport.Frame
	done    chan struct{}
	applied atomic.Uint32
	bytesIn atomic.Uint64
}

/*
================
dialGame

Connect the WebSocket lane and complete HELLO -> WELCOME with the
one-use admission ticket. Pings met on the way are answered, as the
probe client does; anything else before WELCOME is a named failure.
================
*/
func dialGame(ctx context.Context, base, ticket string, timeout time.Duration) (*gameConn, transport.Welcome, error) {
	address := strings.TrimSuffix(base, "/") + transport.PathWS
	scheme := "ws://"
	if strings.HasPrefix(base, "https://") {
		scheme = "wss://"
	}
	address = scheme + strings.TrimPrefix(strings.TrimPrefix(address, "http://"), "https://")
	dialer := websocket.Dialer{Proxy: http.ProxyFromEnvironment, HandshakeTimeout: timeout}
	ws, resp, err := dialer.DialContext(ctx, address, nil)
	if err != nil {
		if resp != nil && resp.Body != nil {
			_ = resp.Body.Close()
		}
		return nil, transport.Welcome{}, fmt.Errorf("dial: %w", err)
	}
	ws.SetReadLimit(int64(transport.MaxFrameBytes) + 4)
	// The dial context ends at the handshake, so the HELLO -> WELCOME
	// wait needs its own bound: a connected but silent GameWorld must
	// fail the bot at cfg.timeout, not hang the step past its report.
	// The deadline also covers any pings met on the way and is cleared
	// once WELCOME lands.
	_ = ws.SetReadDeadline(time.Now().Add(timeout))
	c := &gameConn{ws: ws, frames: make(chan transport.Frame, 256), done: make(chan struct{})}
	if err := c.writeFrame(transport.Frame{
		Opcode:  transport.OpHello,
		Payload: transport.EncodeHello(transport.Hello{AdmissionToken: []byte(ticket)}),
	}); err != nil {
		_ = ws.Close()
		return nil, transport.Welcome{}, fmt.Errorf("hello: %w", err)
	}
	for {
		frame, err := c.readFrame()
		if err != nil {
			_ = ws.Close()
			return nil, transport.Welcome{}, fmt.Errorf("welcome: %w", err)
		}
		if frame.Opcode == transport.OpPing {
			if err := c.writeFrame(transport.Frame{Opcode: transport.OpPong, Payload: frame.Payload}); err != nil {
				_ = ws.Close()
				return nil, transport.Welcome{}, fmt.Errorf("welcome pong: %w", err)
			}
			continue
		}
		if frame.Opcode != transport.OpWelcome {
			_ = ws.Close()
			return nil, transport.Welcome{}, fmt.Errorf("welcome: first frame = 0x%04X, want WELCOME", frame.Opcode)
		}
		welcome, err := transport.DecodeWelcome(frame.Payload)
		if err != nil {
			_ = ws.Close()
			return nil, transport.Welcome{}, fmt.Errorf("welcome: %w", err)
		}
		_ = ws.SetReadDeadline(time.Time{})
		return c, welcome, nil
	}
}

/*
================
readFrame
================
*/
func (c *gameConn) readFrame() (transport.Frame, error) {
	kind, body, err := c.ws.ReadMessage()
	if err != nil {
		return transport.Frame{}, err
	}
	if kind != websocket.BinaryMessage {
		return transport.Frame{}, fmt.Errorf("websocket message type %d is not binary", kind)
	}
	c.bytesIn.Add(uint64(len(body)))
	return transport.DecodeFrame(body)
}

/*
================
writeFrame
================
*/
func (c *gameConn) writeFrame(frame transport.Frame) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.ws.WriteMessage(websocket.BinaryMessage, frame.Encode())
}

/*
================
readLoop

Fill the frame channel until the socket dies. Every counted frame read
advances the applied stream position, in arrival order.
================
*/
func (c *gameConn) readLoop() {
	defer close(c.frames)
	defer close(c.done)
	for {
		frame, err := c.readFrame()
		if err != nil {
			return
		}
		if transport.CountsOnStream(frame.Opcode) {
			c.applied.Add(1)
		}
		select {
		case c.frames <- frame:
		default:
			// A bot that stops draining must not stall the reader: drop
			// to the error path by closing, counted as a failed bot.
			_ = c.ws.Close()
			return
		}
	}
}

/*
================
appliedPosition
================
*/
func (c *gameConn) appliedPosition() uint32 {
	return c.applied.Load()
}

/*
================
ackLoop

Acknowledge the applied stream position about once a second, the
browser client's cadence, until the context ends.
================
*/
func (c *gameConn) ackLoop(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		case <-ticker.C:
			var payload [4]byte
			binary.LittleEndian.PutUint32(payload[:], c.appliedPosition())
			_ = c.writeFrame(transport.Frame{Opcode: transport.OpStreamAck, Payload: payload[:]})
		}
	}
}

/*
================
close
================
*/
func (c *gameConn) close() {
	_ = c.ws.Close()
}
