/*
===========================================================================

coscommand.go - the 0x769E COS command family and its S->C twins

The strict codec for summoned COS movement, attack, pickup, follow and mount
requests, and the frames the action lane answers with. The
comment on the opcode block below carries the native provenance.

===========================================================================
*/
package wire

import (
	"encoding/binary"
	"fmt"
)

// COS (internal/transport/pet) command plane, C->S opcode 0x769E.
//
// Two client producers are binary-pinned:
// the client's CGInterface_ExecuteActionCommand at 0x695F6B..0x69605D
// (command id 0x1388) emits exactly [u32le cosGid][u8 0x0B] after its own
// gates pass (local player present, gid is the ACTIVE COS at global-data
// header+0x10, the CCOSEntity_IsPetClass command-class gate, and the COS
// character's +0x644 bit 13 CLEAR - i.e. not already mounted); and
// CGInterface_InteractCharacterTarget's mounted follow/attack branch
// emits [u32 mountGid][u8 0x02][u32 targetGid] after resolving a nonzero
// effective-record +0x210.
//
// 0x769E is a multi-form FAMILY keyed on the trailing tag byte (C3 verify,
// server-wave seq 38): 0x01 movement, 0x02 approach, 0x03 direction stop,
// 0x04 steer, 0x08 pickup, 0x09 follow, 0x0B mount. The mount's movement trio is what
// CNavigationDeadreckon sends for a vehicle instead of the player opcodes:
// CNavigationDeadreckon_SendTargetMovePacket (client 0x877D80) tag 0x01
// with the 0x7738 body, CNavigationDeadreckon_SendAngleUpdatePacket
// (client 0x8777B0) tag 0x03 [u16 heading] instead of 0x72F5, and
// CNavigationDeadreckon_SendSteeringUpdate (client 0x877540) tag 0x04
// [u16 heading] instead of 0x72CF.
// Every supported form has an exact body length. Unknown tags are rejected.
// DropItemManager_TickAutoPickupRequests (client 77DFA0) emits tag 8 with
// a target; CICCos_ExecuteActionCommand (client 6A2777) emits tag 9
// without a target.
//
// S->C twins (emitted by action.HandleCosCommand after authoritative
// ActiveCOS identity/capability gates):
//   - 0xB4B5 ride-state apply (client CPSMission_OnCOSRideStateB4B5, LIVE
//     in the browser bridge):
//     mode 1 = [u8 1][u32 riderGid][u8 rideState][u32 vehicleGid],
//     rideState 1 mounts / 0 dismounts. This - not 0xB69E - is what makes a
//     client actually mount.
//   - 0xB69E COS command-result feedback (client
//     CPSMission_OnSystemNotification0xB69E, LIVE):
//     [u8 subType][u8 selector]([u8 resultByte] iff subType==2)[u32 cosGid],
//     selector 8 appends [u32 dropItemGid]. The refusal bytes retail sends
//     for a mount the client itself could never compose are UNPINNED, so
//     refusals stay off the wire entirely.
const (
	// OpCosCommandRequest is the client's COS command family (0x769E).
	OpCosCommandRequest uint16 = 0x769E
	// OpCosRideRequest is the pet panel's explicit ride-state request.
	OpCosRideRequest uint16 = 0x74B5
	// OpCosRideState is the S->C ride-state apply (0xB4B5, client
	// CPSMission_OnCOSRideStateB4B5).
	OpCosRideState uint16 = 0xB4B5
	// OpCosRecordCreate is the S->C COS record seed consumed by the
	// client's CCOSDataManager_DeserializeOwnedCOSRecord.
	OpCosRecordCreate uint16 = 0x3158
	// OpCosCommandResult is the S->C command-result feedback (0xB69E,
	// client CPSMission_OnSystemNotification0xB69E).
	OpCosCommandResult uint16 = 0xB69E
	// OpCosStateRefresh is the S->C COS buff/state refresh (0x3691,
	// client CPSMission_OnRefreshBuffRemainTime0x3691, registrar row 74:
	// opcode write at client 0x0074DCB2). One subtype byte selects the
	// wire shape; only subtype 3 is emitted here.
	OpCosStateRefresh uint16 = 0x3691
)

