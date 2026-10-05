package wire

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"unicode/utf16"
)

// ErrShortPayload is returned when a payload ends before the native reader
// would have finished consuming it.
var ErrShortPayload = errors.New("wire: payload shorter than the layout requires")

// ErrInvalidLength is returned when a caller supplies a negative byte count.
// Rejecting it before slice arithmetic keeps malformed length propagation
// from turning a decode refusal into a process panic.
var ErrInvalidLength = errors.New("wire: byte length cannot be negative")

// ErrTrailingBytes is returned when a payload carries more bytes than the
// layout accounts for. The native readers ignore trailing data, but for a
// server that is a sign of a layout mismatch rather than something to swallow.
var ErrTrailingBytes = errors.New("wire: payload has unconsumed trailing bytes")

// degreesToRadians is the exact constant the client multiplies by when it
// turns a wire heading into a yaw (sub_7780f0 @0x00778150, sub_775cb0
// @0x00775d25). Reproduced bit-for-bit rather than using math.Pi/180 so the
// Go side agrees with the client on the last mantissa bits.
const degreesToRadians = 0.01745329238474369

// Reader consumes a packet payload in the same order and widths as the
// client's CMsgStreamBuffer read primitives, but bound-checked against the
// actual payload length.
type Reader struct {
	payload []byte
	pos     int
}

// NewReader returns a Reader over payload. The payload is not copied.
func NewReader(payload []byte) *Reader {
	return &Reader{payload: payload}
}

// Remaining reports how many bytes are left unread.
func (r *Reader) Remaining() int {
	return len(r.payload) - r.pos
}

func (r *Reader) take(n int) ([]byte, error) {
	if n < 0 {
		return nil, fmt.Errorf("%w: %d", ErrInvalidLength, n)
	}
	if r.Remaining() < n {
		return nil, fmt.Errorf("%w: need %d more byte(s) at offset %d, have %d",
			ErrShortPayload, n, r.pos, r.Remaining())
	}
	chunk := r.payload[r.pos : r.pos+n]
	r.pos += n
	return chunk, nil
}

// U8 reads one byte (CMsgStreamBuffer_ReadU8, sub_4b10f0).
func (r *Reader) U8() (uint8, error) {
	chunk, err := r.take(1)
	if err != nil {
		return 0, err
	}
	return chunk[0], nil
}

// U16 reads a little-endian uint16 (CMsgStreamBuffer_ReadU16, sub_4b10d0).
func (r *Reader) U16() (uint16, error) {
	chunk, err := r.take(2)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint16(chunk), nil
}

// U32 reads a little-endian uint32 (CMsgStreamBuffer_ReadU32, sub_4b1150).
func (r *Reader) U32() (uint32, error) {
	chunk, err := r.take(4)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(chunk), nil
}

// U64 reads a little-endian uint64.
func (r *Reader) U64() (uint64, error) {
	chunk, err := r.take(8)
	if err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(chunk), nil
}

// F32 reads a little-endian IEEE-754 float32.
func (r *Reader) F32() (float32, error) {
	bits, err := r.U32()
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(bits), nil
}

// Bytes reads n raw bytes (CMsgStreamBuffer_Read, sub_4b0eb0).
func (r *Reader) Bytes(n int) ([]byte, error) {
	chunk, err := r.take(n)
	if err != nil {
		return nil, err
	}
	out := make([]byte, n)
	copy(out, chunk)
	return out, nil
}

