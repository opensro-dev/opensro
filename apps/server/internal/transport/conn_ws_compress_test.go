/*
===========================================================================

conn_ws_compress_test.go - permessage-deflate only where it matters

#2311: on the WebSocket fallback the browser hands the client one
message whole, so the ~930 KB EnterWorld record is one silent atomic
unit on a slow link. WriteFrame now deflates a message at or above
wsCompressMinBytes when the peer negotiated permessage-deflate, and
leaves small frames (and peers that never negotiated) byte-identical
to before. These tests pin both directions at the wire: the RSV1 bit
of each message's first frame says whether that message travelled
compressed, and the payload must decode to exactly Frame.Encode
either way.

===========================================================================
*/
package transport

import (
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

/*
================
wireRecorder

A net.Conn wrapper that copies everything read through it. Wrapped
around the client's side of the socket it sees the exact server ->
client wire bytes, HTTP upgrade response included.
================
*/
type wireRecorder struct {
	net.Conn
	mu  sync.Mutex
	buf bytes.Buffer
}

/*
================
wireRecorder.Read
================
*/
func (r *wireRecorder) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.mu.Lock()
		r.buf.Write(p[:n])
		r.mu.Unlock()
	}
	return n, err
}

/*
================
wireRecorder.bytes
================
*/
func (r *wireRecorder) bytes() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]byte{}, r.buf.Bytes()...)
}

/*
================
wireMessage

One WebSocket message as seen on the wire: whether its first frame
carried RSV1 (permessage-deflate) and how many wire bytes the whole
message took, framing headers included.
================
*/
type wireMessage struct {
	compressed bool
	wireBytes  int
}

/*
================
parseWireMessages

Walks recorded client-side bytes past the HTTP upgrade response and
reports each complete message in order. Server -> client frames are
unmasked; a masked frame or a truncated tail fails the test.
================
*/
func parseWireMessages(t *testing.T, raw []byte) []wireMessage {
	t.Helper()
	head := bytes.Index(raw, []byte("\r\n\r\n"))
	if head < 0 {
		t.Fatal("recorded wire has no HTTP upgrade response")
	}
	raw = raw[head+4:]
	var messages []wireMessage
	for len(raw) > 0 {
		if len(raw) < 2 {
			t.Fatalf("truncated frame header: %d bytes left", len(raw))
		}
		rsv1 := raw[0]&0x40 != 0
		opcode := raw[0] & 0x0f
		if raw[1]&0x80 != 0 {
			t.Fatal("server -> client frame is masked")
		}
		length := uint64(raw[1] & 0x7f)
		pos := 2
		switch length {
		case 126:
			if len(raw) < pos+2 {
				t.Fatal("truncated 16-bit length")
			}
			length = uint64(raw[pos])<<8 | uint64(raw[pos+1])
			pos += 2
		case 127:
			if len(raw) < pos+8 {
				t.Fatal("truncated 64-bit length")
			}
			length = 0
			for _, b := range raw[pos : pos+8] {
				length = length<<8 | uint64(b)
			}
			pos += 8
		}
		if uint64(len(raw)) < uint64(pos)+length {
			t.Fatalf("truncated frame payload: have %d, want %d", len(raw), uint64(pos)+length)
		}
		pos += int(length)
		if opcode != 0 {
			messages = append(messages, wireMessage{compressed: rsv1})
		}
		messages[len(messages)-1].wireBytes += pos
		raw = raw[pos:]
	}
	return messages
}

/*
================
dialRecordedWS

Stands up the production write path (newWSConn over an upgrader that
negotiates compression) and dials it with a recording client.
compressOffer says whether the client offers permessage-deflate.
================
*/
func dialRecordedWS(t *testing.T, compressOffer bool) (*wsConn, *websocket.Conn, *wireRecorder) {
	t.Helper()
	upgrader := websocket.Upgrader{EnableCompression: true}
	serverSide := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(rw, r, nil)
		if err != nil {
			return
		}
		serverSide <- c
	}))
	t.Cleanup(server.Close)
	recorder := &wireRecorder{}
	dialer := websocket.Dialer{
		EnableCompression: compressOffer,
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			conn, err := (&net.Dialer{}).DialContext(ctx, network, addr)
			if err != nil {
				return nil, err
			}
			recorder.Conn = conn
			return recorder, nil
		},
	}
	client, _, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	conn := newWSConn(<-serverSide)
	t.Cleanup(func() { conn.Close("test done") })
	return conn, client, recorder
}

/*
================
exchangeFrame

Writes one frame from the server conn and reads the client's next
message, requiring it to be exactly the frame's encoding.
================
*/
func exchangeFrame(t *testing.T, conn *wsConn, client *websocket.Conn, frame Frame) {
	t.Helper()
	writeDone := make(chan error, 1)
	go func() { writeDone <- conn.WriteFrame(frame) }()
	if err := client.SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatalf("read deadline: %v", err)
	}
	_, data, err := client.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if err := <-writeDone; err != nil {
		t.Fatalf("write: %v", err)
	}
	if !bytes.Equal(data, frame.Encode()) {
		t.Fatal("the message bytes differ from Frame.Encode")
	}
}

/*
================
TestWSCompressesOnlyLargeFrames

With permessage-deflate negotiated, a small frame must travel
uncompressed and a large one deflated, both arriving byte-identical.
================
*/
func TestWSCompressesOnlyLargeFrames(t *testing.T) {
	conn, client, recorder := dialRecordedWS(t, true)

	small := Frame{Opcode: 0x3001, Payload: bytes.Repeat([]byte{0x24}, 120)}
	row := []byte(`{"refObjId":12345,"codename":"ITEM_CH_SWORD_07_A","typeFlags":2216,"maxStack":50},`)
	large := Frame{Opcode: 0x0007, Payload: bytes.Repeat(row, (256<<10)/len(row))}

	exchangeFrame(t, conn, client, small)
	exchangeFrame(t, conn, client, large)

	messages := parseWireMessages(t, recorder.bytes())
	if len(messages) != 2 {
		t.Fatalf("wire carries %d messages, want 2", len(messages))
	}
	if messages[0].compressed {
		t.Fatal("the small frame travelled compressed")
	}
	if !messages[1].compressed {
		t.Fatal("the large frame travelled uncompressed")
	}
	if messages[1].wireBytes >= len(large.Payload)/2 {
		t.Fatalf("the large frame took %d wire bytes for %d payload bytes: deflate did not bite",
			messages[1].wireBytes, len(large.Payload))
	}
}

/*
================
TestWSLargeFrameArrivesWithoutNegotiatedCompression

A peer that never offered permessage-deflate (an old tab, the Go
load tester) negotiates nothing: the per-message toggle is a no-op
and a large frame still arrives byte-identical, uncompressed.
================
*/
func TestWSLargeFrameArrivesWithoutNegotiatedCompression(t *testing.T) {
	conn, client, recorder := dialRecordedWS(t, false)

	row := []byte(`{"refObjId":12345,"codename":"ITEM_CH_SWORD_07_A","typeFlags":2216,"maxStack":50},`)
	large := Frame{Opcode: 0x0007, Payload: bytes.Repeat(row, (256<<10)/len(row))}
	exchangeFrame(t, conn, client, large)

	messages := parseWireMessages(t, recorder.bytes())
	if len(messages) != 1 {
		t.Fatalf("wire carries %d messages, want 1", len(messages))
	}
	if messages[0].compressed {
		t.Fatal("a frame was compressed without negotiation")
	}
}