// CosStateRefreshSummonTimer is the subtype byte the client's
// CPSMission_OnRefreshBuffRemainTime0x3691 routes to the kind-3
// list-control command (CGInterface_UpdateMagicStateSlot ->
// CIFMagicStateBoard_SetSlotRemainingTime), which builds the
// magic-state board's two-bar COS slot.
const CosStateRefreshSummonTimer uint8 = 3

/*
================
EncodeCosSummonTimer3691

EncodeCosSummonTimer3691 builds the 0x3691 subtype-3 body:

	[u8 3][u32 itemRefObjID][u32 remainingSec][u32 packedExtra]

The client's CPSMission_OnRefreshBuffRemainTime0x3691 at 0x7760FE reads
the three u32s and dispatches CGInterface_UpdateMagicStateSlot(gi, 3,
id, 0, 0, 0) when both trailing words are zero, otherwise
CGInterface_UpdateMagicStateSlot(gi, 3, id, 1, remainingSec,
packedExtra). That flag reaches CIFMagicStateBoard_AddOrRemoveSlot as
its submit selector: 0 REMOVES the matching row, 1 upserts it, so a zero
pair is the native retirement rather than an empty window.

CIFMagicStateBoard_SetSlotRemainingTime kind 3 resolves the ITEM record
through GlobalDataManager_GetItemCommonRecordById and seeds the row from
its +0x29C duration: the limit is duration*1000 and the elapsed
accumulator is limit - remainingSec*1000, which
CIFMagicStateBoard_OnUpdate then draws as (limit - elapsed) / limit.
The third word is what is LEFT, never what has
run; kinds 6, 7 and 8 subtract their own arg from a fixed cap identically.
The +0x2A4 aux is the second bar's limit and the packed extra's low u16 is
its own remaining, in seconds. The id is the SUMMONING ITEM's ref id, not
the COS character's.
================
*/
func EncodeCosSummonTimer3691(itemRefObjID, remainingSec, packedExtra uint32) []byte {
	return NewWriter(13).
		U8(CosStateRefreshSummonTimer).
		U32(itemRefObjID).
		U32(remainingSec).
		U32(packedExtra).
		Payload()
}

/*
================
EncodeCosSummonTimerRetire3691

EncodeCosSummonTimerRetire3691 is the zero pair
CIFMagicStateBoard_AddOrRemoveSlot treats as its remove selector. It
retires the board row without expiring its window.
================
*/
func EncodeCosSummonTimerRetire3691(itemRefObjID uint32) []byte {
	return EncodeCosSummonTimer3691(itemRefObjID, 0, 0)
}

// CosCommandMountTag is the mount arm's trailing tag byte (the u8 0x0B
// the client's CGInterface_ExecuteActionCommand 0x1388 arm appends
// after the gid).
const CosCommandMountTag uint8 = 0x0B

// CosCommandPickupTag is emitted by the client's
// DropItemManager_TickAutoPickupRequests (client 0x77E1A7).
const CosCommandPickupTag uint8 = 0x08

// CosCommandFollowTag is the command-class pet follow request (client
// 6A2777 in CICCos_ExecuteActionCommand).
const CosCommandFollowTag uint8 = 0x09

// CosCommandAttackTag is the mounted basic-attack selector of client
// CGInterface_InteractCharacterTarget.
const CosCommandAttackTag uint8 = 0x02

// client CNavigationDeadreckon_SendTargetMovePacket ->
// CNavigationDeadreckon_WriteMovementPayloadToStream: COS gid, tag 1,
// ordinary native move body.
const CosCommandMovementTag uint8 = 0x01

// CosCommandStopTag is the vehicle's direction stop (client 0x8777B0),
// the twin of
// 0x72F5: [u32 gid][u8 0x03][u16 heading].
const CosCommandStopTag uint8 = 0x03

// CosCommandSteerTag is the vehicle's steer (client 0x877540), the twin
// of 0x72CF:
// [u32 gid][u8 0x04][u16 heading].
const CosCommandSteerTag uint8 = 0x04

// CosHeadingRequestSize is the stop/steer body size: gid + tag + heading.
const CosHeadingRequestSize = 7

// CosCommandRequestSize is the mount arm's exact body size: u32 gid + u8 tag.
const CosCommandRequestSize = 5

