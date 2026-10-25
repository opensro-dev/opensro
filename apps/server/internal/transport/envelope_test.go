package transport

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

// TestFrameEncodeGolden pins the exact wire bytes: opcode uint16 LE, then
// payload. 0x706D (item move request) must serialize as 6D 70.
func TestFrameEncodeGolden(t *testing.T) {
	f := Frame{Opcode: 0x706D, Payload: []byte{0xDE, 0xAD, 0xBE}}
	got := f.Encode()
	want := []byte{0x6D, 0x70, 0xDE, 0xAD, 0xBE}
	if !bytes.Equal(got, want) {
		t.Fatalf("Encode() = % X, want % X", got, want)
	}
}

func TestFrameDecodeRoundTrip(t *testing.T) {
	cases := []Frame{
		{Opcode: 0xB06D, Payload: []byte{0x01, 0x02, 0x03}},
		{Opcode: 0x0001, Payload: nil},
		{Opcode: 0xFFFF, Payload: make([]byte, 4096)},
	}
	for _, f := range cases {
		got, err := DecodeFrame(f.Encode())
		if err != nil {
			t.Fatalf("DecodeFrame(op=0x%04X): %v", f.Opcode, err)
		}
		if got.Opcode != f.Opcode {
			t.Fatalf("opcode = 0x%04X, want 0x%04X", got.Opcode, f.Opcode)
		}
		if !bytes.Equal(got.Payload, f.Payload) && len(f.Payload) > 0 {
			t.Fatalf("payload mismatch for op 0x%04X", f.Opcode)
		}
	}
}

func TestDecodeFrameTooShort(t *testing.T) {
	if _, err := DecodeFrame([]byte{0x42}); !errors.Is(err, ErrFrameTooShort) {
		t.Fatalf("err = %v, want ErrFrameTooShort", err)
	}
	if _, err := DecodeFrame(nil); !errors.Is(err, ErrFrameTooShort) {
		t.Fatalf("err = %v, want ErrFrameTooShort", err)
	}
}

// TestStreamFrameGolden pins the WT stream framing: uint32 LE length of
// opcode+payload, then the frame.
func TestStreamFrameGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteStreamFrame(&buf, Frame{Opcode: 0x3126, Payload: []byte{0x0A}}); err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x03, 0x00, 0x00, 0x00, // length 3, uint32 LE
		0x26, 0x31, // opcode 0x3126 LE
		0x0A, // payload
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("stream bytes = % X, want % X", buf.Bytes(), want)
	}
}

func TestStreamFrameRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	frames := []Frame{
		{Opcode: 0x706D, Payload: []byte{1, 2, 3, 4}},
		{Opcode: OpPing, Payload: nil},
		{Opcode: 0x30D7, Payload: make([]byte, 70000)}, // bigger than uint16
	}
	for _, f := range frames {
		if err := WriteStreamFrame(&buf, f); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range frames {
		got, err := ReadStreamFrame(&buf)
		if err != nil {
			t.Fatal(err)
		}
		if got.Opcode != want.Opcode || !bytes.Equal(got.Payload, want.Payload) {
			t.Fatalf("round-trip mismatch on op 0x%04X", want.Opcode)
		}
	}
	if _, err := ReadStreamFrame(&buf); !errors.Is(err, io.EOF) {
		t.Fatalf("err at clean end = %v, want io.EOF", err)
	}
}

