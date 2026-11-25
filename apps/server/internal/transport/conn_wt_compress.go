/*
===========================================================================

conn_wt_compress.go - large WebTransport records travel gzip-compressed

The WebSocket has permessage-deflate for its large messages (#2311);
QUIC has no such layer, so the EnterWorld result and the commerce item
references crossed the control stream raw: about 9.5 MB of JSON on every
login and teleport, measured live on 2026-11-25. Port-only, not native,
off unless SRO_WEBTRANSPORT_COMPRESSION is on.

The client asks in the session's URL, so a tab that does not know the
record form is never sent one and nothing in the handshake changes. Only
the stream record is different: frames, the resume ring and the session
above see the same bytes either way. Client frames are never compressed.

===========================================================================
*/
package transport

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"net/http"
	"sync"
)

const (
	// wtCompressQuery and wtCompressGzip are the request a client adds
	// to the WebTransport URL (network.ts WT_COMPRESS_QUERY).
	wtCompressQuery = "compress"
	wtCompressGzip  = "gzip"

	// wtCompressMinBytes is the frame size at which a record is
	// compressed: the WebSocket's own threshold, for its reason. Small
	// per-tick frames stay raw.
	wtCompressMinBytes = wsCompressMinBytes

	// streamCompressedFlag marks a compressed record in its length
	// prefix: the other bits are the length of the gzip stream that
	// follows, which inflates to the encoded frame. No frame reaches
	// MaxFrameBytes (2^24), so the bit is never part of a length.
	streamCompressedFlag uint32 = 1 << 31

	// wtCompressLevel: 6.5 MB of shipped item rows (the live EnterWorld
	// result's size) became 290 KB in 17 ms at this level, 424 KB in
	// 8 ms at BestSpeed (2026-11-25). It runs on the session's writer,
	// outside every game lane, a few times per loading screen.
	wtCompressLevel = gzip.DefaultCompression
)

/*
================
streamCompressor

The server's gzip writers, reused across sessions: one holds over a
megabyte of tables, too much to keep per connection or to allocate per
record.
================
*/
type streamCompressor struct {
	writers sync.Pool
}

/*
================
wantsCompressedRecords

Whether the session's accept request asked for compressed records.
================
*/
func wantsCompressedRecords(r *http.Request) bool {
	return r.URL.Query().Get(wtCompressQuery) == wtCompressGzip
}

/*
================
streamCompressor.encode

One stream record for f: compressed when it is large enough to be worth
it and the result is smaller, else exactly EncodeStreamFrame's. A nil
compressor (the switch is off, or the client did not ask) never
compresses.
================
*/
func (c *streamCompressor) encode(f Frame) ([]byte, error) {
	if c == nil || f.EncodedLen() < wtCompressMinBytes || f.EncodedLen() > MaxFrameBytes {
		return EncodeStreamFrame(f)
	}
	var record bytes.Buffer
	var prefix [streamLenPrefixSize]byte
	record.Write(prefix[:])
	zip, ok := c.writers.Get().(*gzip.Writer)
	if ok {
		zip.Reset(&record)
	} else {
		var err error
		if zip, err = gzip.NewWriterLevel(&record, wtCompressLevel); err != nil {
			return nil, err
		}
	}
	defer c.writers.Put(zip)
	var opcode [2]byte
	binary.LittleEndian.PutUint16(opcode[:], f.Opcode)
	for _, part := range [][]byte{opcode[:], f.Payload} {
		if _, err := zip.Write(part); err != nil {
			return nil, err
		}
	}
	if err := zip.Close(); err != nil {
		return nil, err
	}
	encoded := record.Bytes()
	size := len(encoded) - streamLenPrefixSize
	if size >= f.EncodedLen() {
		return EncodeStreamFrame(f)
	}
	binary.LittleEndian.PutUint32(encoded, uint32(size)|streamCompressedFlag)
	return encoded, nil
}
