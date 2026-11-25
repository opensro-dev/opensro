/*
===========================================================================

conn_wt_compress_test.go - large WebTransport records, compressed on request

Over real loopback sockets: a large frame travels compressed only when the
switch is on and the session's URL asked, it inflates to exactly the bytes
the raw record carries, and the frames around it keep their order. Every
other pairing of switch and request is byte-identical to before.

===========================================================================
*/
package transport_test

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/quic-go/webtransport-go"
	"opensro.online/server/internal/transport"
)

const (
	// compressBurstOp asks the test lane for the burst below.
	compressBurstOp uint16 = 0x7070
	// compressedRecordFlag is the length prefix's mark, spelled out: the
	// client depends on this bit.
	compressedRecordFlag uint32 = 1 << 31
	compressQuery               = "?compress=gzip"
)

/*
================
wireRecord

One server record as it crossed the stream.
================
*/
type wireRecord struct {
	compressed bool
	wireBytes  int
	frame      transport.Frame
}

/*
================
readWireRecord

Reads one record in either form, skipping keepalives.
================
*/
func readWireRecord(t *testing.T, str *webtransport.Stream) wireRecord {
	t.Helper()
	_ = str.SetReadDeadline(time.Now().Add(smokeIOTimeout))
	for {
		var prefix [4]byte
		if _, err := io.ReadFull(str, prefix[:]); err != nil {
			t.Fatalf("record prefix: %v", err)
		}
		length := binary.LittleEndian.Uint32(prefix[:])
		record := wireRecord{compressed: length&compressedRecordFlag != 0}
		body := make([]byte, length&^compressedRecordFlag)
		if _, err := io.ReadFull(str, body); err != nil {
			t.Fatalf("record body: %v", err)
		}
		record.wireBytes = len(body)
		if record.compressed {
			zip, err := gzip.NewReader(bytes.NewReader(body))
			if err != nil {
				t.Fatal(err)
			}
			if body, err = io.ReadAll(zip); err != nil {
				t.Fatal(err)
			}
		}
		frame, err := transport.DecodeFrame(body)
		if err != nil {
			t.Fatal(err)
		}
		if frame.Opcode == transport.OpPing || frame.Opcode == transport.OpPong {
			continue
		}
		record.frame = frame
		return record
	}
}

/*
================
compressBurst

The burst and whether each frame may shrink. The first three fit one
batched write (WriteFrames): a small frame, a repetitive one under the
threshold and a large repetitive one. Then, each past a batch's size
(WriteFrame), a large repetitive one, one gzip cannot shrink, and a
repetitive one again on a reused writer.
================
*/
func compressBurst() ([]transport.Frame, []bool) {
	text := bytes.Repeat([]byte(`{"codename":"ITEM_CH_SWORD_01_A","price":120},`), 4000)
	noise := make([]byte, 64<<10)
	rand.New(rand.NewSource(1)).Read(noise)
	return []transport.Frame{
		{Opcode: 0xB070, Payload: []byte{1}},
		{Opcode: 0xB071, Payload: text[:20000]},
		{Opcode: 0xB072, Payload: text[:40000]},
		{Opcode: 0xB073, Payload: text},
		{Opcode: 0xB074, Payload: noise},
		{Opcode: 0xB075, Payload: append([]byte("again"), text...)},
	}, []bool{false, false, true, true, false, true}
}

/*
================
TestWebTransportCompressesLargeRecordsOnRequest
================
*/
func TestWebTransportCompressesLargeRecordsOnRequest(t *testing.T) {
	for _, row := range []struct {
		name       string
		on         bool
		query      string
		compressed bool
	}{
		{"on and asked", true, compressQuery, true},
		{"on, not asked", true, "", false},
		{"on, another encoding asked", true, "?compress=br", false},
		{"off, asked", false, compressQuery, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			srv := startServerWith(t, func(cfg *transport.Config) { cfg.WebTransportCompression = row.on })
			burst, shrinks := compressBurst()
			srv.Hub.Handle(compressBurstOp, func(s *transport.Session, _ uint16, _ []byte) {
				if err := s.SendBatch(burst); err != nil {
					t.Error(err)
				}
			})
			_, str := dialWTQuery(t, srv, row.query)
			helloWT(t, str, nil)
			if err := transport.WriteStreamFrame(str, transport.Frame{Opcode: compressBurstOp}); err != nil {
				t.Fatal(err)
			}
			for i, want := range burst {
				got := readWireRecord(t, str)
				if got.frame.Opcode != want.Opcode || !bytes.Equal(got.frame.Payload, want.Payload) {
					t.Fatalf("record %d: opcode 0x%04X with %d bytes, want 0x%04X with %d",
						i, got.frame.Opcode, len(got.frame.Payload), want.Opcode, len(want.Payload))
				}
				if got.compressed != (row.compressed && shrinks[i]) {
					t.Fatalf("record %d (0x%04X): compressed %v", i, want.Opcode, got.compressed)
				}
				if got.compressed && got.wireBytes*8 > want.EncodedLen() {
					t.Fatalf("record %d: %d wire bytes for %d", i, got.wireBytes, want.EncodedLen())
				}
				if !got.compressed && got.wireBytes != want.EncodedLen() {
					t.Fatalf("record %d: raw record of %d bytes, want %d", i, got.wireBytes, want.EncodedLen())
				}
			}
		})
	}
}