// Str reads a u16 length and that many single-byte characters
// (CMsgStreamBuffer_ReadAsciiString).
func (r *Reader) Str() (string, error) {
	n, err := r.U16()
	if err != nil {
		return "", err
	}
	raw, err := r.take(int(n))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// WStr reads a u16 character count and that many UTF-16LE units
// (CMsgStreamBuffer_ReadWideString 5E2BA0).
func (r *Reader) WStr() (string, error) {
	n, err := r.U16()
	if err != nil {
		return "", err
	}
	raw, err := r.take(int(n) * 2)
	if err != nil {
		return "", err
	}
	units := make([]uint16, n)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	return string(utf16.Decode(units)), nil
}

// Done reports ErrTrailingBytes when the payload was longer than the layout.
func (r *Reader) Done() error {
	if remaining := r.Remaining(); remaining > 0 {
		return fmt.Errorf("%w: %d byte(s) left at offset %d", ErrTrailingBytes, remaining, r.pos)
	}
	return nil
}

// Writer appends payload bytes in the same order and widths as the client's
// CMsgStreamBuffer append primitives. The zero value is ready to use, and
// every method returns the Writer so calls chain the way the native
// serializers nest theirs.
type Writer struct {
	buf []byte
}

// NewWriter returns a Writer with room reserved for size bytes.
func NewWriter(size int) *Writer {
	return &Writer{buf: make([]byte, 0, size)}
}

// U8 appends one byte (CMsgStreamBuffer_AppendU8, sub_4c4350).
func (w *Writer) U8(value uint8) *Writer {
	w.buf = append(w.buf, value)
	return w
}

// U16 appends a little-endian uint16 (CMsgStreamBuffer_AppendU16, sub_4fcd50).
func (w *Writer) U16(value uint16) *Writer {
	w.buf = append(w.buf, byte(value), byte(value>>8))
	return w
}

// U32 appends a little-endian uint32 (CMsgStreamBuffer_AppendU32, sub_4fcdb0).
func (w *Writer) U32(value uint32) *Writer {
	w.buf = append(w.buf, byte(value), byte(value>>8), byte(value>>16), byte(value>>24))
	return w
}

// U64 appends a little-endian uint64.
func (w *Writer) U64(value uint64) *Writer {
	for i := 0; i < 8; i++ {
		w.buf = append(w.buf, byte(value))
		value >>= 8
	}
	return w
}

// F32 appends a little-endian IEEE-754 float32.
func (w *Writer) F32(value float32) *Writer {
	return w.U32(math.Float32bits(value))
}

// Bytes appends raw bytes (CMsgStreamBuffer_AppendBytes, sub_4c3cf0).
func (w *Writer) Bytes(values []byte) *Writer {
	w.buf = append(w.buf, values...)
	return w
}

// Str appends a u16 length and the string's bytes, the shape Reader.Str
// and the client's ASCII string reader take.
func (w *Writer) Str(value string) *Writer {
	return w.U16(uint16(len(value))).Bytes([]byte(value))
}

// WStr appends a u16 character count and the UTF-16LE units, the shape
// Reader.WStr and the client's wide string reader take.
func (w *Writer) WStr(value string) *Writer {
	units := utf16.Encode([]rune(value))
	w.U16(uint16(len(units)))
	for _, unit := range units {
		w.U16(unit)
	}
	return w
}

// Payload returns the accumulated bytes.
func (w *Writer) Payload() []byte {
	return w.buf
}

// HeadingByteFromAngle converts a full-circle u16 heading (the encoding
// 0x30E3, 0xB2F5 and the spawn blocks carry) into the byte 0x35C7 carries.
//
// The two opcodes scale by different divisors, so this is the only supported
// way to move a heading between them: 0x35C7 divides the byte by 255.0
// (sub_7780f0 @0x00778150) while 0x30E3 divides the word by 65535.0
// (sub_775cb0 @0x00775d25). Round-tripping through this helper keeps the
// resulting yaw equal.
func HeadingByteFromAngle(angle uint16) uint8 {
	return uint8(math.Round(float64(angle)/65535.0*255.0)) & 0xFF
}

// HeadingByteRadians reproduces the yaw 0x35C7's handler derives from its
// heading byte: byte / 255.0 * 360 degrees, in radians.
//
// The divisor is 255.0 and not 256.0, so byte 255 yields a full turn, which is
// the same yaw as 0; the distinct span is 0..254.
func HeadingByteRadians(heading uint8) float64 {
	return float64(heading) / 255.0 * 360.0 * degreesToRadians
}

// HeadingAngleRadians reproduces the yaw 0x30E3's handler derives from its
// heading word: word / 65535.0 * 360 degrees, in radians.
func HeadingAngleRadians(angle uint16) float64 {
	return float64(angle) / 65535.0 * 360.0 * degreesToRadians
}
