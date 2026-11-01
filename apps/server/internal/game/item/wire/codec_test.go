package wire

import (
	"errors"
	"math"
	"reflect"
	"testing"
)

func TestWriterEmitsLittleEndian(t *testing.T) {
	got := NewWriter(0).
		U8(0x11).
		U16(0x2233).
		U32(0x44556677).
		U64(0x8899AABBCCDDEEFF).
		F32(1.0).
		Bytes([]byte{0xAB, 0xCD}).
		Payload()

	want := []byte{
		0x11,
		0x33, 0x22,
		0x77, 0x66, 0x55, 0x44,
		0xFF, 0xEE, 0xDD, 0xCC, 0xBB, 0xAA, 0x99, 0x88,
		0x00, 0x00, 0x80, 0x3F,
		0xAB, 0xCD,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestReaderRoundTripsEveryWidth(t *testing.T) {
	payload := NewWriter(0).
		U8(0x11).
		U16(0x2233).
		U32(0x44556677).
		U64(0x8899AABBCCDDEEFF).
		F32(-2.5).
		Bytes([]byte{0xAB, 0xCD}).
		Payload()

	r := NewReader(payload)

	if got, err := r.U8(); err != nil || got != 0x11 {
		t.Fatalf("U8 = 0x%02X, %v; want 0x11, nil", got, err)
	}
	if got, err := r.U16(); err != nil || got != 0x2233 {
		t.Fatalf("U16 = 0x%04X, %v; want 0x2233, nil", got, err)
	}
	if got, err := r.U32(); err != nil || got != 0x44556677 {
		t.Fatalf("U32 = 0x%08X, %v; want 0x44556677, nil", got, err)
	}
	if got, err := r.U64(); err != nil || got != 0x8899AABBCCDDEEFF {
		t.Fatalf("U64 = 0x%016X, %v; want 0x8899AABBCCDDEEFF, nil", got, err)
	}
	if got, err := r.F32(); err != nil || got != -2.5 {
		t.Fatalf("F32 = %v, %v; want -2.5, nil", got, err)
	}
	if got, err := r.Bytes(2); err != nil || !reflect.DeepEqual(got, []byte{0xAB, 0xCD}) {
		t.Fatalf("Bytes = % X, %v; want AB CD, nil", got, err)
	}
	if err := r.Done(); err != nil {
		t.Fatalf("Done = %v, want nil", err)
	}
}

func TestReaderRejectsShortPayloadForEveryWidth(t *testing.T) {
	cases := []struct {
		name string
		read func(*Reader) error
		have int
	}{
		{"U8", func(r *Reader) error { _, err := r.U8(); return err }, 0},
		{"U16", func(r *Reader) error { _, err := r.U16(); return err }, 1},
		{"U32", func(r *Reader) error { _, err := r.U32(); return err }, 3},
		{"U64", func(r *Reader) error { _, err := r.U64(); return err }, 7},
		{"F32", func(r *Reader) error { _, err := r.F32(); return err }, 3},
		{"Bytes", func(r *Reader) error { _, err := r.Bytes(4); return err }, 3},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			r := NewReader(make([]byte, testCase.have))
			err := testCase.read(r)
			if !errors.Is(err, ErrShortPayload) {
				t.Fatalf("error = %v, want ErrShortPayload", err)
			}
		})
	}
}

func TestReaderDoneReportsTrailingBytes(t *testing.T) {
	r := NewReader([]byte{1, 2, 3})
	if _, err := r.U8(); err != nil {
		t.Fatalf("U8 failed: %v", err)
	}

	err := r.Done()
	if !errors.Is(err, ErrTrailingBytes) {
		t.Fatalf("error = %v, want ErrTrailingBytes", err)
	}
}

func TestReaderRejectsNegativeByteLength(t *testing.T) {
	r := NewReader([]byte{1, 2, 3})
	if _, err := r.Bytes(-1); !errors.Is(err, ErrInvalidLength) {
		t.Fatalf("Bytes(-1) error = %v, want ErrInvalidLength", err)
	}
	if got := r.Remaining(); got != 3 {
		t.Fatalf("negative read consumed input: %d bytes remain, want 3", got)
	}
}

// The two heading encodings on this wave differ, and mixing them is the
// easiest way to put an entity at the wrong yaw. 0x35C7 divides a byte by
// 255.0 (client CPSMission_OnPickupAnim_0x35C7 at 0x00778150); 0x30E3
// divides a word by 65535.0
// (client CPSMission_OnEntitySpeedMode0x30E3 at 0x00775d25).
func TestHeadingDivisorsDifferBetweenByteAndWordEncodings(t *testing.T) {
	// A half turn under each encoding.
	if got, want := HeadingByteRadians(128), math.Pi; math.Abs(got-want) > 0.02 {
		t.Fatalf("byte 128 = %v rad, want about %v", got, want)
	}
	if got, want := HeadingAngleRadians(0x8000), math.Pi; math.Abs(got-want) > 0.001 {
		t.Fatalf("word 0x8000 = %v rad, want about %v", got, want)
	}

	// The full-turn endpoints: the byte divisor is 255, not 256, so byte 255
	// is a whole turn rather than one step short of it.
	if got, want := HeadingByteRadians(255), 2*math.Pi; math.Abs(got-want) > 1e-6 {
		t.Fatalf("byte 255 = %v rad, want a full turn %v", got, want)
	}
	if got, want := HeadingAngleRadians(65535), 2*math.Pi; math.Abs(got-want) > 1e-6 {
		t.Fatalf("word 65535 = %v rad, want a full turn %v", got, want)
	}
}

func TestHeadingByteFromAnglePreservesYaw(t *testing.T) {
	cases := []struct {
		angle uint16
		want  uint8
	}{
		{0, 0},
		{0x4000, 64},
		{0x8000, 128},
		{0xC000, 191},
		{0xFFFF, 255},
	}

	// Half a byte step is the most a nearest-rounding conversion can move the
	// yaw. Compared as circle fractions rather than radians so the native
	// degrees-to-radians constant does not enter the tolerance.
	const tolerance = 0.5/255.0 + 1e-9

	for _, testCase := range cases {
		got := HeadingByteFromAngle(testCase.angle)
		if got != testCase.want {
			t.Fatalf("HeadingByteFromAngle(0x%04X) = %d, want %d", testCase.angle, got, testCase.want)
		}
		wordFraction := float64(testCase.angle) / 65535.0
		byteFraction := float64(got) / 255.0
		if delta := math.Abs(wordFraction - byteFraction); delta > tolerance {
			t.Fatalf("angle 0x%04X drifted %v of a turn, want within half a byte step (%v)",
				testCase.angle, delta, tolerance)
		}
	}
}