// CosAttackRequestSize is mount gid + tag + target gid.
const CosAttackRequestSize = 9

/*
================
CosCommand

CosCommand is the strictly decoded subset of the 0x769E tagged family
for which v1.150 client bytes are proven.
================
*/
type CosCommand struct {
	Tag       uint8
	CosGid    uint32
	TargetGid uint32
	Movement  []byte
	// Heading is the stop/steer arms' heading word.
	Heading uint16
}

/*
================
DecodeCosCommand

DecodeCosCommand parses the decoded 0x769E forms without treating the
family as one fixed-size packet. Length is selected by the in-band tag.
================
*/
func DecodeCosCommand(payload []byte) (CosCommand, error) {
	var out CosCommand
	if len(payload) < CosCommandRequestSize {
		return out, fmt.Errorf("wire: 0x769E body %d bytes, need gid+tag", len(payload))
	}
	r := NewReader(payload)
	gid, err := r.U32()
	if err != nil {
		return out, err
	}
	tag, err := r.U8()
	if err != nil {
		return out, err
	}
	out.CosGid, out.Tag = gid, tag
	switch tag {
	case CosCommandMovementTag:
		body := payload[5:]
		if len(body) == 0 || body[0] == 1 && len(body) != 9 || body[0] == 0 && (len(body) != 4 || body[1] != 1) || body[0] > 1 {
			return CosCommand{}, fmt.Errorf("wire: malformed COS movement")
		}
		out.Movement = append([]byte(nil), body...)
		return out, nil
	case CosCommandStopTag, CosCommandSteerTag:
		if len(payload) != CosHeadingRequestSize {
			return CosCommand{}, fmt.Errorf("wire: 0x769E heading body %d bytes, want %d", len(payload), CosHeadingRequestSize)
		}
		heading, readErr := r.U16()
		if readErr != nil {
			return CosCommand{}, readErr
		}
		out.Heading = heading
	case CosCommandMountTag, CosCommandFollowTag:
		if len(payload) != CosCommandRequestSize {
			return CosCommand{}, fmt.Errorf("wire: 0x769E mount body %d bytes, want %d", len(payload), CosCommandRequestSize)
		}
	case CosCommandAttackTag, CosCommandPickupTag:
		if len(payload) != CosAttackRequestSize {
			return CosCommand{}, fmt.Errorf("wire: 0x769E attack body %d bytes, want %d", len(payload), CosAttackRequestSize)
		}
		targetGid, readErr := r.U32()
		if readErr != nil {
			return CosCommand{}, readErr
		}
		out.TargetGid = targetGid
	default:
		return CosCommand{}, fmt.Errorf("wire: unsupported COS command tag 0x%02X", tag)
	}
	if err := r.Done(); err != nil {
		return CosCommand{}, err
	}
	return out, nil
}

/*
================
CosMountRequest

CosMountRequest is one decoded 0x769E mount request.
================
*/
type CosMountRequest struct {
	// CosGid is the COS entity gid the client claims to mount (the active
	// COS record's +0x04 gid on a retail client - claimed, never trusted).
	CosGid uint32
}

/*
================
DecodeCosMountRequest

DecodeCosMountRequest parses a 0x769E body, accepting exactly the pinned
mount form: [u32le cosGid][u8 0x0B], 5 bytes. Any other length is
malformed; a well-formed-length body with a different tag refuses with a
DISTINCT reason - those forms exist natively (see the family list above)
but their bytes are not pinned here, and half-parsing them would invent a
contract.
================
*/
func DecodeCosMountRequest(payload []byte) (CosMountRequest, error) {
	var out CosMountRequest
	command, err := DecodeCosCommand(payload)
	if err != nil {
		return out, err
	}
	if command.Tag != CosCommandMountTag {
		return out, fmt.Errorf("wire: 0x769E tag 0x%02X is not mount", command.Tag)
	}
	out.CosGid = command.CosGid
	return out, nil
}

// cosRideStateApplyLen is the mode-1 B4B5 body: [1][rider u32][flag][mount u32].
const cosRideStateApplyLen = 10