func TestReadStreamFrameTruncated(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteStreamFrame(&buf, Frame{Opcode: 0x706D, Payload: []byte{1, 2, 3}}); err != nil {
		t.Fatal(err)
	}
	cut := buf.Bytes()[:buf.Len()-2]
	if _, err := ReadStreamFrame(bytes.NewReader(cut)); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated body err = %v, want io.ErrUnexpectedEOF", err)
	}
	// Truncated inside the length prefix itself.
	if _, err := ReadStreamFrame(bytes.NewReader([]byte{0x03, 0x00})); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("truncated prefix err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestReadStreamFrameLimits(t *testing.T) {
	var oversize [4]byte
	binary.LittleEndian.PutUint32(oversize[:], MaxFrameBytes+1)
	if _, err := ReadStreamFrame(bytes.NewReader(oversize[:])); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("oversize err = %v, want ErrFrameTooLarge", err)
	}
	var tiny [4]byte
	binary.LittleEndian.PutUint32(tiny[:], 1)
	if _, err := ReadStreamFrame(bytes.NewReader(tiny[:])); !errors.Is(err, ErrFrameTooShort) {
		t.Fatalf("undersize err = %v, want ErrFrameTooShort", err)
	}

	var inboundOversize [4]byte
	binary.LittleEndian.PutUint32(inboundOversize[:], MaxInboundFrameBytes+1)
	if _, err := readStreamFrame(bytes.NewReader(inboundOversize[:]), MaxInboundFrameBytes); !errors.Is(err, ErrInboundFrameTooLarge) {
		t.Fatalf("inbound oversize err = %v, want ErrInboundFrameTooLarge", err)
	}
}

func TestHelloRoundTrip(t *testing.T) {
	admissionToken := []byte("STA1.example-admission-token")
	fresh := Hello{AdmissionToken: admissionToken}
	got, err := DecodeHello(EncodeHello(fresh))
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ResumeToken) != 0 || !bytes.Equal(got.AdmissionToken, admissionToken) {
		t.Fatalf("fresh hello round-trip = %+v", got)
	}

	token := bytes.Repeat([]byte{0xAB}, ResumeTokenLen)
	resume := Hello{ResumeToken: token, AdmissionToken: admissionToken}
	got, err = DecodeHello(EncodeHello(resume))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.ResumeToken, token) {
		t.Fatalf("resume token round-trip = % X", got.ResumeToken)
	}
}

func TestHelloWireLayout(t *testing.T) {
	resumeToken := bytes.Repeat([]byte{0xAB}, ResumeTokenLen)
	admissionToken := []byte("STA1.example-admission-token")
	want := Hello{
		ResumeToken:    resumeToken,
		AdmissionToken: admissionToken,
	}
	encoded := EncodeHello(want)
	got, err := DecodeHello(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.ResumeToken, want.ResumeToken) ||
		!bytes.Equal(got.AdmissionToken, want.AdmissionToken) {
		t.Fatalf("HELLO round-trip = %+v", got)
	}
	if encoded[0] != ProtocolVersion ||
		binary.LittleEndian.Uint16(encoded[2+ResumeTokenLen:]) != uint16(len(admissionToken)) {
		t.Fatalf("HELLO layout = % X", encoded)
	}
}

func TestDecodeHelloRejectsMalformedAdmission(t *testing.T) {
	for name, payload := range map[string][]byte{
		"missing length": {ProtocolVersion, 0},
		"empty token":    {ProtocolVersion, 0, 0, 0},
		"short token":    {ProtocolVersion, 0, 3, 0, 'a'},
		"trailing byte":  {ProtocolVersion, 0, 1, 0, 'a', 'b'},
		"too large": {
			ProtocolVersion,
			0,
			byte((MaxAdmissionTokenLen + 1) & 0xff),
			byte((MaxAdmissionTokenLen + 1) >> 8),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeHello(payload); err == nil {
				t.Fatalf("malformed HELLO accepted: % X", payload)
			}
		})
	}
}

func TestDecodeHelloRejectsBadTokenLen(t *testing.T) {
	if _, err := DecodeHello([]byte{ProtocolVersion, 5, 1, 2, 3, 4, 5}); err == nil {
		t.Fatal("expected error for token length 5")
	}
	if _, err := DecodeHello([]byte{ProtocolVersion, ResumeTokenLen, 1, 2}); err == nil {
		t.Fatal("expected error for short token body")
	}
}

