package wire

import (
	"encoding/binary"
	"testing"
)

// The 0x343C block layout, byte for byte against sub_75be90's read order.
// THE load-bearing assertion is the offset of the STR/INT words: the
// handler applies +0x20/+0x22 to the live player words, while the
// +0x14/+0x16 pair that reads like STR/INT is staged and dropped. A layout
// that fills the wrong pair (as cmd/active-probe's builder did before the
// progression-wave correction) seeds a player with STR/INT 0.
func TestBaseStatsEncodesStatWordsAtTheAppliedOffsets(t *testing.T) {
	payload := BaseStats{
		MaxHP:   1234,
		MaxMP:   5678,
		StrWord: 111,
		IntWord: 222,
	}.Encode()

	if len(payload) != BaseStatsSize {
		t.Fatalf("block = %d bytes, want 0x%02X", len(payload), BaseStatsSize)
	}
	if got := binary.LittleEndian.Uint32(payload[0x18:]); got != 1234 {
		t.Fatalf("maxHP at +0x18 = %d, want 1234", got)
	}
	if got := binary.LittleEndian.Uint32(payload[0x1c:]); got != 5678 {
		t.Fatalf("maxMP at +0x1c = %d, want 5678", got)
	}
	if got := binary.LittleEndian.Uint16(payload[0x20:]); got != 111 {
		t.Fatalf("STR at +0x20 = %d, want 111 (the word sub_8629f0 applies)", got)
	}
	if got := binary.LittleEndian.Uint16(payload[0x22:]); got != 222 {
		t.Fatalf("INT at +0x22 = %d, want 222 (the word sub_862a00 applies)", got)
	}
	// The lookalike staging pair must stay zero: filling it was the
	// old active-probe trap.
	if got := binary.LittleEndian.Uint16(payload[0x14:]); got != 0 {
		t.Fatalf("+0x14 = %d, want 0 - that word is staged and never applied", got)
	}
	if got := binary.LittleEndian.Uint16(payload[0x16:]); got != 0 {
		t.Fatalf("+0x16 = %d, want 0", got)
	}
}

func TestPointsSkillUpdateLayout(t *testing.T) {
	payload := EncodePointsSkillUpdate(4242, false)

	if len(payload) != 6 {
		t.Fatalf("payload = %d bytes, want 6 ([type][u32][flag])", len(payload))
	}
	if payload[0] != PointsTypeSkill {
		t.Fatalf("type = %d, want %d (skill points)", payload[0], PointsTypeSkill)
	}
	if got := binary.LittleEndian.Uint32(payload[1:]); got != 4242 {
		t.Fatalf("skill points = %d, want the absolute 4242", got)
	}
	if payload[5] != 0 {
		t.Fatalf("notify flag = %d, want 0", payload[5])
	}
	if notified := EncodePointsSkillUpdate(1, true); notified[5] != 1 {
		t.Fatalf("notify flag = %d, want 1", notified[5])
	}
}

func TestPointsAckLayout(t *testing.T) {
	if got := EncodePointsAck(true, 0); len(got) != 1 || got[0] != ResultSuccess {
		t.Fatalf("success ack = %v, want the single byte 0x01", got)
	}
	got := EncodePointsAck(false, ErrCodeStatAllocRefused)
	if len(got) != 2 || got[0] != ResultError || got[1] != ErrCodeStatAllocRefused {
		t.Fatalf("refusal ack = %v, want [02 %02X]", got, ErrCodeStatAllocRefused)
	}
}

func TestMasteryAckLayout(t *testing.T) {
	payload := EncodeMasteryLevelUpAck(513, 36)

	if len(payload) != 6 {
		t.Fatalf("payload = %d bytes, want 6 ([result][u32 id][u8 level])", len(payload))
	}
	if payload[0] != ResultSuccess {
		t.Fatalf("result = %d, want 1", payload[0])
	}
	if got := binary.LittleEndian.Uint32(payload[1:]); got != 513 {
		t.Fatalf("mastery id = %d, want 513", got)
	}
	if payload[5] != 36 {
		t.Fatalf("level = %d, want the POST-training 36", payload[5])
	}

	refusal := EncodeMasteryLevelUpError(ErrCodeMasterySkillPoints)
	if len(refusal) != 2 || refusal[0] != ResultError || refusal[1] != ErrCodeMasterySkillPoints {
		t.Fatalf("refusal = %v, want [02 02]", refusal)
	}
}

func TestDecodeMasteryLevelUpRequest(t *testing.T) {
	body := NewWriter(5).U32(258).U8(1).Payload()

	request, err := DecodeMasteryLevelUpRequest(body)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if request.MasteryID != 258 || request.Amount != 1 {
		t.Fatalf("request = %+v, want {258 1}", request)
	}

	if _, err := DecodeMasteryLevelUpRequest([]byte{0x01, 0x02}); err == nil {
		t.Fatal("a short body must refuse")
	}
	if _, err := DecodeMasteryLevelUpRequest(NewWriter(6).U32(258).U8(1).U8(9).Payload()); err == nil {
		t.Fatal("trailing bytes must refuse (a layout mismatch, not something to swallow)")
	}
}
