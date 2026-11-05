/*
===========================================================================

conn_wt.go - the WebTransport connection: stream and datagram lanes

A wtConn adapts one WebTransport session to the Conn contract. Inbound,
a stream pump and a datagram pump merge frames into one queue. Outbound,
the session's write loop owns the control stream, and one datagram drain
goroutine owns the QUIC datagram queue, so no caller ever blocks on a
peer that stopped reading (#2142).

===========================================================================
*/
package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/quic-go/webtransport-go"
	log "github.com/sirupsen/logrus"
)

// wtStreamAcceptTimeout bounds how long we wait for the client to open its
// control stream; the session-level HELLO timeout is the outer clock, this
// one only keeps WriteFrame from waiting on a stream that never comes.
const wtStreamAcceptTimeout = 15 * time.Second

// wtDatagramQueueLen bounds the encoded datagrams waiting for the drain
// goroutine to hand them to the QUIC session. The QUIC datagram queue
// itself holds 32 frames and then blocks its writer; this queue sits in
// front of it at eight times that depth, so a peer that keeps reading
// never fills it. Only a stalled peer does, and then WriteUnreliable
// drops the frame (errDatagramDropped) instead of parking its caller.
const wtDatagramQueueLen = 256

// wtMaxDatagramPayload is the largest payload WriteUnreliable accepts
// onto the datagram lane. A QUIC datagram must fit one packet on the
// path: QUIC's path floor is 1200 bytes, and the packet, frame and
// WebTransport headers come out of it, so the payload ceiling sits
// well below that. The lane carries scene position updates of a few
// dozen bytes, so a larger frame never belonged on it. Before the
// queue existed, the QUIC send refused such frames synchronously and
// the caller delivered them reliably; the ceiling keeps that refusal
// synchronous, so an oversized frame still goes reliable instead of
// being counted as sent and then vanishing in the drain.
const wtMaxDatagramPayload = 1024

// errWTConnClosed reports a write on a WebTransport connection that has
// already closed.
var errWTConnClosed = errors.New("transport: webtransport connection closed")

// wtConn adapts a WebTransport session to Conn. The client opens one
// bidirectional stream (the control stream) carrying length-prefixed frames
// — the reliable, ordered channel. Datagrams carry bare frames and merge
// into the same inbound queue.
type wtConn struct {
	sess   *webtransport.Session
	frames chan Frame
	closed chan struct{}

	// dgrams is the bounded outbound datagram queue; datagramDrain is
	// its only reader and the only goroutine that touches the QUIC
	// datagram queue. dgramDone closes when the drain has exited.
	dgrams    chan []byte
	dgramDone chan struct{}

	// sendDatagram is the QUIC session's datagram send, held in a field
	// so a test can stand a stalled or recording peer in for a session.
	sendDatagram func([]byte) error

	// sendFailures counts the sends the drain's peer refused (a frame
	// over the size the path negotiates, a session dying mid-queue):
	// frames accepted by WriteUnreliable that never reached the wire.
	// Hub.Metrics sums them over the live connections.
	sendFailures atomic.Uint64

	ctx       context.Context
	ctxCancel context.CancelFunc

	strReady chan struct{}
	str      *webtransport.Stream

	readErrMu sync.Mutex
	readErr   error
	readDone  chan struct{}

	closeOnce sync.Once
}

/*
================
newWTConn

Wires a WebTransport session into a wtConn and starts its goroutines:
the stream pump and datagram pump inbound, the datagram drain outbound.
All three exit when the connection closes.
================
*/
func newWTConn(sess *webtransport.Session) *wtConn {
	ctx, cancel := context.WithCancel(context.Background())
	w := &wtConn{
		sess:         sess,
		frames:       make(chan Frame, 16),
		closed:       make(chan struct{}),
		dgrams:       make(chan []byte, wtDatagramQueueLen),
		dgramDone:    make(chan struct{}),
		sendDatagram: sess.SendDatagram,
		ctx:          ctx,
		ctxCancel:    cancel,
		strReady:     make(chan struct{}),
		readDone:     make(chan struct{}),
	}
	go w.streamPump()
	go w.datagramPump()
	go w.datagramDrain()
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

/*
================
datagramDrain

The one goroutine that talks to the QUIC datagram queue. That queue
blocks its caller once 32 frames are waiting, so it must only ever be
touched from here: WriteUnreliable enqueues onto w.dgrams without
blocking, and a peer that stopped reading parks this goroutine — never
a game goroutine — until the connection closes. Frames leave in
enqueue order. A refused send (a frame over what the path negotiated,
a dying session) drops the frame and counts the refusal in
sendFailures: datagrams are loss-tolerant by contract, and the
control stream pump, not this lane, decides connection death.
================
*/
func (w *wtConn) datagramDrain() {
	defer close(w.dgramDone)
	for {
		select {
		case buf := <-w.dgrams:
			if err := w.sendDatagram(buf); err != nil {
				w.sendFailures.Add(1)
			}
		case <-w.closed:
			return
		}
	}
}

/*
================
DatagramSendFailures

The drain's refused sends so far (see sendFailures). Hub.Metrics reads
it through the Conn it finds attached, so the loss this lane can still
suffer - a frame under the synchronous ceiling but over what the path
negotiated - is counted, never silent.
================
*/
func (w *wtConn) DatagramSendFailures() uint64 {
	return w.sendFailures.Load()
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
		return errWTConnClosed
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

/*
================
WriteUnreliable

Enqueues the encoded frame for the datagram drain and returns at once.
This used to call the QUIC datagram send directly, which blocks once
the peer stops draining its queue; a monster leg publishing movement
from inside the division lane parked there for 4.7 s (#2142). A full
queue now reports errDatagramDropped and the frame is lost, which the
loss-tolerant lane allows by contract. The size checks stay first and
synchronous: a frame too large for a datagram (past the lane ceiling,
wtMaxDatagramPayload) is a refusal, not a drop, and the caller still
delivers it reliably, exactly as it did when the QUIC send itself
refused the frame.
================
*/
func (w *wtConn) WriteUnreliable(f Frame) error {
	if f.EncodedLen() > MaxFrameBytes || len(f.Payload) > wtMaxDatagramPayload {
		return ErrFrameTooLarge
	}
	select {
	case <-w.closed:
		return errWTConnClosed
	default:
	}
	select {
	case w.dgrams <- f.Encode():
		return nil
	default:
		return errDatagramDropped
	}
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