func TestWelcomeRoundTrip(t *testing.T) {
	token := bytes.Repeat([]byte{0xCD}, ResumeTokenLen)
	w := Welcome{Version: ProtocolVersion, Resumed: true, SessionID: 0x1122334455667788, ResumeToken: token}
	got, err := DecodeWelcome(EncodeWelcome(w))
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != w.Version || got.Resumed != w.Resumed || got.SessionID != w.SessionID {
		t.Fatalf("welcome round-trip = %+v", got)
	}
	if !bytes.Equal(got.ResumeToken, token) {
		t.Fatalf("welcome token = % X", got.ResumeToken)
	}
}

// TestControlOpcodesStayReserved documents the contract with the native
// plane: every control opcode fits under 0x0100, and the session-internal
// subset stays below the registrable extension range.
func TestControlOpcodesStayReserved(t *testing.T) {
	for _, op := range []uint16{OpHello, OpWelcome, OpPing, OpPong, OpBye} {
		if op > maxReservedOpcode {
			t.Fatalf("session-internal opcode 0x%04X above reserved range", op)
		}
	}
	for _, op := range []uint16{OpHello, OpWelcome, OpPing, OpPong, OpBye, OpEnterWorld, OpEnterWorldResult, OpStructuredSidecar} {
		if op > maxControlOpcode {
			t.Fatalf("control opcode 0x%04X above control range", op)
		}
		if !(Frame{Opcode: op}).IsControl() {
			t.Fatalf("IsControl(0x%04X) = false", op)
		}
	}
	for _, op := range []uint16{OpEnterWorld, OpEnterWorldResult, OpStructuredSidecar} {
		if op <= maxReservedOpcode {
			t.Fatalf("extension opcode 0x%04X would be swallowed by the session", op)
		}
	}
	if (Frame{Opcode: 0x706D}).IsControl() {
		t.Fatal("native opcode 0x706D misclassified as control")
	}
}

// TestEnvelopeGoldenVectors pins the exact envelope byte vectors: PUSH
// 0x36AB gid=300001 on every
// channel, HELLO fresh, HELLO resume, PING empty.
func TestEnvelopeGoldenVectors(t *testing.T) {
	// 0x36AB ObjectDespawn, gid 300001 (0x000493E1), as WS message /
	// WT datagram body.
	despawn := Frame{Opcode: 0x36AB, Payload: []byte{0xE1, 0x93, 0x04, 0x00}}
	wantWS := []byte{0xAB, 0x36, 0xE1, 0x93, 0x04, 0x00}
	if got := despawn.Encode(); !bytes.Equal(got, wantWS) {
		t.Fatalf("WS/datagram PUSH = % X, want % X", got, wantWS)
	}

	// Same frame on a WT stream: u32 LE n=6 prefix.
	var buf bytes.Buffer
	if err := WriteStreamFrame(&buf, despawn); err != nil {
		t.Fatal(err)
	}
	wantStream := []byte{0x06, 0x00, 0x00, 0x00, 0xAB, 0x36, 0xE1, 0x93, 0x04, 0x00}
	if !bytes.Equal(buf.Bytes(), wantStream) {
		t.Fatalf("WT stream PUSH = % X, want % X", buf.Bytes(), wantStream)
	}

	// HELLO fresh (ver=2, tokenLen=0, admissionLen=3) as a WS frame.
	admissionToken := []byte("STA")
	hello := Frame{Opcode: OpHello, Payload: EncodeHello(Hello{AdmissionToken: admissionToken})}
	wantHello := []byte{0x01, 0x00, 0x02, 0x00, 0x03, 0x00, 'S', 'T', 'A'}
	if got := hello.Encode(); !bytes.Equal(got, wantHello) {
		t.Fatalf("HELLO fresh = % X, want % X", got, wantHello)
	}

	// HELLO resume: ver=2, tokenLen=16, token bytes verbatim, then admission.
	token := bytes.Repeat([]byte{0x5A}, ResumeTokenLen)
	resume := Frame{Opcode: OpHello, Payload: EncodeHello(Hello{
		ResumeToken: token, AdmissionToken: admissionToken,
	})}
	wantResume := append([]byte{0x01, 0x00, 0x02, 0x10}, token...)
	wantResume = append(wantResume, 0x03, 0x00, 'S', 'T', 'A')
	if got := resume.Encode(); !bytes.Equal(got, wantResume) {
		t.Fatalf("HELLO resume = % X, want % X", got, wantResume)
	}

	// PING with empty payload.
	ping := Frame{Opcode: OpPing}
	wantPing := []byte{0x03, 0x00}
	if got := ping.Encode(); !bytes.Equal(got, wantPing) {
		t.Fatalf("PING empty = % X, want % X", got, wantPing)
	}
}

