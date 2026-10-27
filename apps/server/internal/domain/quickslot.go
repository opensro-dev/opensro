package domain

// QuickSlotBinding is one persisted `_QuickSlotData` row. Slot is the
// CIFUnderBar +0x3b4 index (0..50), Kind is the native control state kind and
// Payload is the type-dependent dword carried by the six-byte HUD-state row.
type QuickSlotBinding struct {
	Slot    uint8  `json:"slot"`
	Kind    uint8  `json:"kind"`
	Payload uint32 `json:"payload"`
}

const QuickSlotCount = 0x33

// QuickSlotKindValid is the complete client 572080/572E00 state-kind set.
func QuickSlotKindValid(kind uint8) bool {
	switch kind {
	case 0, 0x25, 0x46, 0x47, 0x49, 0x4a, 0x4e:
		return true
	default:
		return false
	}
}
