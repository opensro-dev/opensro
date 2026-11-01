package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// wsWriteTimeout bounds one stall in a WebSocket write, re-armed as
// the message progresses (writeAllWithinStall): a wedged peer cannot
// stall the session's write loop forever, and a large message whose
// peer is still reading is never killed by its own size (#2311); the
// session-level idle logic handles the rest.
const wsWriteTimeout = 10 * time.Second

// wsConn adapts a gorilla WebSocket connection to Conn. Frames map 1:1 to
// binary WebSocket messages with no extra framing.
type wsConn struct {
	c      *websocket.Conn
	frames chan Frame
	closed chan struct{}

	// writeMu serializes WriteFrame with Close's graceful close-frame
	// write: gorilla panics on concurrent writers. Close takes it only
	// when free (TryLock); a parked write means the socket is closed
	// directly instead (see Close).
	writeMu sync.Mutex

	readErrMu sync.Mutex
	readErr   error
	readDone  chan struct{}

	closeOnce sync.Once
}

func newWSConn(c *websocket.Conn) *wsConn {
	c.SetReadLimit(MaxInboundFrameBytes)
	w := &wsConn{
		c:        c,
		frames:   make(chan Frame, 16),
		closed:   make(chan struct{}),
		readDone: make(chan struct{}),
	}
	go w.pump()
	return w
}

func (w *wsConn) pump() {
	defer close(w.readDone)
	for {
		typ, data, err := w.c.ReadMessage()
		if err != nil {
			w.setReadErr(err)
			return
		}
		if typ != websocket.BinaryMessage {
			w.setReadErr(fmt.Errorf("transport: non-binary websocket message type %d", typ))
			// Close (not w.c.Close) so the teardown holds writeMu and cannot
			// race a WriteFrame in flight.
			w.Close("non-binary message")
			return
		}
		f, err := DecodeFrame(data)
		if err != nil {
			w.setReadErr(err)
			w.Close("malformed frame")
			return
		}
		select {
		case w.frames <- f:
		case <-w.closed:
			return
		}
	}
}

func (w *wsConn) setReadErr(err error) {
	w.readErrMu.Lock()
	if w.readErr == nil {
		w.readErr = err
	}
	w.readErrMu.Unlock()
}

func (w *wsConn) getReadErr() error {
	w.readErrMu.Lock()
	defer w.readErrMu.Unlock()
	if w.readErr == nil {
		return errors.New("transport: websocket closed")
	}
	return w.readErr
}

func (w *wsConn) ReadFrame(ctx context.Context) (Frame, error) {
	select {
	case f := <-w.frames:
		return f, nil
	case <-w.readDone:
		// Drain frames that raced with the pump exiting.
		select {
		case f := <-w.frames:
			return f, nil
		default:
			return Frame{}, w.getReadErr()
		}
	case <-ctx.Done():
		return Frame{}, ctx.Err()
	}
}

func (w *wsConn) WriteFrame(f Frame) error {
	if f.EncodedLen() > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	select {
	case <-w.closed:
		return errors.New("transport: websocket closed")
	default:
	}
	// One message, written in pieces with the deadline re-armed on
	// progress: the bytes on the wire are exactly f.Encode(), only
	// the pacing changed (gorilla frames the pieces itself).
	arm := func() { _ = w.c.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) }
	arm()
	writer, err := w.c.NextWriter(websocket.BinaryMessage)
	if err != nil {
		return err
	}
	if err := writeAllWithinStall(writer, f.Encode(), arm); err != nil {
		// The message is unfinished; the write-error path detaches
		// and closes the socket, so the writer is never closed.
		return err
	}
	arm()
	return writer.Close()
}

func (w *wsConn) SupportsUnreliable() bool { return false }

func (w *wsConn) WriteUnreliable(Frame) error {
	return errors.New("transport: websocket has no unreliable channel")
}

// Close tears the connection down. When no write is in flight it sends
// the graceful close frame first, as before. When the session's write
// loop is parked inside WriteFrame on a peer that stopped reading, that
// write holds writeMu until its 10 s deadline; queueing behind it made a
// resume's WELCOME wait out the remainder (~8 s observed, #1453). The
// underlying socket close is concurrency-safe (gorilla: "The Close and
// WriteControl methods can be called concurrently with all other
// methods") and aborts the parked write at once, so take that path
// instead: a peer that is not reading will not read a close frame
// either. A write aborted this way errors and the write loop detaches;
// the frame keeps its stream position, and a resume re-sends it only
// if the client's position says it never arrived (stream_ring.go).
func (w *wsConn) Close(reason string) error {
	var err error
	w.closeOnce.Do(func() {
		close(w.closed)
		if w.writeMu.TryLock() {
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason)
			_ = w.c.SetWriteDeadline(time.Now().Add(2 * time.Second))
			_ = w.c.WriteMessage(websocket.CloseMessage, msg)
			err = w.c.Close()
			w.writeMu.Unlock()
			return
		}
		err = w.c.Close()
	})
	return err
}

func (w *wsConn) Kind() string { return "websocket" }

func (w *wsConn) RemoteAddr() string { return w.c.RemoteAddr().String() }
