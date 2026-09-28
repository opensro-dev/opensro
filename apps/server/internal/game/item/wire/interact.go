/*
===========================================================================

interact.go - exact target-action wire bodies for attack, follow and pickup

The second byte selects the authority owner. Trace is family 3, never an
alternate attack spelling; accepting it as combat changes player intent.

===========================================================================
*/
package wire

import "fmt"

/*
================
TargetInteract

The ground-item request at client 698B45 is [01 02 01 u32le gid]. The
bare [02] command cancels the current approach. These exact discriminators
keep skills and follow requests out of the pickup reply conversation.

693190 emits a throttled cancel, not a pickup retry. Duplicate pickup
requests can still come from clicks and must remain idempotent while the
server owns travel. Cancellation releases the native latch at +618.
================
*/
type TargetInteract struct {
	// Cancel is true for the bare [0x02] form; Gid is then meaningless.
	Cancel bool
	// Gid is the ground-item entity id being interacted with.
	Gid uint32
}

/*
================
BasicAttackEngage

Client 692CB0 emits [01 01 01 gid]. The authority owns repeated approach
and attacks until a superseding command or invalid state ends the intent.
================
*/
type BasicAttackEngage struct {
	TargetGid uint32
}

/*
================
FollowTarget

Client 695420 action 1003 emits [01 03 01 gid]. Server 4AE3D0 starts
pursuit of another player without creating a skill or attack instance.
================
*/
type FollowTarget struct {
	TargetGid uint32
}

const (
	targetInteractExecute   uint8 = 0x01
	targetInteractCancel    uint8 = 0x02
	targetInteractGroundLeg uint8 = 0x02
	targetInteractItemKind  uint8 = 0x01
	targetInteractAttackLeg uint8 = 0x01
	targetInteractFollowLeg uint8 = 0x03
	targetInteractActorKind uint8 = 0x01
)

/*
================
BasicAttackEngage.Encode
================
*/
func (b BasicAttackEngage) Encode() []byte {
	return NewWriter(7).
		U8(targetInteractExecute).
		U8(targetInteractAttackLeg).
		U8(targetInteractActorKind).
		U32(b.TargetGid).
		Payload()
}

/*
================
FollowTarget.Encode
================
*/
func (f FollowTarget) Encode() []byte {
	return NewWriter(7).U8(targetInteractExecute).U8(targetInteractFollowLeg).
		U8(targetInteractActorKind).U32(f.TargetGid).Payload()
}

/*
================
decodeActorTarget

Shared shape validation keeps strict extent and identity checks consistent
without allowing one action family to fall through into another owner.
================
*/
func decodeActorTarget(payload []byte, expectedLeg uint8) (uint32, error) {
	r := NewReader(payload)
	lead, err := r.U8()
	if err != nil {
		return 0, err
	}
	leg, err := r.U8()
	if err != nil {
		return 0, err
	}
	kind, err := r.U8()
	if err != nil {
		return 0, err
	}
	if lead != targetInteractExecute || kind != targetInteractActorKind ||
		leg != expectedLeg {
		return 0, fmt.Errorf(
			"wire: 0x72CD discriminators %02X %02X %02X do not select actor family %02X",
			lead, leg, kind, expectedLeg,
		)
	}
	gid, err := r.U32()
	if err != nil {
		return 0, err
	}
	if gid == 0 {
		return 0, fmt.Errorf("wire: 0x72CD actor target gid is zero")
	}
	return gid, r.Done()
}

/*
================
DecodeBasicAttackEngage
================
*/
func DecodeBasicAttackEngage(payload []byte) (BasicAttackEngage, error) {
	gid, err := decodeActorTarget(payload, targetInteractAttackLeg)
	return BasicAttackEngage{TargetGid: gid}, err
}

/*
================
DecodeFollowTarget
================
*/
func DecodeFollowTarget(payload []byte) (FollowTarget, error) {
	gid, err := decodeActorTarget(payload, targetInteractFollowLeg)
	return FollowTarget{TargetGid: gid}, err
}

/*
================
TargetInteract.Encode
================
*/
func (t TargetInteract) Encode() []byte {
	if t.Cancel {
		return []byte{targetInteractCancel}
	}
	return NewWriter(7).
		U8(targetInteractExecute).
		U8(targetInteractGroundLeg).
		U8(targetInteractItemKind).
		U32(t.Gid).
		Payload()
}

/*
================
DecodeTargetInteract

Pickup and cancellation have their own reply conversation.
================
*/
func DecodeTargetInteract(payload []byte) (TargetInteract, error) {
	var out TargetInteract
	r := NewReader(payload)

	lead, err := r.U8()
	if err != nil {
		return out, err
	}

	switch lead {
	case targetInteractCancel:
		out.Cancel = true
		return out, r.Done()
	case targetInteractExecute:
		leg, err := r.U8()
		if err != nil {
			return out, err
		}
		kind, err := r.U8()
		if err != nil {
			return out, err
		}
		if leg != targetInteractGroundLeg || kind != targetInteractItemKind {
			return out, fmt.Errorf("wire: 0x72CD discriminators %02X %02X are not the ground-item leg", leg, kind)
		}
		if out.Gid, err = r.U32(); err != nil {
			return out, err
		}
		return out, r.Done()
	default:
		return out, fmt.Errorf("wire: 0x72CD lead byte 0x%02X is neither execute nor cancel", lead)
	}
}
