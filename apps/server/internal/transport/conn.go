package transport

import (
	"context"
	"errors"
	"io"
)

// errDatagramDropped reports a WriteUnreliable frame dropped because the
// connection's bounded datagram queue was full. It is not a failure: the
// frame is loss-tolerant by contract, and dropping it keeps a stalled
// peer from parking the caller (the monster leg held the division lane
// for 4.7 s that way, #2142). Callers count the drop and move on; they
// must not reroute the frame onto the reliable lane, whose backpressure
// is exactly what the drop avoids.
var errDatagramDropped = errors.New("transport: datagram queue full, frame dropped")

// Conn is one live client connection, either a WebTransport session or a
// WebSocket. Both implementations pump inbound frames on an internal
// goroutine so ReadFrame is a plain blocking call with context cancel.
//
// WriteFrame is not safe for concurrent use; the session's single write
// loop is the only caller. WriteUnreliable IS safe from any goroutine on
// connections that support it (QUIC datagrams) and never blocks.
type Conn interface {
	// ReadFrame blocks until a frame arrives, the context is done, or the
	// connection dies. After an error the connection is unusable.
	ReadFrame(ctx context.Context) (Frame, error)
	// WriteFrame sends one frame on the reliable, ordered channel.
	WriteFrame(f Frame) error
	// SupportsUnreliable reports whether WriteUnreliable actually is
	// unreliable-datagram based on this connection.
	SupportsUnreliable() bool
	// WriteUnreliable sends a frame as a datagram, best effort. Only valid
	// when SupportsUnreliable. It never blocks: a connection whose
	// datagram queue is full drops the frame and reports
	// errDatagramDropped. ErrFrameTooLarge still reports a frame that can
	// never travel as a datagram at all.
	WriteUnreliable(f Frame) error
	// Close tears the connection down. reason is best-effort diagnostics.
	Close(reason string) error
	// Kind is "webtransport" or "websocket", for logs and metrics.
	Kind() string
	// RemoteAddr describes the peer, for logs.
	RemoteAddr() string
}

// datagramSendFailureReporter is the optional Conn extension for a
// datagram lane that counts, after accepting a frame, the sends its
// peer went on to refuse (the WebTransport drain, conn_wt.go).
// Hub.Metrics sums the live connections' counts.
type datagramSendFailureReporter interface {
	DatagramSendFailures() uint64
}

// clientFacter is the optional Conn extension carrying the client
// facts the accept request presented (clientaddr.go): the User-Agent
// header. The session snapshots them at attach for the connection
// census (#2716); connections built in-process report none.
type clientFacter interface {
	ClientUserAgent() string
}

// streamWriteChunk is the piece size writeAllWithinStall offers a
// connection per deadline arming. It sits below the flow-control
// windows a reading peer advertises, so a piece is accepted as fast
// as the peer drains; only a stall waits out the deadline.
const streamWriteChunk = 32 << 10

/*
================
writeAllWithinStall

Writes buf in streamWriteChunk pieces, arming the caller's write
deadline before every piece. The deadline then bounds a stall (no
progress), not the whole record: one 10 s deadline across an entire
record killed large records whose peer was still reading, just
slowly (#2311). A peer that keeps reading finishes a record of any
size; a peer that stops reading fails within one stall bound of the
last byte it accepted, which is the hard limit that keeps a wedged
peer from parking the session's write loop forever (#1453).
================
*/
func writeAllWithinStall(w io.Writer, buf []byte, arm func()) error {
	for len(buf) > 0 {
		arm()
		piece := buf
		if len(piece) > streamWriteChunk {
			piece = piece[:streamWriteChunk]
		}
		n, err := w.Write(piece)
		if n > 0 {
			buf = buf[n:]
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
	}
	return nil
}
