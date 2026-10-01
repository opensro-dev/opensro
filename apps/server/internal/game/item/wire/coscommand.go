/*
===========================================================================

coscommand.go - the 0x769E COS command family and its S->C twins

The strict codec for what a client sends about its summoned COS (movement,
mount, mounted attack) and the frames the action lane answers with. The
comment on the opcode block below carries the native provenance.

===========================================================================
*/
package wire

import "fmt"

// COS (internal/transport/pet) command plane, C->S opcode 0x769E.
//
// Two client producers are binary-pinned:
// sub_695420 / CGInterface_ExecuteActionCommand, command id 0x1388
// (@0x695f6b..0x69605d) emits exactly [u32le cosGid][u8 0x0B] after its own
// gates pass (local player present, gid is the ACTIVE COS at global-data
// header+0x10, the 82d310 command-class gate, and the COS character's +0x644
// bit 13 CLEAR - i.e. not already mounted); and sub_692cb0's mounted
// follow/attack branch emits [u32 mountGid][u8 0x02][u32 targetGid] after
// resolving a nonzero effective-record +0x210.
//
// 0x769E is a multi-form FAMILY keyed on the trailing tag byte (C3 verify,
// server-wave seq 38): 0x01 movement, 0x02 approach, 0x03 direction stop,
// 0x04 steer, 0x08 pickup, 0x0B mount. The mount's movement trio is what
// CNavigationDeadreckon sends for a vehicle instead of the player opcodes:
// SendTargetMovePacket (0x877D80) tag 0x01 with the 0x7738 body,
// SendAngleUpdatePacket (0x8777B0) tag 0x03 [u16 heading] instead of 0x72F5,
// and SendSteeringUpdate (0x877540) tag 0x04 [u16 heading] instead of 0x72CF.
// Those, the mount and the mounted attack are decoded here; every other tag
// refuses LOUDLY as unsupported rather than being half-parsed (the
// moverequest.go unsupported-mode precedent).
//
// S->C twins (emitted by action.HandleCosCommand after authoritative
// ActiveCOS identity/capability gates):
//   - 0xB4B5 ride-state apply (sub_777f60, LIVE in the browser bridge):
//     mode 1 = [u8 1][u32 riderGid][u8 rideState][u32 vehicleGid],
//     rideState 1 mounts / 0 dismounts. This - not 0xB69E - is what makes a
//     client actually mount.
//   - 0xB69E COS command-result feedback (sub_74fcd0, LIVE):
//     [u8 subType][u8 selector]([u8 resultByte] iff subType==2)[u32 cosGid],
//     selector 8 appends [u32 dropItemGid]. The refusal bytes retail sends
//     for a mount the client itself could never compose are UNPINNED, so
//     refusals stay off the wire entirely.
const (
	// OpCosCommandRequest is the client's COS command family (0x769E).
	OpCosCommandRequest uint16 = 0x769E
	// OpCosRideState is the S->C ride-state apply (0xB4B5, sub_777f60).
	OpCosRideState uint16 = 0xB4B5
	// OpCosRecordCreate is the S->C COS record seed consumed by sub_830ec0.
	OpCosRecordCreate uint16 = 0x3158
	// OpCosCommandResult is the S->C command-result feedback (0xB69E,
	// sub_74fcd0).
	OpCosCommandResult uint16 = 0xB69E
	// OpCosStateRefresh is the S->C COS buff/state refresh (0x3691,
	// sub_775f20, registrar row 74: opcode write @0x0074dcb2). One subtype
	// byte selects the wire shape; only subtype 3 is emitted here.
	OpCosStateRefresh uint16 = 0x3691
)

// CosStateRefreshSummonTimer is the subtype byte sub_775f20 routes to the
// kind-3 list-control command (sub_67A470 -> sub_6E6E00), which builds the
// magic-state board's two-bar COS slot.
const CosStateRefreshSummonTimer uint8 = 3

