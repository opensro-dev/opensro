package wire

import (
	"encoding/binary"
	"fmt"
)

const OpCosBehaviorRequest uint16 = 0x705b
const OpCosBehaviorResult uint16 = 0xb05b

// v1.150 6FFA60: GID, family selector, complete command-mode dword.
type CosBehavior struct {
	GID      uint32
	Selector uint8
	Mode     uint32
}

func DecodeCosBehavior(p []byte) (CosBehavior, error) {
	if len(p) != 9 || (p[4] != 1 && p[4] != 2) {
		return CosBehavior{}, fmt.Errorf("invalid COS behavior request")
	}
	q := CosBehavior{binary.LittleEndian.Uint32(p), p[4], binary.LittleEndian.Uint32(p[5:])}
	if q.GID == 0 {
		return CosBehavior{}, fmt.Errorf("missing COS identity")
	}
	return q, nil
}

// Cash-pet state as written by the client's CICCos_ExecuteActionCommand,
// CosEntryPanel_SendCashPetPickupSetting, CIFPetConfig_OnPickupModeChanged
// and CIFPetConfig_OnPickupCategoryChanged: preserve unexposed bits
// instead of accepting client mutations to undocumented flags.
func (q CosBehavior) ValidTransition(band, previous uint32) bool {
	return q.Selector == 1 && band == 3 && q.Mode <= 1 ||
		q.Selector == 2 && band == 4 && (q.Mode^previous) & ^uint32(0xc7) == 0
}

func (q CosBehavior) Success() Frame {
	return Frame{Opcode: OpCosBehaviorResult, Payload: NewWriter(10).U8(1).U32(q.GID).U8(q.Selector).U32(q.Mode).Payload()}
}
