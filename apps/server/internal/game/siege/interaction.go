/*
===========================================================================

interaction.go - v1.150 fortress staff request decoding

703130 emits 0x71E1; 754A40 consumes 0xB1E1. The v1.188 server dispatcher
519E60 uses 0x705E and a wider error code, so its packet layout must not be
copied onto the v1.150 wire. Admission and durable changes belong to the
fortress authority; this file only decodes complete requests.

===========================================================================
*/
package siege

import (
	"encoding/binary"
	"fmt"
)

const (
	OpInteractionRequest  uint16 = 0x71e1
	OpInteractionResponse uint16 = 0xb1e1
)

const (
	ActionTaxQuery uint8 = iota
	ActionTaxRate
	ActionTaxCollect
	ActionStaffQuery
	ActionStaffHire
	ActionSchedule
	ActionRegistrationQuery
	ActionRegister
	ActionWithdraw
	ActionAide
	ActionConstruct
	ActionUpgrade
	ActionRepair
	ActionSmithQuery
	ActionSmithProduce
	ActionSmithCancel
	ActionSmithCollect
	ActionTrainerQuery
	ActionTrainerProduce
	ActionTrainerCancel
	ActionTrainerCollect
	ActionGate
	ActionDismiss
	ActionDemolish
	ActionStructureQuery
)

/*
================
Interaction

The target is an NPC for ordinary staff actions, a summoned object for
dismissal/demolition, and absent from the construction completion request.
Value16 preserves signed tax bits as well as unsigned quantities/gate state.
================
*/
type Interaction struct {
	Action    uint8
	Target    uint32
	Fortress  uint32
	Reference uint32
	Value16   uint16
	Value8    uint8
	Gold      int64
}

/*
================
DecodeInteraction

703130's construction completion has a different header and must use
DecodeConstruction after the authority identifies a pending construction.
Its bytes can also form a valid ordinary request; never sniff the first byte.
================
*/
func DecodeInteraction(payload []byte) (Interaction, error) {
	var request Interaction
	if len(payload) < 5 {
		return request, fmt.Errorf("truncated fortress interaction")
	}
	request.Target = binary.LittleEndian.Uint32(payload[:4])
	request.Action = payload[4]
	want := 9
	switch request.Action {
	case ActionRegistrationQuery, ActionAide:
		want = 5
	case ActionTaxQuery, ActionStaffQuery, ActionSchedule, ActionSmithQuery,
		ActionTrainerQuery, ActionDismiss, ActionDemolish, ActionStructureQuery:
	case ActionTaxRate, ActionGate:
		want = 11
	case ActionTaxCollect:
		want = 17
	case ActionStaffHire, ActionRegister, ActionWithdraw:
		want = 10
	case ActionUpgrade:
		want = 14
	case ActionRepair, ActionSmithCancel, ActionTrainerCancel:
		want = 13
	case ActionSmithProduce, ActionSmithCollect, ActionTrainerProduce, ActionTrainerCollect:
		want = 15
	default:
		return Interaction{}, fmt.Errorf("unsupported fortress action 0x%02X", request.Action)
	}
	if len(payload) != want {
		return Interaction{}, fmt.Errorf("fortress action 0x%02X: got %d bytes, want %d", request.Action, len(payload), want)
	}
	if want == 5 {
		return request, nil
	}
	request.Fortress = binary.LittleEndian.Uint32(payload[5:9])
	switch request.Action {
	case ActionTaxRate, ActionGate:
		request.Value16 = binary.LittleEndian.Uint16(payload[9:11])
	case ActionTaxCollect:
		request.Gold = int64(binary.LittleEndian.Uint64(payload[9:17]))
	case ActionStaffHire, ActionRegister, ActionWithdraw:
		request.Value8 = payload[9]
	case ActionUpgrade, ActionRepair, ActionSmithCancel, ActionTrainerCancel,
		ActionSmithProduce, ActionSmithCollect, ActionTrainerProduce, ActionTrainerCollect:
		request.Reference = binary.LittleEndian.Uint32(payload[9:13])
		if want == 14 {
			request.Value8 = payload[13]
		} else if want == 15 {
			request.Value16 = binary.LittleEndian.Uint16(payload[13:15])
		}
	}
	return request, nil
}

/*
================
DecodeConstruction

The action-0xA completion branch at 703160 has no target GID.
================
*/
func DecodeConstruction(payload []byte) (Interaction, error) {
	if len(payload) != 9 || payload[0] != ActionConstruct {
		return Interaction{}, fmt.Errorf("invalid fortress construction completion")
	}
	return Interaction{
		Action:    ActionConstruct,
		Fortress:  binary.LittleEndian.Uint32(payload[1:5]),
		Reference: binary.LittleEndian.Uint32(payload[5:9]),
	}, nil
}

/*
================
InteractionService

519E60's service admission table, restricted to the v1.150 action domain.
Zero identifies object actions with their own admission, never an NPC grant.
================
*/
func InteractionService(action uint8) uint8 {
	switch {
	case action <= ActionSchedule:
		return 0x17
	case action <= ActionWithdraw:
		return 0x18
	case action <= ActionRepair || action == ActionStructureQuery:
		return 0x19
	case action <= ActionSmithCollect:
		return 0x1a
	case action <= ActionTrainerCollect:
		return 0x1b
	case action == ActionGate:
		return 0x1f
	default:
		return 0
	}
}
