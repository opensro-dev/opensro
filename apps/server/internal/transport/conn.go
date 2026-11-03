package transport

import (
	"context"
	"io"
)

// Conn is one live client connection, either a WebTransport session or a
// WebSocket. Both implementations pump inbound frames on an internal
// goroutine so ReadFrame is a plain blocking call with context cancel.
//
// WriteFrame is not safe for concurrent use; the session's single write
// loop is the only caller. WriteUnreliable IS safe from any goroutine on
// connections that support it (QUIC datagrams).
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
	// when SupportsUnreliable.
	WriteUnreliable(f Frame) error
	// Close tears the connection down. reason is best-effort diagnostics.
	Close(reason string) error
	// Kind is "webtransport" or "websocket", for logs and metrics.
	Kind() string
	// RemoteAddr describes the peer, for logs.
	RemoteAddr() string
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
