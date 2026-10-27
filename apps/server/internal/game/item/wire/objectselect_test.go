package wire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

// W5 direct codec falsifiers for the 0x745A decode: the live native gateway
// trace (clicking the spawned NPC_EU_SMITH) carried body 41 0D 03 00 =
// gid 200001.
// The handler tests in internal/game/action/select_test.go replay the same bytes
// end-to-end; this pins the decoded value in isolation. The 0xB45A encoder
// pins below hand-roll their oracle bytes with encoding/binary so the
// assertion cannot inherit a Writer bug.

func TestDecodeObjectSelectRequestPinsTheNativeTraceBytes(t *testing.T) {
	gid, err := DecodeObjectSelectRequest([]byte{0x41, 0x0D, 0x03, 0x00})
	if err != nil {
		t.Fatalf("the captured retail frame refused: %v", err)
	}
	if gid != 200001 {
		t.Errorf("gid = %d, want 200001 (0x00030D41 little-endian)", gid)
	}
}

func TestDecodeObjectSelectRequestRejectsWrongLengths(t *testing.T) {
	for _, payload := range [][]byte{{}, {0x41}, {0x41, 0x0D, 0x03}, {0x41, 0x0D, 0x03, 0x00, 0x00}} {
		if _, err := DecodeObjectSelectRequest(payload); err == nil {
			t.Errorf("payload % X accepted, want error (body is exactly one u32)", payload)
		}
	}
}

// TestEncodeNpcObjectSelectResultPinsTheSmithGrantBytes is the literal oracle
// for the frame the live NPC_EU_SMITH click elicits: gid 200001 (the
// traced roster gid), capability flags 0x23, npcExtra 0. The bytes are
// pasted, byte-identical to the client composer devComposeSyntheticB45A for
// the same inputs - the frame the folded sub_764c60 CHARACTER arm reads.
func TestEncodeNpcObjectSelectResultPinsTheSmithGrantBytes(t *testing.T) {
	want := []byte{
		0x01,                   // result 1
		0x41, 0x0D, 0x03, 0x00, // gid 200001 le
		0x00,                   // vitalsMask 0
		0x23, 0x00, 0x00, 0x00, // capabilityFlags 0x23 le
		0x00, // npcExtra 0
	}
	if got := EncodeNpcObjectSelectResult(200001, 0x23, 0); !bytes.Equal(got, want) {
		t.Errorf("smith grant\n got % X\nwant % X", got, want)
	}
}

// TestEncodeNpcObjectSelectResultPinsTheFieldOrder hand-rolls the oracle with
// encoding/binary over inputs whose every field differs, npcExtra nonzero
// included, so a swapped or widened field cannot cancel out.
func TestEncodeNpcObjectSelectResultPinsTheFieldOrder(t *testing.T) {
	const (
		gid      uint32 = 0xA1B2C3D4
		flags    uint32 = 0x00004001
		npcExtra uint8  = 0x02
	)
	want := []byte{0x01}
	want = binary.LittleEndian.AppendUint32(want, gid)
	want = append(want, 0x00)
	want = binary.LittleEndian.AppendUint32(want, flags)
	want = append(want, npcExtra)
	got := EncodeNpcObjectSelectResult(gid, flags, npcExtra)
	if len(got) != 11 {
		t.Fatalf("frame length = %d, want 11 (the fixed live-CICNPC shape)", len(got))
	}
	if !bytes.Equal(got, want) {
		t.Errorf("grant body\n got % X\nwant % X", got, want)
	}
}

func TestEncodeMonsterObjectSelectResultPinsCurrentHPShape(t *testing.T) {
	const (
		gid       uint32 = 400001
		currentHP uint32 = 1080
	)
	want := []byte{1}
	want = binary.LittleEndian.AppendUint32(want, gid)
	want = append(want, 1)
	want = binary.LittleEndian.AppendUint32(want, currentHP)
	want = binary.LittleEndian.AppendUint32(want, 0)

	got := EncodeMonsterObjectSelectResult(gid, currentHP)
	if len(got) != 14 {
		t.Fatalf("frame length = %d, want 14 (exact CICMonster shape)", len(got))
	}
	if !bytes.Equal(got, want) {
		t.Errorf("monster grant\n got % X\nwant % X", got, want)
	}
}