// TestEnterWorldGolden pins the 0x0006 bind layout.
func TestEnterWorldGolden(t *testing.T) {
	got := EncodeEnterWorld(EnterWorld{Division: "DIV01", CharName: "CG", AuthToken: []byte{0xAA, 0xBB}})
	want := []byte{
		0x05, 0x00, 'D', 'I', 'V', '0', '1',
		0x02, 0x00, 'C', 'G',
		0x02, 0x00, 0xAA, 0xBB,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("EnterWorld = % X, want % X", got, want)
	}
	back, err := DecodeEnterWorld(got)
	if err != nil {
		t.Fatal(err)
	}
	if back.Division != "DIV01" || back.CharName != "CG" || !bytes.Equal(back.AuthToken, []byte{0xAA, 0xBB}) {
		t.Fatalf("round-trip = %+v", back)
	}
}

// TestEnterWorldAuthTokenRequired pins the single current authenticated wire
// shape and rejects the retired self-asserted identity payload.
func TestEnterWorldAuthTokenRequired(t *testing.T) {
	got := EncodeEnterWorld(EnterWorld{Division: "d1", CharName: "asd2", AuthToken: []byte{0xAA, 0xBB}})
	wantTok := []byte{
		0x02, 0x00, 'd', '1',
		0x04, 0x00, 'a', 's', 'd', '2',
		0x02, 0x00, 0xAA, 0xBB,
	}
	if !bytes.Equal(got, wantTok) {
		t.Fatalf("token encode = % X, want % X", got, wantTok)
	}
	back, err := DecodeEnterWorld(got)
	if err != nil {
		t.Fatal(err)
	}
	if back.Division != "d1" || back.CharName != "asd2" || !bytes.Equal(back.AuthToken, []byte{0xAA, 0xBB}) {
		t.Fatalf("token round-trip = %+v", back)
	}

	bare := []byte{0x02, 0x00, 'd', '1', 0x04, 0x00, 'a', 's', 'd', '2'}
	if _, err := DecodeEnterWorld(bare); err == nil {
		t.Fatal("tokenless EnterWorld payload accepted")
	}

	// Malformed tails are rejected.
	if _, err := DecodeEnterWorld(append(bare, 0x01)); err == nil {
		t.Fatal("1-byte tail accepted")
	}
	if _, err := DecodeEnterWorld(append(append([]byte(nil), bare...), 0x05, 0x00, 0x01)); err == nil {
		t.Fatal("short token body accepted")
	}
	if _, err := DecodeEnterWorld(append(append([]byte(nil), bare...), 0x00, 0x00)); err == nil {
		t.Fatal("empty token accepted")
	}
	huge := append(append([]byte(nil), bare...), 0xFF, 0xFF)
	if _, err := DecodeEnterWorld(huge); err == nil {
		t.Fatal("oversize tokenLen accepted")
	}
}

func TestEnterWorldDecodeRejectsTruncation(t *testing.T) {
	full := EncodeEnterWorld(EnterWorld{Division: "DIV01", CharName: "CG", AuthToken: []byte("token")})
	for cut := 0; cut < len(full); cut++ {
		if _, err := DecodeEnterWorld(full[:cut]); err == nil {
			t.Fatalf("no error at cut %d", cut)
		}
	}
	if _, err := DecodeEnterWorld(append(full, 0x00)); err == nil {
		t.Fatal("no error on trailing byte")
	}
}

