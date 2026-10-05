package simulation

import (
	"math"
	"strings"
	"testing"
)

// A pinned live capture: a
// SRO_Client v1.150 ground click near the Europe start.
//
//	01 4F 6B B1 04 50 00 63 01
//	mode=1 region=0x6B4F x=1201 y=80 (height, middle slot) z=355
var capturedMoveRequest = []byte{0x01, 0x4F, 0x6B, 0xB1, 0x04, 0x50, 0x00, 0x63, 0x01}

func TestDecodeClientMovementRequestCapturedBytes(t *testing.T) {
	request, refusal := DecodeClientMovementRequest(capturedMoveRequest)
	if refusal != nil {
		t.Fatalf("captured request refused: %v", refusal)
	}
	if request.Mode != MovementAckDestinationMode {
		t.Errorf("mode = %d, want 1", request.Mode)
	}
	if request.RegionID != 0x6B4F {
		t.Errorf("regionId = 0x%04X, want 0x6B4F", request.RegionID)
	}
	if request.X != 1201 || request.Y != 80 || request.Z != 355 {
		t.Errorf("xyz = (%v, %v, %v), want (1201, 80, 355) - the middle i16 is the HEIGHT", request.X, request.Y, request.Z)
	}
}

func TestDecodeClientMovementRequestNegativeHeight(t *testing.T) {
	// y = -170 (harbor seabed depths are negative): i16 LE 0xFF56.
	payload := []byte{0x01, 0x50, 0x68, 0xBC, 0x02, 0x56, 0xFF, 0x64, 0x00}
	request, refusal := DecodeClientMovementRequest(payload)
	if refusal != nil {
		t.Fatalf("refused: %v", refusal)
	}
	if request.Y != -170 {
		t.Errorf("y = %v, want -170 (i16 sign must survive)", request.Y)
	}
	if request.X != 700 || request.Z != 100 {
		t.Errorf("x/z = %v/%v, want 700/100", request.X, request.Z)
	}
}

func TestDecodeClientMovementRequestAngularForm(t *testing.T) {
	// sub_877f30's turn-in-place form: [u8 0][u8 angularMode=1][u16 word LE].
	request, refusal := DecodeClientMovementRequest([]byte{0x00, 0x01, 0x00, 0x40})
	if refusal != nil {
		t.Fatalf("angular form refused: %v", refusal)
	}
	if request.Mode != MovementAckAngularMode {
		t.Errorf("mode = %d, want %d", request.Mode, MovementAckAngularMode)
	}
	if request.AngularMode != 1 {
		t.Errorf("angularMode = %d, want 1 (the only value sub_877f30 emits)", request.AngularMode)
	}
	if request.HeadingWord != 0x4000 {
		t.Errorf("headingWord = 0x%04X, want 0x4000", request.HeadingWord)
	}
	// The positional fields stay zero - the angular arm never carries xyz
	// (sub_877cc0's angular branch appends nothing else).
	if request.RegionID != 0 || request.X != 0 || request.Y != 0 || request.Z != 0 {
		t.Errorf("positional fields leaked into the angular decode: %+v", request)
	}
}

func TestDecodeClientMovementRequestRefusals(t *testing.T) {
	// The two refusal families stay DISTINCT: a wrong-length frame of a
	// known mode is malformed; a mode byte the serializer cannot emit
	// (sub_877cc0 produces only 0/1) is the unsupported-mode refusal.
	cases := []struct {
		name    string
		payload []byte
		reason  string
	}{
		{"short", capturedMoveRequest[:8], "malformedMovementRequest"},
		{"long", append(append([]byte{}, capturedMoveRequest...), 0x00), "malformedMovementRequest"},
		{"empty", nil, "malformedMovementRequest"},
		{"angularShort", []byte{0x00, 0x01, 0x00}, "malformedMovementRequest"},
		{"angularLong", []byte{0x00, 0x01, 0x00, 0x40, 0x00}, "malformedMovementRequest"},
		{"angularGroundClickShape", []byte{0x00, 0x4F, 0x6B, 0xB1, 0x04, 0x50, 0x00, 0x63, 0x01}, "malformedMovementRequest"},
		{"unknownMode2", []byte{0x02, 0x4F, 0x6B, 0xB1, 0x04, 0x50, 0x00, 0x63, 0x01}, "unsupportedMovementMode"},
		{"unknownModeFF", []byte{0xFF, 0x01, 0x00, 0x40}, "unsupportedMovementMode"},
	}
	for _, tc := range cases {
		_, refusal := DecodeClientMovementRequest(tc.payload)
		if refusal == nil {
			t.Errorf("%s: expected refusal", tc.name)
			continue
		}
		if refusal.NativeErrorCode != 0x02 {
			t.Errorf("%s: nativeErrorCode = 0x%02X, want 0x02", tc.name, refusal.NativeErrorCode)
		}
		if !strings.Contains(refusal.Reason, tc.reason) {
			t.Errorf("%s: reason %q must carry %q", tc.name, refusal.Reason, tc.reason)
		}
	}
}

