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

// wtGoodbyeLinger bounds how long a closing session waits for the
// client to end it after the control stream's FIN (wtConn.Close): the
// WebSocket close frame's bound (wsCloseFrameDeadline), which a BYE and
// its acknowledgement need only a round trip of.
const wtGoodbyeLinger = wsCloseFrameDeadline

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

	// clientAddr and userAgent are the accept request's client facts,
	// set by the listener before AcceptConn (server.go): the real
	// client (clientaddr.go) and its User-Agent. The WebTransport
	// session itself keeps neither once Upgrade returns.
	clientAddr string
	userAgent  string

	strReady chan struct{}
	str      *webtransport.Stream

	readErrMu sync.Mutex
	readErr   error
	readDone  chan struct{}

	// writeMu serializes the control stream's writes with Close's FIN:
	// quic-go forbids closing a stream concurrently with a write to it.
	writeMu sync.Mutex

	// compress encodes the control stream's large records; nil sends
	// every record raw (conn_wt_compress.go). Set by the listener before
	// AcceptConn (server.go), as clientAddr is.
	compress *streamCompressor

	// closing is the server's count of goodbyes still in flight
	// (Server.Shutdown waits on it before closing the QUIC listener);
	// nil when nothing waits, as in the unit tests' bare connections.
	closing *closeGroup

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
func newWTConn(sess *webtransport.Session, closing *closeGroup) *wtConn {
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
		closing:      closing,
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
	buf, err := w.compress.encode(f)
	if err != nil {
		return err
	}
	return w.writeStream(buf)
}

/*
================
wtConn.WriteFrames

Several frames as the same length-prefixed records WriteFrame sends,
concatenated into one stream write (session_write.go). The stream is
one byte sequence either way, so the client's record reader sees the
same records in the same order.
================
*/
func (w *wtConn) WriteFrames(frames []Frame) error {
	select {
	case <-w.strReady:
	case <-w.closed:
		return errWTConnClosed
	case <-w.readDone:
		return w.getReadErr()
	}
	size := 0
	for _, f := range frames {
		size += streamLenPrefixSize + f.EncodedLen()
	}
	buf := make([]byte, 0, size)
	for _, f := range frames {
		record, err := w.compress.encode(f)
		if err != nil {
			// The records ahead of the refused one still go, as one
			// WriteFrame each would have sent them.
			if len(buf) > 0 {
				_ = w.writeStream(buf)
			}
			return err
		}
		buf = append(buf, record...)
	}
	return w.writeStream(buf)
}

/*
================
wtConn.writeStream

The control stream write with the stall deadline re-armed per piece.
================
*/
func (w *wtConn) writeStream(buf []byte) error {
	w.writeMu.Lock()
	defer w.writeMu.Unlock()
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

/*
================
wtConn.Close

Ends the session without losing what the writer already handed to the
control stream: above all the BYE that CloseWhenDrained queues last.
webtransport-go's CloseWithError resets every stream of the session
(v0.13.0 closeWithSession: CancelWrite), and a reset discards whatever
the stream has not delivered yet. So a BYE written a moment before the
close never reached the browser, the session's closed promise rejected,
and every polite end - an Exit or Restart, a reload replacing a tab, a
server restart - filed as a lost connection: 216 of the 477 live
connection_closed incidents on 11-21/22, each with no BYE in the
client's journal (#4115).

The close now finishes the control stream first: Close sends a FIN
after every queued byte, and QUIC retransmits that data until it is
acknowledged. The client closes its session as soon as it reads the
BYE (network.ts end()), which ends the wait at once; a client that
never does is closed after wtGoodbyeLinger. The session close then
resets a stream that has nothing left to lose.

That wait runs on its own goroutine, as the WebSocket close frame does
(conn_ws.go, #2142): callers close sessions from inside a division
lane and must never wait on a peer. A writer parked on a peer that
stopped reading holds writeMu; then the stream cannot be finished in
order (Close must not race a Write) and the session closes at once, as
before - a peer that is not reading would not read a BYE either.
================
*/
func (w *wtConn) Close(reason string) error {
	var err error
	w.closeOnce.Do(func() {
		close(w.closed)
		if w.streamOpen() && w.writeMu.TryLock() {
			w.closing.begin()
			go func() {
				defer w.closing.end()
				defer w.writeMu.Unlock()
				w.finishStream()
				_ = w.endSession(reason)
			}()
			return
		}
		err = w.endSession(reason)
	})
	return err
}

/*
================
wtConn.endSession

Stops the pumps and closes the session with the polite code 0 the
client reads as a goodbye (network.ts WT_CLEAN_CLOSE_CODE).
================
*/
func (w *wtConn) endSession(reason string) error {
	w.ctxCancel()
	return w.sess.CloseWithError(0, reason)
}

/*
================
wtConn.streamOpen

Whether the client's control stream was accepted, so there is a stream
to finish.
================
*/
func (w *wtConn) streamOpen() bool {
	select {
	case <-w.strReady:
		return true
	default:
		return false
	}
}

/*
================
wtConn.finishStream

Sends the control stream's FIN behind everything written to it, then
waits until the client ends the session or wtGoodbyeLinger passes.
The caller holds writeMu.
================
*/
func (w *wtConn) finishStream() {
	_ = w.str.SetWriteDeadline(time.Now().Add(wtGoodbyeLinger))
	if err := w.str.Close(); err != nil {
		return
	}
	linger := time.NewTimer(wtGoodbyeLinger)
	defer linger.Stop()
	select {
	case <-w.sess.Context().Done():
	case <-linger.C:
	}
}

func (w *wtConn) Kind() string { return "webtransport" }

/*
================
closeGroup

The WebTransport goodbyes still in flight (wtConn.Close). A plain
WaitGroup does not fit: a session can still close while Shutdown
waits, and a WaitGroup forbids an Add from zero concurrent with Wait.
A nil group counts nothing.
================
*/
type closeGroup struct {
	mu     sync.Mutex
	active int
	idle   chan struct{}
}

/*
================
closeGroup.begin
================
*/
func (g *closeGroup) begin() {
	if g == nil {
		return
	}
	g.mu.Lock()
	if g.active == 0 {
		g.idle = make(chan struct{})
	}
	g.active++
	g.mu.Unlock()
}

/*
================
closeGroup.end
================
*/
func (g *closeGroup) end() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.active--
	if g.active == 0 {
		close(g.idle)
	}
	g.mu.Unlock()
}

/*
================
closeGroup.wait

Returns once no goodbye is in flight, or when ctx ends.
================
*/
func (g *closeGroup) wait(ctx context.Context) {
	g.mu.Lock()
	if g.active == 0 {
		g.mu.Unlock()
		return
	}
	idle := g.idle
	g.mu.Unlock()
	select {
	case <-idle:
	case <-ctx.Done():
	}
}

/*
================
RemoteAddr

The client this connection serves: the accept request's forwarded
client when the listener recorded one, else the QUIC session's peer.
WebTransport is dialed directly in the public posture, so the peer
already is the client; the forwarded form only matters if an edge
ever fronts it (clientaddr.go).
================
*/
func (w *wtConn) RemoteAddr() string {
	if w.clientAddr != "" {
		return w.clientAddr
	}
	return w.sess.RemoteAddr().String()
}

/*
================
ClientUserAgent

The accept request's User-Agent, "" when none was recorded.
================
*/
func (w *wtConn) ClientUserAgent() string { return w.userAgent }
