package enterworld

// Count-field boundary tests for the u8->u16 object-list start fix: past
// 255 rows the start packet's count is a proper LE u16 (the encoding
// monstertick.go's despawn bracket always used). The char-data mastery
// and skill lists are the opposite shape - the native reader is
// marker-terminated and reads no count - so their bodies are never
// clamped.

import (
	"bytes"
	"fmt"
	"testing"
)

// buildCountedObjectListPackets drives buildBootstrapPackets with n
// synthetic object rows and returns the start payload plus the number of
// row packets emitted between the start and finalize brackets.
func buildCountedObjectListPackets(t *testing.T, n int) (startPayload []byte, rowCount int) {
	t.Helper()
	deps := &Deps{ObjectListRows: func(string, *Character, *LocalPlayerEntry) []Packet {
		rows := make([]Packet, n)
		for i := range rows {
			rows[i] = NewPacket(OpcodeObjectListChunk, []byte{byte(i)})
		}
		return rows
	}}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}

	packets, err := buildBootstrapPackets(deps, "global-official", &Character{Name: "rowcarrier"}, entry, nil, 100001)
	if err != nil {
		t.Fatal(err)
	}

	startIndex := -1
	finalizeIndex := -1
	for i, packet := range packets {
		switch packet.NativeOpcode {
		case OpcodeObjectListStart:
			startIndex = i
		case OpcodeObjectListFinalize:
			finalizeIndex = i
		}
	}
	if startIndex < 0 || finalizeIndex < 0 || finalizeIndex < startIndex {
		t.Fatalf("object-list bracket missing: start=%d finalize=%d", startIndex, finalizeIndex)
	}
	return packets[startIndex].Payload, finalizeIndex - startIndex - 1
}

// TestObjectListStartCountIsLittleEndianU16: at 256 and 300 rows the count
// field carries the real row count as LE u16 (the old hardcoded 0x00 high
// byte truncated it mod 256), and every row still emits.
func TestObjectListStartCountIsLittleEndianU16(t *testing.T) {
	cases := []struct {
		rows int
		want []byte
	}{
		{rows: 256, want: []byte{0x01, 0x00, 0x01}},
		{rows: 300, want: []byte{0x01, 0x2c, 0x01}},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%d-rows", tc.rows), func(t *testing.T) {
			payload, rowCount := buildCountedObjectListPackets(t, tc.rows)
			if len(payload) != 3 || payload[0] != tc.want[0] || payload[1] != tc.want[1] || payload[2] != tc.want[2] {
				t.Fatalf("start payload = %v, want %v", payload, tc.want)
			}
			if rowCount != tc.rows {
				t.Fatalf("emitted %d row packets, want %d", rowCount, tc.rows)
			}
		})
	}
}

// TestObjectListStartCountBelow256StaysByteIdentical: the fixed encoder's
// high byte is genuinely zero below 256, so the frozen fixture sequences
// (which never approach the boundary) carry the exact bytes they always
// did.
func TestObjectListStartCountBelow256StaysByteIdentical(t *testing.T) {
	payload, rowCount := buildCountedObjectListPackets(t, 7)
	want := []byte{0x01, 0x07, 0x00}
	if len(payload) != 3 || payload[0] != want[0] || payload[1] != want[1] || payload[2] != want[2] {
		t.Fatalf("start payload = %v, want %v", payload, want)
	}
	if rowCount != 7 {
		t.Fatalf("emitted %d row packets, want 7", rowCount)
	}
}

// TestObjectListRowsClampAtTheU16Ceiling: past 65535 rows the emitted rows
// hard-cap so the count field can never disagree with the body (no
// multi-frame paging contract is pinned; truncate-and-log is the posture).
func TestObjectListRowsClampAtTheU16Ceiling(t *testing.T) {
	payload, rowCount := buildCountedObjectListPackets(t, 0x10000+5)
	want := []byte{0x01, 0xff, 0xff}
	if len(payload) != 3 || payload[0] != want[0] || payload[1] != want[1] || payload[2] != want[2] {
		t.Fatalf("start payload = %v, want %v", payload, want)
	}
	if rowCount != 0xffff {
		t.Fatalf("emitted %d row packets, want %d (the u16 ceiling)", rowCount, 0xffff)
	}
}

// TestCharDataSkillListEmitsEveryLearnedSkill: skill learning has no cap,
// and the native reader (client 0x866e50) discards the lead byte and
// takes marker-framed rows until the terminator - so a record past 255
// learned skills emits every body, and the tail the old clamp dropped
// reaches the client.
func TestCharDataSkillListEmitsEveryLearnedSkill(t *testing.T) {
	skills := make([]uint32, 300)
	for i := range skills {
		skills[i] = uint32(i + 1)
	}
	character := &Character{
		ID:     1,
		Name:   "skillhoarder",
		Skills: skills,
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}

	payload := BuildLocalPlayerEntryPayload(character, entry, 0, nil)

	// The expected subsequence: empty mastery framing, the lead byte
	// (300 in a byte, unread), then all 300 marker-framed bodies
	// (ids 1..300) and the end marker before the empty quest block.
	var want bytes.Buffer
	want.Write([]byte{0x00, 0x02}) // mastery list: count 0, end marker
	want.WriteByte(300 & 0xff)     // skill lead byte, wrapped and unread
	for i := 1; i <= 300; i++ {
		want.Write([]byte{0x01, byte(i), byte(i >> 8), 0x00, 0x00, 0x01})
	}
	want.Write([]byte{0x02})             // skill end marker
	want.Write([]byte{0x00, 0x00, 0x00}) // empty quest block
	if !bytes.Contains(payload, want.Bytes()) {
		t.Fatal("char-data payload does not carry the full 300-body skill list")
	}
}

// TestCharDataMasteryListEmitsEveryPersistedMastery: the mastery list is
// the reader's first loop in the same function (client 0x866e50) - lead
// byte discarded, marker-framed rows - so it shares the skill list's
// shape: past 255 persisted masteries every body emits, and the tail the
// old clamp dropped reaches the client. Real records cap at the racial
// set (<=7); this pins the grammar, not a reachable population.
func TestCharDataMasteryListEmitsEveryPersistedMastery(t *testing.T) {
	masteries := make([]CharacterMastery, 300)
	for i := range masteries {
		masteries[i] = CharacterMastery{ID: uint32(1001 + i), Level: 1}
	}
	character := &Character{
		ID:        1,
		Name:      "masteryhoarder",
		Masteries: masteries,
	}
	entry := &LocalPlayerEntry{StartProfile: StartProfileForRaceProfile()}

	payload := BuildLocalPlayerEntryPayload(character, entry, 0, nil)

	// The expected subsequence: the lead byte (300 in a byte, unread),
	// then all 300 marker-framed bodies (ids 1001..1300 at level 1), the
	// end marker, the empty skill framing and the empty quest block.
	var want bytes.Buffer
	want.WriteByte(300 & 0xff) // mastery lead byte, wrapped and unread
	for i := 0; i < 300; i++ {
		id := uint32(1001 + i)
		want.Write([]byte{0x01, byte(id), byte(id >> 8), byte(id >> 16), byte(id >> 24), 0x01})
	}
	want.Write([]byte{0x02})             // mastery end marker
	want.Write([]byte{0x00, 0x02})       // skill list: count 0, end marker
	want.Write([]byte{0x00, 0x00, 0x00}) // empty quest block
	if !bytes.Contains(payload, want.Bytes()) {
		t.Fatal("char-data payload does not carry the full 300-body mastery list")
	}
}
