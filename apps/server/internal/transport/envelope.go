package transport

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

// MaxFrameBytes caps opcode+payload for server output. Object-list and
// inventory snapshots can be substantially larger than client requests, so
// the outbound limit remains generous without granting the same allocation
// budget to untrusted input.
// Complete skill presentation/effect references plus the item and entity
// catalogues exceed the former 8 MiB bootstrap budget. Both browser codecs
// mirror this bounded server-output ceiling; client input remains 64 KiB.
const MaxFrameBytes = 16 << 20

// MaxInboundFrameBytes caps every client-to-server frame before dispatch.
// Native requests carry commands and identifiers rather than world
// snapshots; 64 KiB leaves ample protocol headroom while bounding one
// attacker-controlled allocation.
const MaxInboundFrameBytes = 64 << 10

// MaxPingPayloadBytes prevents the transport echo path from becoming a
// bandwidth-amplification primitive. The server's own keepalive uses an
// empty payload; 64 bytes still accommodates timestamps and tracing IDs.
const MaxPingPayloadBytes = 64

// ProtocolVersion is carried in HELLO/WELCOME so the envelope can evolve
// without guessing from byte patterns.
const ProtocolVersion uint8 = 2

// MaxAdmissionTokenLen bounds the HELLO admission ticket before allocation.
const MaxAdmissionTokenLen = 512

// Transport-control opcodes. The native SRO plane (internal/game/item/wire) never uses
// values below 0x0100, so this range is reserved for the transport plane.
//
// 0x0001-0x0005 belong to the session machinery itself and are handled
// inside the transport; game code cannot register them. 0x0006-0x00FF are
// control-extension opcodes: transport-defined layouts whose semantics are
// implemented by the game lanes through the normal Hub.Handle path
// (EnterWorld bind, structured sidecars, future control needs).
const (
	// OpHello is the client's first frame on any new connection:
	// [ver u8][tokenLen u8][token bytes]. tokenLen is 0 (fresh session) or
	// ResumeTokenLen (resume attempt).
	OpHello uint16 = 0x0001
	// OpWelcome is the server's reply:
	// [ver u8][resumed u8][sessionID u64 LE][tokenLen u8][token bytes].
	OpWelcome uint16 = 0x0002
	// OpPing carries an opaque payload the peer echoes back in a Pong.
	OpPing uint16 = 0x0003
	// OpPong echoes a Ping payload.
	OpPong uint16 = 0x0004
	// OpBye announces a clean close: [reason u8]. A session ended by Bye is
	// torn down immediately, with no resume grace.
	OpBye uint16 = 0x0005

	// OpEnterWorld (C->S) binds the session to a character after WELCOME:
	// [u16 LE divLen][division utf8][u16 LE nameLen][charName utf8]
	// [u16 LE tokenLen][launcher-issued token].
	// The handler lives in the bootstrap package; the layout is frozen
	// here so client and server share one contract.
	OpEnterWorld uint16 = 0x0006
	// OpEnterWorldResult (S->C) answers OpEnterWorld:
	// [u8 ok][u32 LE nativeErrorCode][u32 LE blobLen][blob]. The blob is a
	// versioned structured sidecar (bootstrap DTO minus packets); world
	// state then arrives as ordinary native frames in order.
	OpEnterWorldResult uint16 = 0x0007
	// OpStructuredSidecar (S->C) carries structured fields native packets
	// cannot: [u16 LE kind][u32 LE blobLen][blob], pushed in-order right
	// after the triggering request's native packets. Per-session serial
	// dispatch makes FIFO-per-kind correlation sound without request IDs.
	OpStructuredSidecar uint16 = 0x0008
	// Replacement-client movement: [v=1 u8][requestID u32][native 7738 body].
	// Result is UTF-8 JSON v1: request ID, GID, verdict, server timestamp and
	// committed WorldState (goal and timed segment). Native clients never opt in.
	OpPredictedMove       uint16 = 0x0009
	OpPredictedMoveResult uint16 = 0x000A
	// OpStreamAck (C->S) [u32 stream position]: the client has applied the
	// stream's frames up to that position; the server drops them from its
	// resume ring (stream_ring.go, #1466).
	OpStreamAck uint16 = 0x000B
	// OpTransportReport (C->S) [version u8][reason u8]: which transport the
	// session connected with and, when the fastest one failed first, why
	// the client fell back (#1449). Sent once after WELCOME on each attach.
	// Telemetry only: the transport never requires it, and a report it
	// cannot decode is dropped, never fatal.
	OpTransportReport uint16 = 0x000C
)