/*
================
EncodeCosRideState

EncodeCosRideState builds the mode-1 B4B5 body of client
CPSMission_OnCOSRideStateB4B5.
================
*/
func EncodeCosRideState(riderGid uint32, mounted bool, vehicleGid uint32) []byte {
	rideState := uint8(0)
	if mounted {
		rideState = 1
	}
	return NewWriter(cosRideStateApplyLen).U8(1).U32(riderGid).U8(rideState).U32(vehicleGid).Payload()
}

/*
================
CosRideStateRider

The rider a mode-1 B4B5 body names; false for a refusal ([2][code]) or a
malformed body.
================
*/
func CosRideStateRider(payload []byte) (uint32, bool) {
	if len(payload) != cosRideStateApplyLen || payload[0] != 1 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(payload[1:]), true
}

/*
================
EncodeCosRecordCreateBand2

EncodeCosRecordCreateBand2 builds the 0x3158 record for a TID-band-2
transport: [gid][ref][hp][mp][status][dead]. The conditional grammar is
selected from the already-seeded characterdata TID word client-side.
================
*/
func EncodeCosRecordCreateBand2(gid, refObjID, hp, mp uint32, status uint8, dead bool) []byte {
	deadWord := uint32(0)
	if dead {
		deadWord = 1
	}
	return NewWriter(21).
		U32(gid).
		U32(refObjID).
		U32(hp).
		U32(mp).
		U8(status).
		U32(deadWord).
		Payload()
}

// CosBandCapturedMonster is the captured quest monster's COS band.
const CosBandCapturedMonster = 6

/*
================
CosSpawnBand2

CosSpawnBand2 is the plain internal/transport/pet create row of client
CICCos_DeserializeSpawnPacket.
================
*/
type CosSpawnBand2 struct {
	// Zero preserves band-2 callers; band 1 omits ownership, 3/4 add pet names.
	Band      uint8
	RefObjID  uint32
	Gid       uint32
	Position  Position
	Walk      float32
	Run       float32
	Scale     float32
	Name      string
	OwnerName string
	// Guild soldiers copy equipment from this player model (client 8554E0).
	OwnerModelRef uint32
	OwnerGid      uint32
	HoldType      uint8
	PvpState      uint8
	BodyStatus    uint8
	State         uint8
}

/*
================
EncodeCosSpawnBand2

EncodeCosSpawnBand2 builds the exact 0x30D7 single-spawn body proven by
cicCosSpawnParity: shared position/movement/scalar block, band-2 name info,
owner gid, then vt+0x68 state byte.
================
*/
func EncodeCosSpawnBand2(row CosSpawnBand2) []byte {
	name := []byte(row.Name)
	ownerName := []byte(row.OwnerName)
	if len(name) > 0xffff {
		name = name[:0xffff]
	}
	if len(ownerName) > 0xffff {
		ownerName = ownerName[:0xffff]
	}
	w := NewWriter(80 + len(name) + len(ownerName)).
		U32(row.RefObjID).
		U32(row.Gid).
		U16(row.Position.RegionID).
		F32(row.Position.X).
		F32(row.Position.Y).
		F32(row.Position.Z).
		U16(row.Position.Heading).
		U8(0).
		U8(1).
		U8(0).
		U16(row.Position.Heading).
		U8(0).
		U8(0).
		U8(row.BodyStatus).
		F32(row.Walk).
		F32(row.Run).
		F32(row.Scale).
		U8(0).
		U8(1).
		U16(uint16(len(name))).
		Bytes(name)
	// Client 854FA0 and 8554E0 omit both the owner-name tail and owner GID for
	// ordinary riding horses. The private COS record binds their owner.
	if row.Band == 1 {
		return w.U8(row.State).Payload()
	}
	if row.Band == 3 || row.Band == 4 {
		w.U16(uint16(len(name))).Bytes(name)
	}
	w.U16(uint16(len(ownerName))).Bytes(ownerName)
	// Client 854FA0: a captured quest monster (band 6) carries neither the hold
	// nor the PvP byte; a pickup pet (4) only the hold byte.
	if row.Band != CosBandCapturedMonster {
		w.U8(row.HoldType)
		if row.Band != 4 {
			w.U8(row.PvpState)
		}
	}
	if row.Band == 5 {
		w.U32(row.OwnerModelRef)
	}
	return w.U32(row.OwnerGid).U8(row.State).Payload()
}