func TestHeadingWordFromRadiansNativeTruncation(t *testing.T) {
	// The serializer (sub_877cc0 @0x877ceb) truncates toward zero with the
	// x87 RC forced (fnstcw / or 0xc00 / fldcw / fistp). The rad->deg
	// constant @0xc13df8 = 57.295780181884766 sits a hair ABOVE the exact
	// 180/pi, which the boundary expectations below bake in.
	cases := []struct {
		name string
		rad  float64
		want uint16
	}{
		{"zero", 0, 0},
		// pi/2 -> 90.0000010 deg -> 16383.7502 -> trunc 16383.
		{"quarterCircle", math.Pi / 2, 16383},
		// pi -> 180.0000021 deg -> 32767.5004 -> trunc 32767.
		{"halfCircle", math.Pi, 32767},
		// 2*pi -> 360.0000042 deg -> 65535.00077 -> 65535: the full word
		// IS the full circle (the /65535 unit, not /65536).
		{"fullCircle", 2 * math.Pi, 65535},
	}
	for _, tc := range cases {
		if got := HeadingWordFromRadians(tc.rad); got != tc.want {
			t.Errorf("%s: word = %d, want %d", tc.name, got, tc.want)
		}
	}

	// WRAP-AROUND: the serializer does not range-check - the truncated
	// integer stores through a 2-byte append, so an over-rotated yaw wraps
	// through the low 16 bits: 2*pi + pi/2 -> 450.0000053 deg -> 81918.75
	// -> 81918 & 0xffff = 16382 (one unit BELOW the in-range pi/2 word:
	// the f32 constant's excess accumulates with the angle).
	if got := HeadingWordFromRadians(2*math.Pi + math.Pi/2); got != 16382 {
		t.Errorf("wrapped word = %d, want 16382", got)
	}
	// Negative yaw never reaches the serializer live (sub_853550 wraps to
	// [0, 2pi) first), but the codec's two's-complement low-16 store is
	// still pinned: -pi/2 -> -90.0000010 deg -> trunc toward zero -16383
	// -> 0xC001.
	if got := HeadingWordFromRadians(-math.Pi / 2); got != 0xC001 {
		t.Errorf("negative-yaw word = 0x%04X, want 0xC001", got)
	}
}

func TestRadiansFromHeadingWordMirrorsAckParser(t *testing.T) {
	// sub_776170 @0x7761c0: word / 65535.0 * 360.0 * f32(pi/180), narrowed
	// to float32 on the store.
	if got := RadiansFromHeadingWord(0); got != 0 {
		t.Errorf("word 0 -> %v, want 0", got)
	}
	// The full word maps to the full circle (2*pi), within float32.
	if full := RadiansFromHeadingWord(0xFFFF); math.Abs(full-2*math.Pi) > 1e-6 {
		t.Errorf("word 0xFFFF -> %v, want ~2*pi", full)
	}
	// Word 0x4000 divides by 65535 (NOT 65536), so it lands a hair ABOVE
	// pi/2 - the native unit truth.
	quarter := RadiansFromHeadingWord(0x4000)
	if quarter <= math.Pi/2 || math.Abs(quarter-math.Pi/2) > 1e-4 {
		t.Errorf("word 0x4000 -> %v, want just above pi/2", quarter)
	}

	// ROUND TRIP: the native constant pair is not an exact inverse and the
	// parser narrows to float32, so word -> radians -> word survives to
	// within one truncation unit, never overshooting.
	for _, w := range []uint16{0, 1, 0x3FFF, 0x4000, 0x8000, 0xBFFF, 0xFFFE, 0xFFFF} {
		back := HeadingWordFromRadians(RadiansFromHeadingWord(w))
		if diff := int(w) - int(back); diff < 0 || diff > 1 {
			t.Errorf("round trip 0x%04X -> 0x%04X (must be within one unit below, never above)", w, back)
		}
	}
}

func TestDecodeClientMovementRequestClampsThroughReferenceRanges(t *testing.T) {
	// A hostile negative x (i16 0x8000 = -32768) clamps to 0 exactly like
	// the reference clampFiniteNumber(x, 0, 0xffff).
	payload := []byte{0x01, 0x4F, 0x6B, 0x00, 0x80, 0x50, 0x00, 0x63, 0x01}
	request, refusal := DecodeClientMovementRequest(payload)
	if refusal != nil {
		t.Fatalf("refused: %v", refusal)
	}
	if request.X != 0 {
		t.Errorf("x = %v, want clamp to 0", request.X)
	}
}

/*
================
TestDecodeClientMovementRequestKeepsSignedDungeonCoordinates

A dungeon region is a signed 16-bit plane: a click at x=-100, z=-7 must
reach the clip as -100, -7, not the outdoor clamp's 0, 0.
================
*/
func TestDecodeClientMovementRequestKeepsSignedDungeonCoordinates(t *testing.T) {
	// Region 0x8001, x=-100 (0xFF9C), y=5, z=-7 (0xFFF9).
	payload := []byte{0x01, 0x01, 0x80, 0x9C, 0xFF, 0x05, 0x00, 0xF9, 0xFF}
	request, refusal := DecodeClientMovementRequest(payload)
	if refusal != nil {
		t.Fatalf("refused: %v", refusal)
	}
	if request.X != -100 || request.Y != 5 || request.Z != -7 {
		t.Errorf("dungeon destination = (%v, %v, %v), want (-100, 5, -7)", request.X, request.Y, request.Z)
	}
}