// Transport fallback reasons for OpTransportReport. The client reports
// why the connection that reached WELCOME was not its first choice;
// FallbackNone means no fallback happened on this connect.
const (
	FallbackNone        uint8 = 0 // connected on the first transport tried
	FallbackUnsupported uint8 = 1 // fastest transport unsupported here
	FallbackTimeout     uint8 = 2 // fastest transport attempt timed out
	FallbackRefused     uint8 = 3 // fastest transport attempt was refused
	FallbackClosedEarly uint8 = 4 // fastest transport closed before WELCOME
)

// transportReportVersion is the only OpTransportReport layout today.
const transportReportVersion uint8 = 1

// maxReservedOpcode is the top of the session-internal range: opcodes the
// transport consumes itself and game code cannot register.
const maxReservedOpcode uint16 = 0x0005

// maxControlOpcode is the top of the whole transport-control range.
const maxControlOpcode uint16 = 0x00FF

// The loss-tolerant native opcodes are the only frames permitted on the
// unreliable lane. Values mirror internal/game/item/wire's opcode table.
const (
	// OpObjectSourceMove repositions a remote entity; superseded every tick.
	OpObjectSourceMove uint16 = 0x30E3
	// OpObjectSourceCorrection hard-corrects a remote entity's position;
	// superseded by any later correction or move.
	OpObjectSourceCorrection uint16 = 0xB2F5
)

// IsLossTolerantOpcode reports whether a frame may travel unreliably.
// Session.SendUnreliable / SendUnreliableKeyed force everything else onto
// the reliable lane, and inbound datagrams carrying other opcodes are
// dropped.
func IsLossTolerantOpcode(op uint16) bool {
	return op == OpObjectSourceMove || op == OpObjectSourceCorrection
}

// Bye reason codes.
const (
	ByeReasonNormal       uint8 = 0
	ByeReasonProtocolErr  uint8 = 1
	ByeReasonHelloTimeout uint8 = 2
	ByeReasonIdleTimeout  uint8 = 3
	ByeReasonSlowConsumer uint8 = 4
	ByeReasonShutdown     uint8 = 5
	ByeReasonReplaced     uint8 = 6
	ByeReasonServerBusy   uint8 = 7
	ByeReasonUnauthorized uint8 = 8
)

// ResumeTokenLen is the size of the opaque resume token in WELCOME/HELLO.
const ResumeTokenLen = 16

// streamLenPrefixSize is the uint32 length prefix used on WT streams.
const streamLenPrefixSize = 4

var (
	ErrFrameTooShort        = errors.New("transport: frame shorter than the 2-byte opcode")
	ErrFrameTooLarge        = fmt.Errorf("transport: frame exceeds MaxFrameBytes (%d)", MaxFrameBytes)
	ErrInboundFrameTooLarge = fmt.Errorf("transport: inbound frame exceeds MaxInboundFrameBytes (%d)", MaxInboundFrameBytes)
)

// Frame is the envelope every channel carries: a native SRO opcode (or a
// reserved transport-control opcode) and its payload.
type Frame struct {
	Scope   []ObjectScopeChange `json:"-"`
	Opcode  uint16
	Payload []byte
	// Current is checked by the ordered writer after dequeue, outside session
	// locks. It may only read authority; it must not mutate or send packets.
	Current func() bool `json:"-"`
}

