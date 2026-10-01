package wire

import (
	"errors"
	"reflect"
	"testing"
)

// sub_7780f0 reads a 4-byte gid @0x007780ff then a 1-byte heading
// @0x0077810d, so the payload is exactly five bytes in that order.
func TestPickupAnimEncodesGidThenHeading(t *testing.T) {
	got := PickupAnim{Gid: 100007, Heading: 0x40}.Encode()

	want := []byte{
		0xA7, 0x86, 0x01, 0x00, // gid 100007
		0x40, // heading
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
	if len(got) != PickupAnimSize {
		t.Fatalf("size = %d, want %d", len(got), PickupAnimSize)
	}
}

func TestPickupAnimRoundTrip(t *testing.T) {
	want := PickupAnim{Gid: 0xDEADBEEF, Heading: 0xFE}

	got, err := DecodePickupAnim(want.Encode())
	if err != nil {
		t.Fatalf("DecodePickupAnim failed: %v", err)
	}
	if got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}
}

func TestDecodePickupAnimRejectsWrongLength(t *testing.T) {
	if _, err := DecodePickupAnim([]byte{1, 2, 3, 4}); !errors.Is(err, ErrShortPayload) {
		t.Fatalf("short payload error = %v, want ErrShortPayload", err)
	}
	if _, err := DecodePickupAnim([]byte{1, 2, 3, 4, 5, 6}); !errors.Is(err, ErrTrailingBytes) {
		t.Fatalf("long payload error = %v, want ErrTrailingBytes", err)
	}
}

// sub_777310 performs a single 4-byte read @0x0077731c and nothing else.
func TestObjectDespawnIsBareGid(t *testing.T) {
	got := ObjectDespawn{Gid: 300001}.Encode()

	want := []byte{0xE1, 0x93, 0x04, 0x00} // 300001
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
	if len(got) != ObjectDespawnSize {
		t.Fatalf("size = %d, want %d", len(got), ObjectDespawnSize)
	}
}

func TestObjectDespawnRoundTrip(t *testing.T) {
	want := ObjectDespawn{Gid: 300042}

	got, err := DecodeObjectDespawn(want.Encode())
	if err != nil {
		t.Fatalf("DecodeObjectDespawn failed: %v", err)
	}
	if got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}
}

func TestDecodeObjectDespawnRejectsTrailingBytes(t *testing.T) {
	if _, err := DecodeObjectDespawn([]byte{1, 2, 3, 4, 5}); !errors.Is(err, ErrTrailingBytes) {
		t.Fatalf("error = %v, want ErrTrailingBytes", err)
	}
}

func TestGoldRefreshIsLittleEndianU64(t *testing.T) {
	got := GoldRefresh{Balance: 8800, Notify: true}.Encode()

	// [u8 type 1][u64 8800][u8 notify]
	want := []byte{1, 0x60, 0x22, 0, 0, 0, 0, 0, 0, 1}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
	if len(got) != GoldRefreshSize {
		t.Fatalf("size = %d, want %d", len(got), GoldRefreshSize)
	}
}

func TestGoldRefreshRoundTrip(t *testing.T) {
	want := GoldRefresh{Balance: 0x0011223344556677, Notify: true}

	got, err := DecodeGoldRefresh(want.Encode())
	if err != nil {
		t.Fatalf("DecodeGoldRefresh failed: %v", err)
	}
	if got != want {
		t.Fatalf("decoded = %+v, want %+v", got, want)
	}
}

// The pickup-burst opcodes and their encoder payloads, pinned numerically.
// This carried the same expectations against the deleted framework packet
// helpers (the retired emulator send path); the values are the wire contract
// and outlive that plumbing.
func TestPickupOpcodesCarryEncoderPayloads(t *testing.T) {
	cases := []struct {
		name    string
		got     uint16
		opcode  uint16
		payload []byte
		want    []byte
	}{
		{
			name:    "0x35C7 pickup anim",
			got:     OpPickupAnim,
			opcode:  0x35C7,
			payload: PickupAnim{Gid: 7, Heading: 3}.Encode(),
			want:    []byte{7, 0, 0, 0, 3},
		},
		{
			name:    "0x36AB despawn",
			got:     OpObjectDespawn,
			opcode:  0x36AB,
			payload: ObjectDespawn{Gid: 300001}.Encode(),
			want:    []byte{0xE1, 0x93, 0x04, 0x00},
		},
		{
			name:    "0x30B3 gold refresh",
			got:     OpPointsUpdate,
			opcode:  0x30B3,
			payload: GoldRefresh{Balance: 1}.Encode(),
			want:    []byte{1, 1, 0, 0, 0, 0, 0, 0, 0, 0},
		},
		{
			name:    "0xB06D error",
			got:     OpItemMoveResponse,
			opcode:  0xB06D,
			payload: EncodeItemMoveError(ErrCodeCannotBePicked),
			want:    []byte{0x02, 0x39},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.got != testCase.opcode {
				t.Fatalf("message id = 0x%04X, want 0x%04X", testCase.got, testCase.opcode)
			}
			if !reflect.DeepEqual(testCase.payload, testCase.want) {
				t.Fatalf("payload = % X, want % X", testCase.payload, testCase.want)
			}
		})
	}
}