// TestEnterWorldResultGolden pins the 0x0007 layout.
func TestEnterWorldResultGolden(t *testing.T) {
	got := EncodeEnterWorldResult(EnterWorldResult{OK: true, Blob: []byte{0xAA, 0xBB}})
	want := []byte{
		0x01,                   // ok
		0x00, 0x00, 0x00, 0x00, // nativeErrorCode
		0x02, 0x00, 0x00, 0x00, // blobLen
		0xAA, 0xBB,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("EnterWorldResult = % X, want % X", got, want)
	}
	back, err := DecodeEnterWorldResult(got)
	if err != nil {
		t.Fatal(err)
	}
	if !back.OK || back.NativeErrorCode != 0 || !bytes.Equal(back.Blob, []byte{0xAA, 0xBB}) {
		t.Fatalf("round-trip = %+v", back)
	}

	fail := EncodeEnterWorldResult(EnterWorldResult{NativeErrorCode: 0x39})
	back, err = DecodeEnterWorldResult(fail)
	if err != nil {
		t.Fatal(err)
	}
	if back.OK || back.NativeErrorCode != 0x39 || back.Blob != nil {
		t.Fatalf("error round-trip = %+v", back)
	}

	oversized := make([]byte, 9)
	binary.LittleEndian.PutUint32(oversized[5:9], ^uint32(0))
	if _, err := DecodeEnterWorldResult(oversized); err == nil {
		t.Fatal("no error on impossible blob length")
	}
}

// TestStructuredSidecarGolden pins the 0x0008 layout.
func TestStructuredSidecarGolden(t *testing.T) {
	got := EncodeStructuredSidecar(StructuredSidecar{Kind: 0x0002, Blob: []byte{0x7B, 0x7D}})
	want := []byte{
		0x02, 0x00, // kind
		0x02, 0x00, 0x00, 0x00, // blobLen
		0x7B, 0x7D,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("StructuredSidecar = % X, want % X", got, want)
	}
	back, err := DecodeStructuredSidecar(got)
	if err != nil {
		t.Fatal(err)
	}
	if back.Kind != 0x0002 || !bytes.Equal(back.Blob, []byte{0x7B, 0x7D}) {
		t.Fatalf("round-trip = %+v", back)
	}
	if _, err := DecodeStructuredSidecar(got[:5]); err == nil {
		t.Fatal("no error on truncated sidecar")
	}

	oversized := make([]byte, 6)
	binary.LittleEndian.PutUint32(oversized[2:6], ^uint32(0))
	if _, err := DecodeStructuredSidecar(oversized); err == nil {
		t.Fatal("no error on impossible blob length")
	}
}

/*
================
TestTransportReportDecoding

The report the client sends after WELCOME (#1449): every defined reason
decodes to its history name, and anything this build cannot read - no
fallback, a newer version, an unknown reason, a wrong size - is not a
fact and must not decode.
================
*/
func TestTransportReportDecoding(t *testing.T) {
	for reason, want := range map[uint8]string{
		FallbackUnsupported: "unsupported",
		FallbackTimeout:     "timeout",
		FallbackRefused:     "refused",
		FallbackClosedEarly: "closed-early",
	} {
		name, ok := DecodeTransportReport(EncodeTransportReport(reason))
		if !ok || name != want {
			t.Fatalf("reason %d decoded as %q/%v, want %q", reason, name, ok, want)
		}
	}
	for name, payload := range map[string][]byte{
		"no fallback":    EncodeTransportReport(FallbackNone),
		"newer version":  {transportReportVersion + 1, FallbackTimeout},
		"unknown reason": {transportReportVersion, 99},
		"empty":          {},
		"short":          {transportReportVersion},
		"long":           {transportReportVersion, FallbackTimeout, 0},
	} {
		if got, ok := DecodeTransportReport(payload); ok {
			t.Fatalf("%s decoded as %q, want not-ok", name, got)
		}
	}
}