// IsControl reports whether the frame belongs to the transport itself rather
// than the game plane.
func (f Frame) IsControl() bool {
	return f.Opcode <= maxControlOpcode
}

// EncodedLen is the size of the encoded frame: opcode + payload.
func (f Frame) EncodedLen() int {
	return 2 + len(f.Payload)
}

// Encode renders the frame as it travels in a WS message or WT datagram:
// opcode uint16 LE, then the payload.
func (f Frame) Encode() []byte {
	buf := make([]byte, f.EncodedLen())
	binary.LittleEndian.PutUint16(buf[0:2], f.Opcode)
	copy(buf[2:], f.Payload)
	return buf
}

// DecodeFrame parses a WS message or WT datagram body into a Frame. The
// payload slice aliases b; callers that retain it copy it themselves.
func DecodeFrame(b []byte) (Frame, error) {
	if len(b) < 2 {
		return Frame{}, ErrFrameTooShort
	}
	if len(b) > MaxFrameBytes {
		return Frame{}, ErrFrameTooLarge
	}
	return Frame{
		Opcode:  binary.LittleEndian.Uint16(b[0:2]),
		Payload: b[2:],
	}, nil
}

// EncodeStreamFrame renders one length-prefixed WT stream record:
// uint32 LE length of (opcode+payload), then the frame.
func EncodeStreamFrame(f Frame) ([]byte, error) {
	n := f.EncodedLen()
	if n > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	buf := make([]byte, streamLenPrefixSize+n)
	binary.LittleEndian.PutUint32(buf[0:4], uint32(n))
	binary.LittleEndian.PutUint16(buf[4:6], f.Opcode)
	copy(buf[6:], f.Payload)
	return buf, nil
}

// WriteStreamFrame writes one length-prefixed frame to a WT stream:
// uint32 LE length of (opcode+payload), then the frame.
func WriteStreamFrame(w io.Writer, f Frame) error {
	buf, err := EncodeStreamFrame(f)
	if err != nil {
		return err
	}
	_, err = w.Write(buf)
	return err
}

// ReadStreamFrame reads one length-prefixed frame from a WT stream. It
// returns io.EOF only on a clean boundary (no bytes of the next frame read);
// a frame cut off mid-way surfaces io.ErrUnexpectedEOF.
func ReadStreamFrame(r io.Reader) (Frame, error) {
	return readStreamFrame(r, MaxFrameBytes)
}

