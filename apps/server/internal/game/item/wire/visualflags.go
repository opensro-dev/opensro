package wire

import "fmt"

const (
	OpVisualFlagsRequest uint16 = 0x7683
	OpVisualFlagsUpdate  uint16 = 0xB683
)

// DecodeVisualFlagsRequest is the exact body the client's
// NetClient_SendVisualFlags7683 composes: one complete
// CICUser+0x779 byte, not a boolean toggle.
func DecodeVisualFlagsRequest(payload []byte) (uint8, error) {
	if len(payload) != 1 {
		return 0, fmt.Errorf("visual flags request length %d, want 1", len(payload))
	}
	return payload[0], nil
}

// EncodeVisualFlagsUpdate is the exact body consumed by the client's
// CPSMission_OnEntityNavUpdateResponse0xB683: [entity gid:u32]
// [complete flags:u8].
func EncodeVisualFlagsUpdate(gid uint32, flags uint8) []byte {
	return NewWriter(5).U32(gid).U8(flags).Payload()
}
