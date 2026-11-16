package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
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

// wsCompressMinBytes is the frame size at which WriteFrame asks for
// permessage-deflate on that one message (#2311). A browser delivers
// a WebSocket message to the client only whole, so the ~930 KB
// EnterWorld record is one silent atomic unit on the fallback no
// matter how the write is paced; deflating it (snapshot JSON lands
// roughly 10x smaller) cuts that silent stretch proportionally.
// Small per-tick frames stay uncompressed: their flate cost would
// buy nothing. Compression is negotiated at upgrade and toggled per
// message, so Frame bytes, the resume ring and the WT path never see
// it, and a peer that did not negotiate it is written to exactly as
// before (EnableWriteCompression is then a no-op).
const wsCompressMinBytes = 32 << 10

// wsWriteBufferSize is the upgrader's write buffer (server.go). gorilla
// writes a message whose encoded frame fits it as one unfragmented
// WebSocket frame, so WriteFrames can build the same bytes itself for
// every frame up to this size; a larger frame keeps gorilla's own path.
const wsWriteBufferSize = 4096

// wsControlWriteWait bounds a control reply (pong, close) the read pump
// writes, gorilla's own default (writeWait).
const wsControlWriteWait = time.Second

// wsConn adapts a gorilla WebSocket connection to Conn. Frames map 1:1 to
// binary WebSocket messages with no extra framing.
type wsConn struct {
	c      *websocket.Conn
	frames chan Frame
	closed chan struct{}

	// clientAddr and userAgent are the accept request's client facts,
	// set by the listener before AcceptConn (server.go): the real
	// client behind the local edge (clientaddr.go) and its User-Agent.
	// Empty for in-process connections, which keep the raw peer.
	clientAddr string
	userAgent  string

	// writeMu serializes WriteFrame and WriteFrames with Close's graceful
	// close-frame write and the read pump's control replies: gorilla
	// panics on concurrent writers, and WriteFrames writes the socket
	// beneath gorilla. Close takes it only when free (TryLock); a parked
	// write means the socket is closed directly instead (see Close).
	writeMu sync.Mutex
	// batch is WriteFrames' reusable buffer, owned under writeMu.
	batch []byte

	readErrMu sync.Mutex
	readErr   error
	readDone  chan struct{}

	closeOnce sync.Once
}

func newWSConn(c *websocket.Conn) *wsConn {
	c.SetReadLimit(MaxInboundFrameBytes)
	// Stay uncompressed until WriteFrame opts a large message in:
	// negotiation alone would otherwise deflate every small frame.
	c.EnableWriteCompression(false)
	w := &wsConn{
		c:        c,
		frames:   make(chan Frame, 16),
		closed:   make(chan struct{}),
		readDone: make(chan struct{}),
	}
	// WriteFrames writes the socket beneath gorilla, so the replies
	// gorilla's reader would write on its own take writeMu first.
	c.SetPingHandler(w.replyPing)
	c.SetCloseHandler(w.replyClose)
	go w.pump()
	return w
}

/*
================
wsConn.replyPing

gorilla's default ping reply, serialized with the writer: a client that
pings waits behind a write in flight instead of cutting into its bytes.
Browsers never send WebSocket pings; the session's keepalive is its own
PING frame.
================
*/
func (w *wsConn) replyPing(data string) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	err := w.c.WriteControl(websocket.PongMessage, []byte(data), time.Now().Add(wsControlWriteWait))
	var netErr net.Error
	if errors.Is(err, websocket.ErrCloseSent) || errors.As(err, &netErr) && netErr.Timeout() {
		return nil
	}
	return err
}

/*
================
wsConn.replyClose

gorilla's default close reply, written only when no write is in flight:
the connection is torn down right after the peer's close either way, and
a parked write means the peer is not reading (the rule Close follows).
================
*/
func (w *wsConn) replyClose(code int, _ string) error {
	if !w.writeMu.TryLock() {
		return nil
	}
	defer w.writeMu.Unlock()
	_ = w.c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, ""), time.Now().Add(wsControlWriteWait))
	return nil
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
	return w.writeMessageLocked(f)
}

