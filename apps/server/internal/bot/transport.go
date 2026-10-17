/*
===========================================================================

transport.go - the bot's WebSocket connection and admission sequence

The bot connects to the GameWorld transport over the loopback WebSocket and
runs the ordinary admission path: HELLO, EnterWorld, GameReady. It reuses
the production transport codecs, so a bot is a normal client on the wire.

Phase 1 of issue #74 (issue #75).

===========================================================================
*/
package bot

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// ErrReadTimeout is returned when Read runs out of its time budget. It is not
// a connection failure, so a caller waits for the next frame instead of
// closing the session.
var ErrReadTimeout = errors.New("bot: read timeout")

// Socket is one live transport connection over WebSocket. One reader
// goroutine owns the connection's reads, because a gorilla read timeout is
// permanent. The callers select on the frame channel with their own timeout.
type Socket struct {
	conn    *websocket.Conn
	frames  chan transport.Frame
	done    chan struct{}
	mu      sync.Mutex
	writeMu sync.Mutex
	err     error
}

// DialWebSocket connects to the transport WebSocket URL, for example
// ws://127.0.0.1:4460/transport/ws.
func DialWebSocket(ctx context.Context, url string, timeout time.Duration) (*Socket, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	dialer := websocket.Dialer{HandshakeTimeout: timeout}
	conn, response, err := dialer.DialContext(ctx, url, nil)
	if err != nil {
		if response != nil {
			return nil, fmt.Errorf("bot: dial %s: status %d: %w", url, response.StatusCode, err)
		}
		return nil, fmt.Errorf("bot: dial %s: %w", url, err)
	}
	s := &Socket{
		conn:   conn,
		frames: make(chan transport.Frame, 256),
		done:   make(chan struct{}),
	}
	go s.pump()
	return s, nil
}

// pump reads frames until the connection ends. It answers keepalive traffic
// inline and delivers every other frame to the caller.
func (s *Socket) pump() {
	defer close(s.done)
	for {
		kind, data, err := s.conn.ReadMessage()
		if err != nil {
			s.setErr(err)
			return
		}
		if kind != websocket.BinaryMessage {
			continue
		}
		frame, err := transport.DecodeFrame(data)
		if err != nil {
			s.setErr(fmt.Errorf("bot: decode frame: %w", err))
			return
		}
		switch frame.Opcode {
		case transport.OpPing:
			if err := s.Write(transport.OpPong, frame.Payload); err != nil {
				s.setErr(err)
				return
			}
			continue
		case transport.OpPong:
			continue
		}
		select {
		case s.frames <- frame:
		case <-s.done:
			return
		}
	}
}

func (s *Socket) setErr(err error) {
	s.mu.Lock()
	if s.err == nil {
		s.err = err
	}
	s.mu.Unlock()
}

func (s *Socket) readErr() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err == nil {
		return fmt.Errorf("bot: connection ended")
	}
	return s.err
}

// Write sends one reliable frame.
func (s *Socket) Write(opcode uint16, payload []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.conn.WriteMessage(websocket.BinaryMessage, transport.Frame{Opcode: opcode, Payload: payload}.Encode())
}

// Read returns the next game frame, or a timeout after the given duration.
func (s *Socket) Read(ctx context.Context, timeout time.Duration) (transport.Frame, error) {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case frame := <-s.frames:
		return frame, nil
	case <-s.done:
		// Drain a buffered frame that raced the close.
		select {
		case frame := <-s.frames:
			return frame, nil
		default:
		}
		return transport.Frame{}, s.readErr()
	case <-timer.C:
		return transport.Frame{}, ErrReadTimeout
	case <-ctx.Done():
		return transport.Frame{}, ctx.Err()
	}
}

// ReadUntil returns the first frame with one of the wanted opcodes.
func (s *Socket) ReadUntil(ctx context.Context, timeout time.Duration, wanted ...uint16) (transport.Frame, error) {
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return transport.Frame{}, fmt.Errorf("bot: timeout waiting for opcode(s) %v", wanted)
		}
		frame, err := s.Read(ctx, remaining)
		if err != nil {
			return transport.Frame{}, err
		}
		for _, opcode := range wanted {
			if frame.Opcode == opcode {
				return frame, nil
			}
		}
	}
}

// Close closes the socket.
func (s *Socket) Close() error {
	return s.conn.Close()
}

