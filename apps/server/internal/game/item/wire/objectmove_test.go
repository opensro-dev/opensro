package wire

import (
	"math"
	"reflect"
	"testing"
)

func f32le(value float32) []byte {
	bits := math.Float32bits(value)
	return []byte{byte(bits), byte(bits >> 8), byte(bits >> 16), byte(bits >> 24)}
}

func concat(chunks ...[]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}

func testPosition() Position {
	return Position{RegionID: 0x6B4F, X: 1205, Y: 80, Z: 396, Heading: 0x8000}
}

// sub_775cb0 reads region (2 B @0x00775cc1), then x/y/z/heading as one 14-byte
// block (@0x00775ccf), then the gid (4 B @0x00775cdd). The gid is LAST.
func TestObjectSourceMovePutsGidLast(t *testing.T) {
	got := ObjectSourceMove{Position: testPosition(), Gid: 200007}.Encode()

	want := concat(
		[]byte{0x4F, 0x6B},             // region 0x6B4F
		f32le(1205),                    // x
		f32le(80),                      // y
		f32le(396),                     // z
		[]byte{0x00, 0x80},             // heading 0x8000
		[]byte{0x47, 0x0D, 0x03, 0x00}, // gid 200007, last
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
	if len(got) != ObjectMoveSize {
		t.Fatalf("size = %d, want %d", len(got), ObjectMoveSize)
	}
}

// asm.asm @0x00775b62 (sub_775b50): the correction carries the same six fields
// with the gid FIRST.
func TestObjectSourceCorrectionPutsGidFirst(t *testing.T) {
	got := ObjectSourceCorrection{Gid: 200007, Position: testPosition()}.Encode()

	want := concat(
		[]byte{0x47, 0x0D, 0x03, 0x00}, // gid 200007, first
		[]byte{0x4F, 0x6B},             // region
		f32le(1205),
		f32le(80),
		f32le(396),
		[]byte{0x00, 0x80},
	)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
	if len(got) != ObjectMoveSize {
		t.Fatalf("size = %d, want %d", len(got), ObjectMoveSize)
	}
}

// The two opcodes are the same length, so swapping them is silent on the wire.
// This pins the fact that they are genuinely different byte strings and that
// decoding one as the other yields nonsense rather than a passable result.
func TestObjectMoveAndCorrectionAreNotInterchangeable(t *testing.T) {
	position := testPosition()
	const gid uint32 = 200007

	move := ObjectSourceMove{Position: position, Gid: gid}.Encode()
	correction := ObjectSourceCorrection{Gid: gid, Position: position}.Encode()

	if reflect.DeepEqual(move, correction) {
		t.Fatal("0x30E3 and 0xB2F5 encoded identically; the field-order mirror is gone")
	}

	// Reading a correction as a move puts the gid's low half where the region
	// belongs, so nothing survives the transposition.
	misread, err := DecodeObjectSourceMove(correction)
	if err != nil {
		t.Fatalf("decoding a correction as a move failed outright: %v", err)
	}
	if misread.Gid == gid && misread.RegionID == position.RegionID {
		t.Fatal("a transposed payload decoded cleanly; the layouts are not actually distinct")
	}
	if misread.RegionID != uint16(gid&0xFFFF) {
		t.Fatalf("misread region = 0x%04X, want the gid low half 0x%04X", misread.RegionID, uint16(gid&0xFFFF))
	}
}

func TestObjectMoveRoundTrips(t *testing.T) {
	want := ObjectSourceMove{Position: testPosition(), Gid: 200007}

	got, err := DecodeObjectSourceMove(want.Encode())
	if err != nil {
		t.Fatalf("DecodeObjectSourceMove failed: %v", err)
	}
	if got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}
}

func TestObjectCorrectionRoundTrips(t *testing.T) {
	want := ObjectSourceCorrection{Gid: 200007, Position: testPosition()}

	got, err := DecodeObjectSourceCorrection(want.Encode())
	if err != nil {
		t.Fatalf("DecodeObjectSourceCorrection failed: %v", err)
	}
	if got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}
}

// asm.asm @0x00777b62 (sub_777b60): [u32 gid][u8 stateType][u8 value].
func TestObjectStateRefreshLayout(t *testing.T) {
	cases := []struct {
		name    string
		refresh ObjectStateRefresh
		want    []byte
	}{
		{
			name:    "run/walk channel set to run",
			refresh: ObjectStateRefresh{Gid: 200007, StateType: StateChannelMove, Value: MoveStateRun},
			want:    []byte{0x47, 0x0D, 0x03, 0x00, 1, 3},
		},
		{
			name:    "life channel set to dead",
			refresh: ObjectStateRefresh{Gid: 200007, StateType: StateChannelLife, Value: LifeStateDead},
			want:    []byte{0x47, 0x0D, 0x03, 0x00, 0, 2},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got := testCase.refresh.Encode()
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("payload = % X, want % X", got, testCase.want)
			}
			if len(got) != ObjectStateRefreshSize {
				t.Fatalf("size = %d, want %d", len(got), ObjectStateRefreshSize)
			}

			decoded, err := DecodeObjectStateRefresh(got)
			if err != nil {
				t.Fatalf("DecodeObjectStateRefresh failed: %v", err)
			}
			if decoded != testCase.refresh {
				t.Fatalf("decoded = %+v, want %+v", decoded, testCase.refresh)
			}
		})
	}
}