/*
================
wsConn.writeMessageLocked

One frame as one gorilla message. Caller holds writeMu.
================
*/
func (w *wsConn) writeMessageLocked(f Frame) error {
	// One message, written in pieces with the deadline re-armed on
	// progress: the bytes on the wire are exactly f.Encode(), only
	// the pacing changed (gorilla frames the pieces itself). A large
	// message additionally rides permessage-deflate when the peer
	// negotiated it (wsCompressMinBytes): the client still receives
	// exactly f.Encode(), reassembled and inflated by its side of
	// the socket, but the silent whole-message stretch on a slow
	// fallback link shrinks with the compressed size.
	arm := func() { _ = w.c.SetWriteDeadline(time.Now().Add(wsWriteTimeout)) }
	arm()
	w.c.EnableWriteCompression(f.EncodedLen() >= wsCompressMinBytes)
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

/*
================
wsConn.WriteFrames

Several frames as the same binary messages WriteFrame sends, written to
the socket in one write (session_write.go). A frame that fits gorilla's
write buffer is one unfragmented, unmasked, uncompressed frame on the
wire, and that is the frame built here; a larger one ends the run and
goes through gorilla (fragmentation, deflate) as before, so every byte
the client reads is the byte WriteFrame would have sent. The stall
deadline re-arms per 32 KiB piece exactly as for one message.
================
*/
func (w *wsConn) WriteFrames(frames []Frame) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	select {
	case <-w.closed:
		return errors.New("transport: websocket closed")
	default:
	}
	arm := func() { _ = w.c.NetConn().SetWriteDeadline(time.Now().Add(wsWriteTimeout)) }
	buf := w.batch[:0]
	defer func() {
		// Keep a buffer of the usual size; drop one a backlog grew.
		if cap(buf) <= 2*writeBatchMaxBytes {
			w.batch = buf[:0]
		}
	}()
	for _, f := range frames {
		if f.EncodedLen() > MaxFrameBytes {
			if err := writeAllWithinStall(w.c.NetConn(), buf, arm); err != nil {
				return err
			}
			return ErrFrameTooLarge
		}
		if f.EncodedLen() <= wsWriteBufferSize {
			buf = appendWSBinaryFrame(buf, f)
			continue
		}
		if err := writeAllWithinStall(w.c.NetConn(), buf, arm); err != nil {
			return err
		}
		buf = buf[:0]
		if err := w.writeMessageLocked(f); err != nil {
			return err
		}
	}
	return writeAllWithinStall(w.c.NetConn(), buf, arm)
}

/*
================
appendWSBinaryFrame

One final, unmasked, uncompressed binary WebSocket frame carrying
f.Encode() (RFC 6455 section 5.2): the header gorilla's flushFrame
writes for a server message that fits its buffer.
================
*/
func appendWSBinaryFrame(buf []byte, f Frame) []byte {
	const (
		finBinary = 0x80 | websocket.BinaryMessage
		len16     = 126
		len64     = 127
	)
	n := f.EncodedLen()
	switch {
	case n >= 1<<16:
		buf = append(buf, finBinary, len64)
		buf = binary.BigEndian.AppendUint64(buf, uint64(n))
	case n > 125:
		buf = append(buf, finBinary, len16)
		buf = binary.BigEndian.AppendUint16(buf, uint16(n))
	default:
		buf = append(buf, finBinary, byte(n))
	}
	buf = binary.LittleEndian.AppendUint16(buf, f.Opcode)
	return append(buf, f.Payload...)
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
//
// The close frame itself is written from its own goroutine (#2142): a
// game goroutine closes a session while it holds a division lane (an
// outbound overflow, an eviction), and a peer that stopped reading made
// this write wait out its whole deadline with the lane held, stalling
// every player in the division. Close returns at once; the goroutine owns
// writeMu until the frame is written or its deadline passes, then closes
// the socket.
func (w *wsConn) Close(reason string) error {
	var err error
	w.closeOnce.Do(func() {
		close(w.closed)
		if w.writeMu.TryLock() {
			msg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason)
			go func() {
				defer w.writeMu.Unlock()
				_ = w.c.SetWriteDeadline(time.Now().Add(wsCloseFrameDeadline))
				_ = w.c.WriteMessage(websocket.CloseMessage, msg)
				_ = w.c.Close()
			}()
			return
		}
		err = w.c.Close()
	})
	return err
}

// wsCloseFrameDeadline bounds the close frame's write; the socket closes
// when it passes even if the peer never read the frame.
const wsCloseFrameDeadline = 2 * time.Second

func (w *wsConn) Kind() string { return "websocket" }

/*
================
RemoteAddr

The client this connection serves: the accept request's forwarded
client when the listener recorded one, else the socket's peer.
================
*/
func (w *wsConn) RemoteAddr() string {
	if w.clientAddr != "" {
		return w.clientAddr
	}
	return w.c.RemoteAddr().String()
}

/*
================
ClientUserAgent

The accept request's User-Agent, "" when none was recorded.
================
*/
func (w *wsConn) ClientUserAgent() string { return w.userAgent }