// readStreamFrame is the direction-aware stream decoder. Public callers keep
// the general frame limit; live client connections pass MaxInboundFrameBytes.
func readStreamFrame(r io.Reader, maxBytes uint32) (Frame, error) {
	var lenBuf [streamLenPrefixSize]byte
	if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
		if errors.Is(err, io.ErrUnexpectedEOF) {
			return Frame{}, io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	n := binary.LittleEndian.Uint32(lenBuf[:])
	if n < 2 {
		return Frame{}, ErrFrameTooShort
	}
	if n > maxBytes {
		if maxBytes == MaxInboundFrameBytes {
			return Frame{}, ErrInboundFrameTooLarge
		}
		return Frame{}, ErrFrameTooLarge
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(r, body); err != nil {
		if errors.Is(err, io.EOF) {
			return Frame{}, io.ErrUnexpectedEOF
		}
		return Frame{}, err
	}
	return Frame{
		Opcode:  binary.LittleEndian.Uint16(body[0:2]),
		Payload: body[2:],
	}, nil
}

// Hello is the parsed OpHello payload.
type Hello struct {
	ResumeToken    []byte // empty or ResumeTokenLen bytes
	AdmissionToken []byte // required one-use Agent-minted admission ticket
	// HasPosition and LastApplied: the stream position the client last
	// applied, an optional u32 tail sent with a resume token (#1466). A
	// resume without it is refused (stream_ring.go).
	HasPosition bool
	LastApplied uint32
}

// DecodeStreamAck parses an OpStreamAck payload.
func DecodeStreamAck(b []byte) (uint32, bool) {
	if len(b) != 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(b), true
}

/*
================
EncodeTransportReport

Builds the OpTransportReport payload the client sends after WELCOME.
================
*/
func EncodeTransportReport(reason uint8) []byte {
	return []byte{transportReportVersion, reason}
}

/*
================
DecodeTransportReport

Parses an OpTransportReport payload into the fallback's history name.
FallbackNone (and any report this build cannot read: wrong size, newer
version, unknown reason) reports ok=false, because telemetry from a
client we do not understand must not be recorded as a fact (#1449).
================
*/
func DecodeTransportReport(b []byte) (name string, ok bool) {
	if len(b) != 2 || b[0] != transportReportVersion {
		return "", false
	}
	switch b[1] {
	case FallbackUnsupported:
		return "unsupported", true
	case FallbackTimeout:
		return "timeout", true
	case FallbackRefused:
		return "refused", true
	case FallbackClosedEarly:
		return "closed-early", true
	}
	return "", false
}

// EncodeHello builds the OpHello payload.
func EncodeHello(h Hello) []byte {
	size := 4 + len(h.ResumeToken) + len(h.AdmissionToken)
	if h.HasPosition {
		size += 4
	}
	buf := make([]byte, size)
	buf[0] = ProtocolVersion
	buf[1] = uint8(len(h.ResumeToken))
	copy(buf[2:], h.ResumeToken)
	offset := 2 + len(h.ResumeToken)
	binary.LittleEndian.PutUint16(buf[offset:offset+2], uint16(len(h.AdmissionToken)))
	copy(buf[offset+2:], h.AdmissionToken)
	if h.HasPosition {
		binary.LittleEndian.PutUint32(buf[size-4:], h.LastApplied)
	}
	return buf
}

// DecodeHello parses an OpHello payload.
func DecodeHello(b []byte) (Hello, error) {
	if len(b) < 2 {
		return Hello{}, fmt.Errorf("transport: HELLO payload %d bytes, need at least 2", len(b))
	}
	if b[0] != ProtocolVersion {
		return Hello{}, fmt.Errorf("transport: protocol version %d not supported", b[0])
	}
	tokenLen := int(b[1])
	if tokenLen != 0 && tokenLen != ResumeTokenLen {
		return Hello{}, fmt.Errorf("transport: HELLO token length %d, want 0 or %d", tokenLen, ResumeTokenLen)
	}
	prefixBytes := 2 + tokenLen
	if len(b) < prefixBytes {
		return Hello{}, fmt.Errorf("transport: HELLO payload %d bytes, need at least %d", len(b), prefixBytes)
	}
	h := Hello{}
	if tokenLen > 0 {
		h.ResumeToken = append([]byte(nil), b[2:2+tokenLen]...)
	}
	if len(b) < prefixBytes+2 {
		return Hello{}, fmt.Errorf("transport: HELLO has no admission token length")
	}
	admissionLen := int(binary.LittleEndian.Uint16(b[prefixBytes : prefixBytes+2]))
	if admissionLen == 0 || admissionLen > MaxAdmissionTokenLen {
		return Hello{}, fmt.Errorf("transport: HELLO admission token length %d, want 1..%d", admissionLen, MaxAdmissionTokenLen)
	}
	end := prefixBytes + 2 + admissionLen
	switch len(b) {
	case end:
	case end + 4:
		h.HasPosition = true
		h.LastApplied = binary.LittleEndian.Uint32(b[end:])
	default:
		return Hello{}, fmt.Errorf("transport: HELLO payload %d bytes, want %d or %d", len(b), end, end+4)
	}
	h.AdmissionToken = append([]byte(nil), b[prefixBytes+2:end]...)
	return h, nil
}

// Welcome is the parsed OpWelcome payload.
type Welcome struct {
	Version     uint8
	Resumed     bool
	SessionID   uint64
	ResumeToken []byte
}

// EncodeWelcome builds the OpWelcome payload.
func EncodeWelcome(w Welcome) []byte {
	buf := make([]byte, 2+8+1+len(w.ResumeToken))
	buf[0] = w.Version
	if w.Resumed {
		buf[1] = 1
	}
	binary.LittleEndian.PutUint64(buf[2:10], w.SessionID)
	buf[10] = uint8(len(w.ResumeToken))
	copy(buf[11:], w.ResumeToken)
	return buf
}

// DecodeWelcome parses an OpWelcome payload.
func DecodeWelcome(b []byte) (Welcome, error) {
	if len(b) < 11 {
		return Welcome{}, fmt.Errorf("transport: WELCOME payload %d bytes, need at least 11", len(b))
	}
	tokenLen := int(b[10])
	if len(b) != 11+tokenLen {
		return Welcome{}, fmt.Errorf("transport: WELCOME payload %d bytes, want %d", len(b), 11+tokenLen)
	}
	w := Welcome{
		Version:   b[0],
		Resumed:   b[1] == 1,
		SessionID: binary.LittleEndian.Uint64(b[2:10]),
	}
	if tokenLen > 0 {
		w.ResumeToken = append([]byte(nil), b[11:11+tokenLen]...)
	}
	return w, nil
}

// MaxAuthTokenLen bounds the required EnterWorld auth token.
const MaxAuthTokenLen = 512

// EnterWorld is the parsed OpEnterWorld payload: the post-WELCOME identity
// bind. The bootstrap lane owns what happens with it.
//
// AuthToken is the required launcher-issued bind token tail. Verification
// happens in the transport's EnterWorldAuth gate before the game handler runs.
type EnterWorld struct {
	Division  string
	CharName  string
	AuthToken []byte
}

// EncodeEnterWorld builds the OpEnterWorld payload:
// [u16 LE divLen][division][u16 LE nameLen][charName]
// [u16 LE tokenLen][token]. An empty token deliberately produces an invalid
// payload; callers must obtain a launcher-issued token before encoding.
func EncodeEnterWorld(e EnterWorld) []byte {
	div, name := []byte(e.Division), []byte(e.CharName)
	size := 2 + len(div) + 2 + len(name) + 2 + len(e.AuthToken)
	buf := make([]byte, size)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(len(div)))
	copy(buf[2:], div)
	off := 2 + len(div)
	binary.LittleEndian.PutUint16(buf[off:off+2], uint16(len(name)))
	copy(buf[off+2:], name)
	off += 2 + len(name)
	binary.LittleEndian.PutUint16(buf[off:off+2], uint16(len(e.AuthToken)))
	copy(buf[off+2:], e.AuthToken)
	return buf
}

// DecodeEnterWorld parses an authenticated OpEnterWorld payload.
func DecodeEnterWorld(b []byte) (EnterWorld, error) {
	if len(b) < 2 {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD payload %d bytes, need at least 2", len(b))
	}
	divLen := int(binary.LittleEndian.Uint16(b[0:2]))
	if len(b) < 2+divLen+2 {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD truncated in division (divLen=%d, have %d)", divLen, len(b))
	}
	off := 2 + divLen
	nameLen := int(binary.LittleEndian.Uint16(b[off : off+2]))
	if len(b) < off+2+nameLen {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD truncated in name (nameLen=%d, have %d)", nameLen, len(b))
	}
	ew := EnterWorld{
		Division: string(b[2 : 2+divLen]),
		CharName: string(b[off+2 : off+2+nameLen]),
	}
	rest := b[off+2+nameLen:]
	if len(rest) == 0 {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD auth token is required")
	}
	if len(rest) < 2 {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD token tail %d bytes, need at least 2", len(rest))
	}
	tokenLen := int(binary.LittleEndian.Uint16(rest[0:2]))
	if tokenLen == 0 {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD auth token is empty")
	}
	if tokenLen > MaxAuthTokenLen {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD token %d bytes exceeds max %d", tokenLen, MaxAuthTokenLen)
	}
	if len(rest) != 2+tokenLen {
		return EnterWorld{}, fmt.Errorf("transport: ENTERWORLD token tail %d bytes, want %d", len(rest), 2+tokenLen)
	}
	ew.AuthToken = append([]byte(nil), rest[2:2+tokenLen]...)
	return ew, nil
}

// EnterWorldResult is the parsed OpEnterWorldResult payload.
type EnterWorldResult struct {
	OK bool
	// NativeErrorCode carries the SRO-native error when OK is false.
	NativeErrorCode uint32
	// Blob is the versioned structured sidecar (bootstrap DTO minus
	// packets); opaque to the transport.
	Blob []byte
}

// EncodeEnterWorldResult builds the OpEnterWorldResult payload:
// [u8 ok][u32 LE nativeErrorCode][u32 LE blobLen][blob].
func EncodeEnterWorldResult(r EnterWorldResult) []byte {
	buf := make([]byte, 1+4+4+len(r.Blob))
	if r.OK {
		buf[0] = 1
	}
	binary.LittleEndian.PutUint32(buf[1:5], r.NativeErrorCode)
	binary.LittleEndian.PutUint32(buf[5:9], uint32(len(r.Blob)))
	copy(buf[9:], r.Blob)
	return buf
}

// DecodeEnterWorldResult parses an OpEnterWorldResult payload.
func DecodeEnterWorldResult(b []byte) (EnterWorldResult, error) {
	if len(b) < 9 {
		return EnterWorldResult{}, fmt.Errorf("transport: ENTERWORLDRESULT payload %d bytes, need at least 9", len(b))
	}
	rawBlobLen := binary.LittleEndian.Uint32(b[5:9])
	if uint64(rawBlobLen) != uint64(len(b)-9) {
		return EnterWorldResult{}, fmt.Errorf("transport: ENTERWORLDRESULT payload %d bytes does not match blob length %d", len(b), rawBlobLen)
	}
	blobLen := int(rawBlobLen)
	r := EnterWorldResult{
		OK:              b[0] == 1,
		NativeErrorCode: binary.LittleEndian.Uint32(b[1:5]),
	}
	if blobLen > 0 {
		r.Blob = append([]byte(nil), b[9:9+blobLen]...)
	}
	return r, nil
}

// StructuredSidecar is the parsed OpStructuredSidecar payload.
type StructuredSidecar struct {
	// Kind discriminates the sidecar family (move minimap, pickup ETA,
	// chat permission error, ...); values are owned by the game lanes.
	Kind uint16
	Blob []byte
}

// EncodeStructuredSidecar builds the OpStructuredSidecar payload:
// [u16 LE kind][u32 LE blobLen][blob].
func EncodeStructuredSidecar(s StructuredSidecar) []byte {
	buf := make([]byte, 2+4+len(s.Blob))
	binary.LittleEndian.PutUint16(buf[0:2], s.Kind)
	binary.LittleEndian.PutUint32(buf[2:6], uint32(len(s.Blob)))
	copy(buf[6:], s.Blob)
	return buf
}

// DecodeStructuredSidecar parses an OpStructuredSidecar payload.
func DecodeStructuredSidecar(b []byte) (StructuredSidecar, error) {
	if len(b) < 6 {
		return StructuredSidecar{}, fmt.Errorf("transport: SIDECAR payload %d bytes, need at least 6", len(b))
	}
	rawBlobLen := binary.LittleEndian.Uint32(b[2:6])
	if uint64(rawBlobLen) != uint64(len(b)-6) {
		return StructuredSidecar{}, fmt.Errorf("transport: SIDECAR payload %d bytes does not match blob length %d", len(b), rawBlobLen)
	}
	blobLen := int(rawBlobLen)
	s := StructuredSidecar{Kind: binary.LittleEndian.Uint16(b[0:2])}
	if blobLen > 0 {
		s.Blob = append([]byte(nil), b[6:6+blobLen]...)
	}
	return s, nil
}
