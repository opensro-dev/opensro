package wire

import "fmt"

// Target release closes a server-owned NPC interaction.
//
// v1.150 byte truth:
//   - C->S sub_693790 / CGInterface_SendTargetRelease74B3:
//     0x74B3 [trackedTargetGid624:u32le]
//   - S->C sub_761820 / CPSMission_HandleTalkCloseB4B3:
//     0xB4B3 [mode:u8] ([extra:u8] only when mode == 2)
//
// Mode 1 is the minimal close acknowledgement: the client unconditionally
// runs sub_69fff0's interaction-window sweep and needs no extra byte. Mode 2
// is the refusal: 761820 reads its one extra byte and discards it, then
// closes the interaction as for mode 1.
const (
	OpTargetReleaseRequest uint16 = 0x74B3
	OpTalkCloseResult      uint16 = 0xB4B3
)

// TargetReleaseRequestSize is exactly one little-endian object gid.
const TargetReleaseRequestSize = 4

// DecodeTargetReleaseRequest parses the exact sub_693790 request body.
func DecodeTargetReleaseRequest(payload []byte) (uint32, error) {
	if len(payload) != TargetReleaseRequestSize {
		return 0, fmt.Errorf(
			"wire: 0x74B3 body %d bytes, want exactly %d",
			len(payload),
			TargetReleaseRequestSize,
		)
	}
	reader := NewReader(payload)
	gid, err := reader.U32()
	if err != nil {
		return 0, err
	}
	return gid, nil
}

// EncodeTalkCloseResult composes 0xB4B3 mode 1. Do not append an invented
// success byte: sub_761820 interprets this first byte as a mode selector.
func EncodeTalkCloseResult() []byte {
	return []byte{1}
}

// TalkCloseRefusedCode is the byte mode 2 carries. INFERENCE: the v1.150
// client reads and discards it (761820) and the v1.188 server renumbers its
// opcodes, so no evidence names a value; any byte is equivalent to the
// client, and 1 is used.
const TalkCloseRefusedCode = 1

// EncodeTalkCloseRefusal composes 0xB4B3 mode 2, the answer to a release the
// server refuses: the client's release waits on this untagged reply.
func EncodeTalkCloseRefusal() []byte {
	return []byte{2, TalkCloseRefusedCode}
}
