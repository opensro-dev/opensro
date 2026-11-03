package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/quic-go/webtransport-go"
	log "github.com/sirupsen/logrus"
)

// wtStreamAcceptTimeout bounds how long we wait for the client to open its
// control stream; the session-level HELLO timeout is the outer clock, this
// one only keeps WriteFrame from waiting on a stream that never comes.
const wtStreamAcceptTimeout = 15 * time.Second

// wtConn adapts a WebTransport session to Conn. The client opens one
// bidirectional stream (the control stream) carrying length-prefixed frames
// — the reliable, ordered channel. Datagrams carry bare frames and merge
// into the same inbound queue.
type wtConn struct {
	sess   *webtransport.Session
	frames chan Frame
	closed chan struct{}

	ctx       context.Context
	ctxCancel context.CancelFunc

	strReady chan struct{}
	str      *webtransport.Stream

	readErrMu sync.Mutex
	readErr   error
	readDone  chan struct{}

	closeOnce sync.Once
}

func newWTConn(sess *webtransport.Session) *wtConn {
	ctx, cancel := context.WithCancel(context.Background())
	w := &wtConn{
		sess:      sess,
		frames:    make(chan Frame, 16),
		closed:    make(chan struct{}),
		ctx:       ctx,
		ctxCancel: cancel,
		strReady:  make(chan struct{}),
		readDone:  make(chan struct{}),
	}
	go w.streamPump()
	go w.datagramPump()
	return w
}

// streamPump owns the control stream: it waits for the client to open it,
// then decodes length-prefixed frames until the stream or session dies. It
// is the connection's liveness signal.
func (w *wtConn) streamPump() {
	defer close(w.readDone)

	acceptCtx, cancel := context.WithTimeout(w.ctx, wtStreamAcceptTimeout)
	str, err := w.sess.AcceptStream(acceptCtx)
	cancel()
	if err != nil {
		w.setReadErr(err)
		return
	}
	w.str = str
	close(w.strReady)

	for {
		f, err := readStreamFrame(str, MaxInboundFrameBytes)
		if err != nil {
			w.setReadErr(err)
			return
		}
		select {
		case w.frames <- f:
		case <-w.closed:
			return
		}
	}
}

// datagramPump merges unreliable frames into the inbound queue. Datagram
// errors are not treated as connection death; the control stream decides
// that.
//
// The freeze's reliability classes apply inbound too: a datagram may only
// carry a loss-tolerant opcode (or PING/PONG for RTT probing). Anything
// else is dropped — datagrams are droppable by definition, and must-deliver
// traffic belongs on the stream.
func (w *wtConn) datagramPump() {
	for {
		data, err := w.sess.ReceiveDatagram(w.ctx)
		if err != nil {
			return
		}
		if len(data) > MaxInboundFrameBytes {
			continue
		}
		f, err := DecodeFrame(data)
		if err != nil {
			continue // a malformed datagram is dropped, not fatal
		}
		if !IsLossTolerantOpcode(f.Opcode) && f.Opcode != OpPing && f.Opcode != OpPong {
			log.WithField("opcode", fmt.Sprintf("0x%04X", f.Opcode)).
				Debug("transport: dropped datagram with non-loss-tolerant opcode")
			continue
		}
		select {
		case w.frames <- f:
		case <-w.closed:
			return
		}
	}
}

func (w *wtConn) setReadErr(err error) {
	w.readErrMu.Lock()
	if w.readErr == nil {
		w.readErr = err
	}
	w.readErrMu.Unlock()
}

func (w *wtConn) getReadErr() error {
	w.readErrMu.Lock()
	defer w.readErrMu.Unlock()
	if w.readErr == nil {
		return errors.New("transport: webtransport session closed")
	}
	return w.readErr
}

func (w *wtConn) ReadFrame(ctx context.Context) (Frame, error) {
	select {
	case f := <-w.frames:
		return f, nil
	case <-w.readDone:
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

// wtWriteTimeout bounds one stall in a stream write, the WebSocket
// bound (wsWriteTimeout) applied to WebTransport: WriteFrame had no
// deadline at all, so a session's write loop could park forever on a
// peer that stopped reading - and a resume's WELCOME queues behind
// that loop (#1453). WriteFrame re-arms the deadline as the record
// progresses (writeAllWithinStall), so the bound kills a peer that
// stopped reading, never a large record whose peer is still reading
// (#2311). An expired write errors and follows the normal write-error
// path the WebSocket shape already takes: the write loop detaches, the
// frame keeps its stream position, and a resume re-sends it only if
// the client's position says it never arrived (stream_ring.go).
const wtWriteTimeout = 10 * time.Second

func (w *wtConn) WriteFrame(f Frame) error {
	select {
	case <-w.strReady:
	case <-w.closed:
		return errors.New("transport: webtransport connection closed")
	case <-w.readDone:
		return w.getReadErr()
	}
	buf, err := EncodeStreamFrame(f)
	if err != nil {
		return err
	}
	return writeAllWithinStall(w.str, buf, func() {
		_ = w.str.SetWriteDeadline(time.Now().Add(wtWriteTimeout))
	})
}

func (w *wtConn) SupportsUnreliable() bool { return true }

func (w *wtConn) WriteUnreliable(f Frame) error {
	if f.EncodedLen() > MaxFrameBytes {
		return ErrFrameTooLarge
	}
	return w.sess.SendDatagram(f.Encode())
}

func (w *wtConn) Close(reason string) error {
	var err error
	w.closeOnce.Do(func() {
		close(w.closed)
		w.ctxCancel()
		err = w.sess.CloseWithError(0, reason)
	})
	return err
}

func (w *wtConn) Kind() string { return "webtransport" }

func (w *wtConn) RemoteAddr() string { return w.sess.RemoteAddr().String() }
