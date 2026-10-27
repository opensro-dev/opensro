package wire

import "fmt"

// SkillAction is one C→S 0x72CD skill/action-use body. Two composers exist
// (from the exhaustive sub_4fc9e0/sub_71ff00 send-site census): sub_6fcd50 /
// CGInterface_ExecuteSelectedActionAtTarget builds the flag-0x00/0x01 bodies
// in place, and sub_878100 / CharMovement_ResendGoalMove builds the
// flag-0x02 ground-location body on the deferred walk-into-range path.
//
// Wire (ASM-PINNED against SRO_Client.exe; three layouts):
//
//	no-target:       [0x01][0x04][u32le actionId][0x00]
//	with-target:     [0x01][0x04][u32le actionId][0x01][u32le targetGid]
//	ground-location: [0x01][0x04][u32le actionId][0x02][u16le region]
//	                 [u16le x][u16le y][u16le z]
//
// Discriminator vs pickup: pickup uses second byte 0x02
// ([01][02][01][gid]); skill-action uses second byte 0x04. DecodeTargetInteract
// must keep rejecting the 0x04 family so a skill send never grants an item.
type SkillAction struct {
	// ActionId is the skill/action host id the dispatch rides: the clicked
	// control's CIFWnd+0x5c0 commandId, pushed as sub_6fcd50's one dword
	// argument (there is NO "selected action id 758" field - CGInterface
	// +0x758 is the cool-time list pointer). On the deferred flag-0x02 path
	// the same id is armed at the click-move controller's +0x4c
	// (armedLocationCastActionId4c) and composed later by sub_878100.
	ActionId uint32
	// HasTarget is true for the flag-0x01 body (a target gid follows).
	HasTarget bool
	// TargetGid is the selected target entity id when HasTarget is true
	// (CGInterface_GetSelectedTarget → stack slot consumed by AppendBytes).
	TargetGid uint32
	// HasGroundTarget is true for the flag-0x02 ground-location body
	// (the sub_878100 second composer; region + xyz words follow).
	HasGroundTarget bool
	// Region / GroundX / GroundY / GroundZ are the flag-0x02 location words.
	Region  uint16
	GroundX uint16
	GroundY uint16
	GroundZ uint16
}

const (
	skillActionExecute      uint8 = 0x01
	skillActionLeg          uint8 = 0x04
	skillActionNoTarget     uint8 = 0x00
	skillActionHasTarget    uint8 = 0x01
	skillActionGroundTarget uint8 = 0x02
)

// Encode returns the 0x72CD skill-action payload for the pinned layouts.
func (s SkillAction) Encode() []byte {
	w := NewWriter(15).
		U8(skillActionExecute).
		U8(skillActionLeg).
		U32(s.ActionId)
	if s.HasTarget {
		return w.U8(skillActionHasTarget).U32(s.TargetGid).Payload()
	}
	if s.HasGroundTarget {
		return w.U8(skillActionGroundTarget).
			U16(s.Region).U16(s.GroundX).U16(s.GroundY).U16(s.GroundZ).
			Payload()
	}
	return w.U8(skillActionNoTarget).Payload()
}

// DecodeSkillAction parses a 0x72CD skill-action body. Pickup forms and any
// other discriminator fail closed.
func DecodeSkillAction(payload []byte) (SkillAction, error) {
	var out SkillAction
	r := NewReader(payload)

	lead, err := r.U8()
	if err != nil {
		return out, err
	}
	if lead != skillActionExecute {
		return out, fmt.Errorf("wire: 0x72CD skill-action lead 0x%02X is not execute", lead)
	}

	leg, err := r.U8()
	if err != nil {
		return out, err
	}
	if leg != skillActionLeg {
		return out, fmt.Errorf("wire: 0x72CD skill-action leg 0x%02X is not 0x04", leg)
	}

	if out.ActionId, err = r.U32(); err != nil {
		return out, err
	}

	flag, err := r.U8()
	if err != nil {
		return out, err
	}
	switch flag {
	case skillActionNoTarget:
		out.HasTarget = false
		return out, r.Done()
	case skillActionHasTarget:
		out.HasTarget = true
		if out.TargetGid, err = r.U32(); err != nil {
			return out, err
		}
		return out, r.Done()
	case skillActionGroundTarget:
		out.HasGroundTarget = true
		if out.Region, err = r.U16(); err != nil {
			return out, err
		}
		if out.GroundX, err = r.U16(); err != nil {
			return out, err
		}
		if out.GroundY, err = r.U16(); err != nil {
			return out, err
		}
		if out.GroundZ, err = r.U16(); err != nil {
			return out, err
		}
		return out, r.Done()
	default:
		return out, fmt.Errorf("wire: 0x72CD skill-action target flag 0x%02X is not one of {0,1,2}", flag)
	}
}

// IsSkillActionPayload reports whether payload matches a pinned skill-action
// shape without allocating a decoded struct. Used by multiplex callers that
// must not treat skill bytes as pickup.
func IsSkillActionPayload(payload []byte) bool {
	_, err := DecodeSkillAction(payload)
	return err == nil
}
