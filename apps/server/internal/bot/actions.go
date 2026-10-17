/*
===========================================================================

actions.go - the bot's client action payloads

The bot builds the same client bodies a retail client sends: the 0x7738
ground-click move request and the 0x72CD basic attack. The server validates
both through the ordinary navigation and action lanes.

Phase 1 of issue #74 (issue #75).

===========================================================================
*/
package bot

import (
	"encoding/binary"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// EncodeMovementTo builds the 9-byte positional 0x7738 body:
// [u8 1][u16 region][i16 x][i16 y][i16 z]. The server navigation validates
// the destination and answers with a 0xB738 ack or a refusal.
func EncodeMovementTo(region uint16, x, y, z int16) []byte {
	body := make([]byte, simulation.ClientMovementRequestSize)
	body[0] = simulation.MovementAckDestinationMode
	binary.LittleEndian.PutUint16(body[1:3], region)
	binary.LittleEndian.PutUint16(body[3:5], uint16(x))
	binary.LittleEndian.PutUint16(body[5:7], uint16(y))
	binary.LittleEndian.PutUint16(body[7:9], uint16(z))
	return body
}

// EncodeBasicAttack builds the 0x72CD basic-attack body for one target gid.
func EncodeBasicAttack(targetGid uint32) []byte {
	return wire.BasicAttackEngage{TargetGid: targetGid}.Encode()
}

// EncodePickup builds the 0x72CD ground-item pickup body [01 02 01 gid] for
// one ground entity gid. The server owns the travel: an out-of-range request
// arms the approach walk and grants the drop on arrival.
func EncodePickup(groundGid uint32) []byte {
	return wire.TargetInteract{Gid: groundGid}.Encode()
}

// OpObjectSelectRequest is the 0x745A object-select request (issue #171).
const OpObjectSelectRequest uint16 = wire.OpObjectSelectRequest

// PortalOpcode is the 0x7495 gate-travel request (issue #171).
const PortalOpcode uint16 = 0x7495

// PortalKindTravel is the 0x7495 type of a directed gate travel (issue #171).
const PortalKindTravel uint8 = 2

// EncodeObjectSelect builds the 0x745A body [u32le gid]. The server records
// the selection the portal request then consumes (issue #171).
func EncodeObjectSelect(gid uint32) []byte {
	body := make([]byte, 4)
	binary.LittleEndian.PutUint32(body, gid)
	return body
}

// EncodePortal builds the 0x7495 gate-travel body
// [u32le gid][u8 kind=2][u32le destination]. The server requires the gate NPC
// selected, resident and within reach, and the (source, target) pair linked
// (issue #171).
func EncodePortal(gid, destinationID uint32) []byte {
	body := make([]byte, 9)
	binary.LittleEndian.PutUint32(body[0:4], gid)
	body[4] = PortalKindTravel
	binary.LittleEndian.PutUint32(body[5:9], destinationID)
	return body
}

// MovementOpcode is the 0x7738 client movement request.
const MovementOpcode = simulation.OpClientMovementRequest

// TargetInteractOpcode is the 0x72CD target action.
const TargetInteractOpcode = wire.OpTargetInteract

// OpcodeSkillCastResult is the 0xB245 cast bracket open. A refused attack is
// the two-byte body [0x02][error code].
const OpcodeSkillCastResult uint16 = wire.OpSkillCastResult

// OpcodeItemMoveResponse is the 0xB06D item-operation result. A refused pickup
// is the two-byte body [0x02][error code].
const OpcodeItemMoveResponse uint16 = wire.OpItemMoveResponse

// refusalResult is the native result byte of a refused frame body.
const refusalResult uint8 = 0x02

// RefusalCode returns the native error code of a refusal body [0x02][code].
// A success body or a malformed body reports false.
func RefusalCode(payload []byte) (uint8, bool) {
	if len(payload) == 2 && payload[0] == refusalResult {
		return payload[1], true
	}
	return 0, false
}

// skillCastSuccessBytes is the fixed length of the 0xB245 success prefix:
// [ok=0x01][btResult u8][skillId u32][casterGid u32][instanceToken u32]
// [ownerOrTargetGid u32] (wire.SkillCastSuccess.writePrefix). A shorter body
// is a refusal or a malformed frame, so it carries no attacker.
const skillCastSuccessBytes = 18

// skillCastSuccessOK is the leading "cast accepted" byte of the 0xB245 prefix.
const skillCastSuccessOK uint8 = 0x01

// AttackerFromSkillCast returns the monster gid that attacks selfGID, when the
// 0xB245 payload is a cast success from a monster and names selfGID as the
// target. A refused attack (the two-byte [0x02][code] body), a player or NPC
// caster, and a cast that targets another character report false (issue #185).
func AttackerFromSkillCast(payload []byte, selfGID uint32) (uint32, bool) {
	if selfGID == 0 || len(payload) < skillCastSuccessBytes || payload[0] != skillCastSuccessOK {
		return 0, false
	}
	caster := binary.LittleEndian.Uint32(payload[6:10])
	target := binary.LittleEndian.Uint32(payload[14:18])
	if caster < domain.MonsterGIDBase || target != selfGID {
		return 0, false
	}
	return caster, true
}
