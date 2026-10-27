package quest

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// The byte pins below mirror the client grammar the re-harness
// questInfoWireParity suite pins from the other side
// (SQuestInfo_Deserialize / CPSMission_OnNetProcessInLog0x31ED): every
// emitted stream must deserialize on the REAL folds
// byte-for-byte. The enter-world 0x32B3 section-2 emission
// (bootstrap/wire.go) follows the same flag-driven rules; the corpus
// suite pins that half.

func u32le(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

func TestDecodeQuestRefRequest(t *testing.T) {
	t.Parallel()
	refID, err := DecodeQuestRefRequest(u32le(0x2001))
	if err != nil || refID != 0x2001 {
		t.Fatalf("decode = %d, %v; want 0x2001", refID, err)
	}
	if _, err := DecodeQuestRefRequest([]byte{1, 2, 3}); err == nil {
		t.Fatal("a short body must refuse")
	}
	if _, err := DecodeQuestRefRequest([]byte{1, 2, 3, 4, 5}); err == nil {
		t.Fatal("trailing bytes must refuse (strict decode)")
	}
}

// TestQuestUpdateRemoveLegs pins the op-3/op-4 payloads: [op][refId u32],
// nothing further (the client's remove leg reads nothing past the header).
func TestQuestUpdateRemoveLegs(t *testing.T) {
	t.Parallel()
	if got, want := EncodeQuestUpdateComplete(0x2001), append([]byte{3}, u32le(0x2001)...); !bytes.Equal(got, want) {
		t.Fatalf("op 3 = % X, want % X", got, want)
	}
	if got, want := EncodeQuestUpdateAbandon(0x0107), append([]byte{4}, u32le(0x0107)...); !bytes.Equal(got, want) {
		t.Fatalf("op 4 = % X, want % X", got, want)
	}
}

// TestQuestInfoBodyFlagCombos pins the flag-driven SQuestInfo body
// emission across the wire-legal combos the client-side parity listing
// pins (0x00, 0x04, 0x40, 0x44 and the full 0x5c), plus this server's
// own 0x18 shape and the 0xFF objective sentinel.
func TestQuestInfoBodyFlagCombos(t *testing.T) {
	t.Parallel()
	for _, testCase := range []struct {
		name   string
		record enterworld.ActiveQuestRecord
		want   []byte
	}{
		{
			name:   "flags 0x00: header only",
			record: enterworld.ActiveQuestRecord{RefID: 9, U08: 0x12, U09: 0x34, Flags: 0},
			want:   []byte{0x12, 0x34, 0x00},
		},
		{
			name:   "flags 0x04: progress word (zero still emits 4 bytes)",
			record: enterworld.ActiveQuestRecord{RefID: 9, Flags: 0x04, Progress: 0},
			want:   []byte{0, 0, 0x04, 0, 0, 0, 0},
		},
		{
			name:   "flags 0x40: target-id vector",
			record: enterworld.ActiveQuestRecord{RefID: 9, Flags: 0x40, TargetIds: []uint32{0x0107, 0x0108}},
			want:   append(append([]byte{0, 0, 0x40, 2}, u32le(0x0107)...), u32le(0x0108)...),
		},
		{
			name: "flags 0x18 (this server's insert shape): kind byte + one counted contents node",
			record: enterworld.ActiveQuestRecord{
				RefID: 9, Flags: 0x18, U10: 2,
				Contents: []enterworld.ActiveQuestContentsNode{
					{Tag: 1, Kind: 1, Description: "SN_C", ObjectiveValues: []uint32{3}},
				},
			},
			want: append(append([]byte{
				0, 0, 0x18,
				2,    // u10 kind byte (flags&0x08)
				1,    // contents count (flags&0x10)
				1, 1, // node tag, node kind
				4, 0, // wire string length u16
				'S', 'N', '_', 'C',
				1, // objectiveCount
			}, u32le(3)...), []byte{}...),
		},
		{
			name: "the 0xFF objective sentinel: NO value array bytes",
			record: enterworld.ActiveQuestRecord{
				RefID: 9, Flags: 0x10,
				Contents: []enterworld.ActiveQuestContentsNode{
					{Tag: 1, Kind: 1, Description: "SN", ObjectiveSentinel: true},
				},
			},
			want: []byte{0, 0, 0x10, 1, 1, 1, 2, 0, 'S', 'N', 0xff},
		},
		{
			name: "flags 0x5c: progress + kind + contents + targets, wire order",
			record: enterworld.ActiveQuestRecord{
				RefID: 9, U08: 1, U09: 2, Flags: 0x5c, Progress: 0xdeadbeef, U10: 7,
				Contents: []enterworld.ActiveQuestContentsNode{
					{Tag: 3, Kind: 0, Description: "SN", ObjectiveValues: []uint32{1, 2}},
				},
				TargetIds: []uint32{0x0107},
			},
			want: append(append(append(append([]byte{
				1, 2, 0x5c,
			}, u32le(0xdeadbeef)...), []byte{
				7,    // u10
				1,    // contents count
				3, 0, // tag, kind
				2, 0, 'S', 'N',
				2, // objectiveCount
			}...), append(u32le(1), u32le(2)...)...), append([]byte{1}, u32le(0x0107)...)...),
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			got := EncodeQuestUpdateUpdate(testCase.record)
			want := append(append([]byte{QuestUpdateOpUpdate}, u32le(testCase.record.RefID)...), testCase.want...)
			if !bytes.Equal(got, want) {
				t.Fatalf("op-2 payload = % X, want % X", got, want)
			}
			insert := EncodeQuestUpdateInsert(testCase.record)
			if insert[0] != QuestUpdateOpInsert || !bytes.Equal(insert[1:], got[1:]) {
				t.Fatalf("op-1 must carry the identical body after the op byte: % X vs % X", insert, got)
			}
		})
	}
}