/*
================
EncodeCosSummonTimer3691

EncodeCosSummonTimer3691 builds the 0x3691 subtype-3 body:

	[u8 3][u32 itemRefObjID][u32 remainingSec][u32 packedExtra]

sub_775f20 @0x7760FE reads the three u32s and dispatches
sub_67A470(gi, 3, id, 0, 0, 0) when both trailing words are zero, otherwise
sub_67A470(gi, 3, id, 1, remainingSec, packedExtra). That flag reaches
sub_6E6150 as its submit selector: 0 REMOVES the matching row, 1 upserts
it, so a zero pair is the native retirement rather than an empty window.

sub_6E6E00 kind 3 resolves the ITEM record through sub_7EFE70 and seeds the
row from its +0x29C duration: the limit is duration*1000 and the elapsed
accumulator is limit - remainingSec*1000, which sub_6E6AA0 then draws as
(limit - elapsed) / limit. The third word is what is LEFT, never what has
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

EncodeCosSummonTimerRetire3691 is the zero pair sub_6E6150 treats as its
remove selector. It retires the board row without expiring its window.
================
*/
func EncodeCosSummonTimerRetire3691(itemRefObjID uint32) []byte {
	return EncodeCosSummonTimer3691(itemRefObjID, 0, 0)
}

// CosCommandMountTag is the mount arm's trailing tag byte (the u8 0x0B the
// sub_695420 0x1388 arm appends after the gid).
const CosCommandMountTag uint8 = 0x0B

// CosCommandAttackTag is sub_692cb0's mounted basic-attack selector.
const CosCommandAttackTag uint8 = 0x02

// sub_877d80 -> sub_877cc0: COS gid, tag 1, ordinary native move body.
const CosCommandMovementTag uint8 = 0x01

// CosCommandStopTag is the vehicle's direction stop (0x8777B0), the twin of
// 0x72F5: [u32 gid][u8 0x03][u16 heading].
const CosCommandStopTag uint8 = 0x03

// CosCommandSteerTag is the vehicle's steer (0x877540), the twin of 0x72CF:
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
	case CosCommandMountTag:
		if len(payload) != CosCommandRequestSize {
			return CosCommand{}, fmt.Errorf("wire: 0x769E mount body %d bytes, want %d", len(payload), CosCommandRequestSize)
		}
	case CosCommandAttackTag:
		if len(payload) != CosAttackRequestSize {
			return CosCommand{}, fmt.Errorf("wire: 0x769E attack body %d bytes, want %d", len(payload), CosAttackRequestSize)
		}
		targetGid, readErr := r.U32()
		if readErr != nil {
			return CosCommand{}, readErr
		}
		out.TargetGid = targetGid
	default:
		return CosCommand{}, fmt.Errorf("wire: 0x769E tag 0x%02X unsupported (decoded forms: 0x01 move, 0x02 attack, 0x03 stop, 0x04 steer, 0x0B mount)", tag)
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

/*
================
EncodeCosRideState

EncodeCosRideState builds sub_777f60's mode-1 B4B5 body.
================
*/
func EncodeCosRideState(riderGid uint32, mounted bool, vehicleGid uint32) []byte {
	rideState := uint8(0)
	if mounted {
		rideState = 1
	}
	return NewWriter(10).U8(1).U32(riderGid).U8(rideState).U32(vehicleGid).Payload()
}

/*
================
CosSpawnBand2

CosSpawnBand2 is sub_8554e0's plain internal/transport/pet create row.
================
*/
type CosSpawnBand2 struct {
	// Zero preserves band-2 callers; 1 drops the owner block; 3/4 select
	// the verified pet name tail.
	Band       uint8
	RefObjID   uint32
	Gid        uint32
	Position   Position
	Walk       float32
	Run        float32
	Scale      float32
	Name       string
	OwnerName  string
	OwnerGid   uint32
	PvpState   uint8
	BodyStatus uint8
	State      uint8
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
	// A riding horse (band 1) carries no owner block and no owner gid
	// (sub_8554e0 reads them for bands 2-6 only).
	if row.Band == 1 {
		return w.U8(row.State).Payload()
	}
	if row.Band == 3 || row.Band == 4 {
		w.U16(uint16(len(name))).Bytes(name)
	}
	w.U16(uint16(len(ownerName))).Bytes(ownerName).U8(0)
	if row.Band != 4 {
		w.U8(row.PvpState)
	}
	return w.U32(row.OwnerGid).U8(row.State).Payload()
}