// Admit runs the ordinary admission path on an open socket: HELLO, wait for
// WELCOME, EnterWorld, wait for the bootstrap finalize, GameReady, wait for
// the vitals frame. The tickets come from the Agent (agent.go).
//
// It returns the object-list create rows (0x3417) it saw between the 0x30CB
// start and the 0x330A finalize, so the caller can build the initial world
// view of the brain. It also returns the first vitals refresh, which carries
// the bot's own object id and current HP, so the brain can seed its self view.
// OpcodeSceneReset is the 0x3369 scene reset. The server sends it on a region
// change, so the bot drops the stale view of the old region.
const OpcodeSceneReset uint16 = enterworld.OpcodeResetClient

func Admit(ctx context.Context, socket *Socket, admissionToken, enterWorldToken, divisionID, characterName string, timeout time.Duration) (transport.Welcome, [][]byte, Vitals, []byte, uint16, error) {
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if err := socket.Write(transport.OpHello, transport.EncodeHello(transport.Hello{
		AdmissionToken: []byte(admissionToken),
	})); err != nil {
		return transport.Welcome{}, nil, Vitals{}, nil, 0, fmt.Errorf("bot: send HELLO: %w", err)
	}
	hello, err := socket.ReadUntil(ctx, timeout, transport.OpWelcome, transport.OpBye)
	if err != nil {
		return transport.Welcome{}, nil, Vitals{}, nil, 0, fmt.Errorf("bot: wait WELCOME: %w", err)
	}
	if hello.Opcode == transport.OpBye {
		return transport.Welcome{}, nil, Vitals{}, nil, 0, fmt.Errorf("bot: HELLO refused (BYE reason %d)", byeReason(hello.Payload))
	}
	welcome, err := transport.DecodeWelcome(hello.Payload)
	if err != nil {
		return transport.Welcome{}, nil, Vitals{}, nil, 0, fmt.Errorf("bot: decode WELCOME: %w", err)
	}
	if err := socket.Write(transport.OpEnterWorld, transport.EncodeEnterWorld(transport.EnterWorld{
		Division:  divisionID,
		CharName:  characterName,
		AuthToken: []byte(enterWorldToken),
	})); err != nil {
		return welcome, nil, Vitals{}, nil, 0, fmt.Errorf("bot: send EnterWorld: %w", err)
	}
	var rows [][]byte
	var local []byte
	var resetRegion uint16
	deadline := time.Now().Add(timeout)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return welcome, rows, Vitals{}, local, resetRegion, fmt.Errorf("bot: wait EnterWorld: timeout")
		}
		frame, err := socket.Read(ctx, remaining)
		if err != nil {
			return welcome, rows, Vitals{}, local, resetRegion, fmt.Errorf("bot: wait EnterWorld: %w", err)
		}
		switch frame.Opcode {
		case OpcodeObjectListChunk:
			rows = append(rows, append([]byte(nil), frame.Payload...))
			continue
		case enterworld.OpcodeResetClient:
			// The reset that opens the bootstrap carries the start region.
			if len(frame.Payload) >= 2 {
				resetRegion = binary.LittleEndian.Uint16(frame.Payload[:2])
			}
			continue
		case enterworld.OpcodeMyCharacterChunk:
			// The local player's own char data carries the start position.
			local = append([]byte(nil), frame.Payload...)
			continue
		case enterworld.OpcodeObjectListFinalize:
		case transport.OpEnterWorldResult:
			result, err := transport.DecodeEnterWorldResult(frame.Payload)
			if err != nil {
				return welcome, rows, Vitals{}, local, resetRegion, fmt.Errorf("bot: decode EnterWorldResult: %w", err)
			}
			if !result.OK {
				return welcome, rows, Vitals{}, local, resetRegion, fmt.Errorf("bot: EnterWorld refused (native error 0x%X)", result.NativeErrorCode)
			}
			// The server sends the bootstrap packets (the object list)
			// after the result, so keep reading until the finalize.
			continue
		default:
			continue
		}
		break
	}
	if err := socket.Write(enterworld.OpcodeGameReady, nil); err != nil {
		return welcome, rows, Vitals{}, local, resetRegion, fmt.Errorf("bot: send GameReady: %w", err)
	}
	vitalsFrame, err := socket.ReadUntil(ctx, timeout, enterworld.OpcodeVitalsUpdate)
	if err != nil {
		return welcome, rows, Vitals{}, local, resetRegion, fmt.Errorf("bot: wait GameReady: %w", err)
	}
	vitals, _ := ParseVitals(vitalsFrame.Payload)
	return welcome, rows, vitals, local, resetRegion, nil
}

func byeReason(payload []byte) uint8 {
	if len(payload) == 0 {
		return 0
	}
	return payload[0]
}
