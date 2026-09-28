/*
===========================================================================

targetaction.go - dispatch ownership for the multiplexed 0x72CD command

Classify the family before decoding its body. A malformed request remains
with its owner and cannot trigger another family's response conversation.

===========================================================================
*/
package wire

import "fmt"

/*
================
TargetActionLane
================
*/
type TargetActionLane uint8

const (
	TargetActionUnknown TargetActionLane = iota
	TargetActionCancel
	TargetActionBasicAttack
	TargetActionGroundItemPickup
	TargetActionFollow
	TargetActionSkill
	TargetActionFortressStructure
	TargetActionCancelActiveEffect
)

/*
================
ClassifyTargetActionLane

Exact extent and tail validation belong to the selected family decoder.
================
*/
func ClassifyTargetActionLane(payload []byte) TargetActionLane {
	if len(payload) == 0 {
		return TargetActionUnknown
	}
	if payload[0] == targetInteractCancel {
		if len(payload) == 1 {
			return TargetActionCancel
		}
		if len(payload) >= 2 && payload[1] == 0x01 {
			return TargetActionFortressStructure
		}
		return TargetActionUnknown
	}
	if payload[0] != targetInteractExecute || len(payload) < 2 {
		return TargetActionUnknown
	}

	switch payload[1] {
	case targetInteractAttackLeg:
		return TargetActionBasicAttack
	case targetInteractGroundLeg:
		return TargetActionGroundItemPickup
	case targetInteractFollowLeg:
		return TargetActionFollow
	case skillActionLeg:
		return TargetActionSkill
	case 0x05:
		return TargetActionCancelActiveEffect
	default:
		return TargetActionUnknown
	}
}

/*
================
FortressStructureInteract

Client 692CB0's CICATStruct form is [02 01 01 gid], distinct from bare
[02] cancellation and [01 02 01 gid] pickup.
================
*/
type FortressStructureInteract struct {
	TargetGid uint32
}

/*
================
FortressStructureInteract.Encode
================
*/
func (f FortressStructureInteract) Encode() []byte {
	return NewWriter(7).U8(0x02).U8(0x01).U8(0x01).U32(f.TargetGid).Payload()
}

/*
================
DecodeFortressStructureInteract
================
*/
func DecodeFortressStructureInteract(payload []byte) (FortressStructureInteract, error) {
	var out FortressStructureInteract
	r := NewReader(payload)
	lead, err := r.U8()
	if err != nil {
		return out, err
	}
	leg, err := r.U8()
	if err != nil {
		return out, err
	}
	kind, err := r.U8()
	if err != nil {
		return out, err
	}
	if lead != 0x02 || leg != 0x01 || kind != 0x01 {
		return out, fmt.Errorf("wire: 0x72CD fortress-structure discriminators are %02X %02X %02X, want 02 01 01", lead, leg, kind)
	}
	if out.TargetGid, err = r.U32(); err != nil {
		return out, err
	}
	if out.TargetGid == 0 {
		return FortressStructureInteract{}, fmt.Errorf("wire: 0x72CD fortress-structure target gid is zero")
	}
	return out, r.Done()
}

/*
================
CancelActiveEffectRequest

Client 6FD710 and server 4AE520 resolve EffectID as a skill record. The
optional nonzero InstanceToken narrows cancellation to one active instance.
================
*/
type CancelActiveEffectRequest struct {
	EffectID      uint32
	InstanceToken uint32
}

/*
================
CancelActiveEffectRequest.Encode
================
*/
func (a CancelActiveEffectRequest) Encode() []byte {
	capacity := 7
	if a.InstanceToken != 0 {
		capacity = 11
	}
	w := NewWriter(capacity).U8(0x01).U8(0x05).U32(a.EffectID)
	if a.InstanceToken != 0 {
		w.U32(a.InstanceToken)
	}
	return w.U8(0x00).Payload()
}

/*
================
DecodeCancelActiveEffectRequest
================
*/
func DecodeCancelActiveEffectRequest(payload []byte) (CancelActiveEffectRequest, error) {
	var out CancelActiveEffectRequest
	r := NewReader(payload)
	lead, err := r.U8()
	if err != nil {
		return out, err
	}
	leg, err := r.U8()
	if err != nil {
		return out, err
	}
	if lead != 0x01 || leg != 0x05 {
		return out, fmt.Errorf("wire: 0x72CD cancel-active-effect discriminators are %02X %02X, want 01 05", lead, leg)
	}
	if out.EffectID, err = r.U32(); err != nil {
		return out, err
	}
	if out.EffectID == 0 {
		return CancelActiveEffectRequest{}, fmt.Errorf("wire: 0x72CD cancel-active-effect id is zero")
	}

	switch r.Remaining() {
	case 1:
	case 5:
		if out.InstanceToken, err = r.U32(); err != nil {
			return out, err
		}
		if out.InstanceToken == 0 {
			return CancelActiveEffectRequest{}, fmt.Errorf("wire: 0x72CD cancel-active-effect optional instance token is zero")
		}
	default:
		return out, fmt.Errorf("wire: 0x72CD cancel-active-effect body has %d bytes after primary id, want 1 or 5", r.Remaining())
	}

	tail, err := r.U8()
	if err != nil {
		return out, err
	}
	if tail != 0x00 {
		return CancelActiveEffectRequest{}, fmt.Errorf("wire: 0x72CD cancel-active-effect tail is %02X, want 00", tail)
	}
	return out, r.Done()
}
