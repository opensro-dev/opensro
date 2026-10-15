/*
===========================================================================

reward.go - authoritative kill and loot events

The bot telemetry counts scene-visibility despawns (0x36AB) as kill and loot
signals. A despawn is not proof: a target can leave view or die to another
player, and a drop can be granted to another player. Two ordinary packets
carry the authoritative proof (issue #175):

  - the 0x30D2 experience update, which the server sends only to the
    character that earned the experience. A monster entity id (400000 and
    above) with a positive delta is a kill by this bot.
  - the 0xB06D item-operation result with result 1 and move type 6, which the
    server sends to the character that received a pickup grant.

===========================================================================
*/
package bot

import (
	"encoding/binary"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// OpcodeExpUpdate is the 0x30D2 experience and skill-experience delta channel.
const OpcodeExpUpdate uint16 = wire.OpExpUpdate

// OpcodePointsUpdate is the 0x30B3 points channel. Type 1 carries the gold
// balance after a pickup, which is the second half of a gold grant.
const OpcodePointsUpdate uint16 = wire.OpPointsUpdate

// expUpdateFixedSize is the 0x30D2 body length without the level-up tail:
// [u32 sourceGid][s32 expDelta][s32 skillExpDelta][u8 flags].
const expUpdateFixedSize = 13

// expUpdateLevelledSize is the 0x30D2 body length with the level-up tail:
// the fixed body plus [u16 statPoints].
const expUpdateLevelledSize = 15

// ExpUpdate is a decoded 0x30D2 experience update.
type ExpUpdate struct {
	// SourceGID is the entity that granted the experience. The server sends
	// the killed monster's gid for a monster kill, 0 for a quest or admin
	// grant and the player's gid for a player kill.
	SourceGID uint32
	// ExpDelta is the signed experience change. The death penalty is negative.
	ExpDelta int32
	// SkillExpDelta is the signed skill-experience change.
	SkillExpDelta int32
	// Flags is the wire flag byte. This server always sends 0.
	Flags uint8
	// Levelled is true when the body carries the stat-point tail, which the
	// server adds only when the grant crossed one or more levels.
	Levelled bool
	// StatPoints is the absolute stat-point pool after a level-up. It is only
	// meaningful when Levelled is true.
	StatPoints uint16
}

// ParseExpUpdate parses a 0x30D2 body. A body shorter than the fixed 13 bytes
// is rejected. The level-up tail is read when the body is 15 bytes or longer.
func ParseExpUpdate(payload []byte) (ExpUpdate, bool) {
	if len(payload) < expUpdateFixedSize {
		return ExpUpdate{}, false
	}
	update := ExpUpdate{
		SourceGID:     binary.LittleEndian.Uint32(payload[0:4]),
		ExpDelta:      int32(binary.LittleEndian.Uint32(payload[4:8])),
		SkillExpDelta: int32(binary.LittleEndian.Uint32(payload[8:12])),
		Flags:         payload[12],
	}
	if len(payload) >= expUpdateLevelledSize {
		update.Levelled = true
		update.StatPoints = binary.LittleEndian.Uint16(payload[13:15])
	}
	return update, true
}

// IsMonsterKill reports whether the update is the authoritative kill proof of
// this bot: the granting entity is a monster and the experience rose. A player
// killer has a player entity id, a quest or admin grant has source 0, and the
// death penalty has a negative delta.
func (u ExpUpdate) IsMonsterKill() bool {
	return u.SourceGID >= domain.MonsterGIDBase && u.ExpDelta > 0
}

// PickupGrant is the fixed prefix of a successful 0xB06D pickup grant.
type PickupGrant struct {
	// Slot is the destination bag slot, or PickupGoldSlot for a gold grant.
	Slot uint8
	// Gold is true when the grant was gold rather than an item.
	Gold bool
	// Amount is the granted gold. It is only meaningful when Gold is true.
	Amount uint32
}

// pickupGrantPrefixSize is the smallest successful pickup body: the result
// byte, the move-type byte and the destination slot.
const pickupGrantPrefixSize = 3

// goldGrantSize is a successful gold pickup body:
// [u8 result 1][u8 move type 6][u8 slot 0xFE][u32 amount].
const goldGrantSize = 7

// ParsePickupGrant parses the fixed prefix of a successful 0xB06D pickup
// grant. It rejects a short body, an error result and every other move type.
// A gold grant carries the u32 amount. An item grant carries a CSOItem body,
// which the telemetry does not read.
func ParsePickupGrant(payload []byte) (PickupGrant, bool) {
	if len(payload) < pickupGrantPrefixSize {
		return PickupGrant{}, false
	}
	if payload[0] != wire.ResultSuccess || payload[1] != wire.MoveTypePickup {
		return PickupGrant{}, false
	}
	grant := PickupGrant{Slot: payload[2]}
	if grant.Slot != wire.PickupGoldSlot {
		return grant, true
	}
	if len(payload) < goldGrantSize {
		return PickupGrant{}, false
	}
	grant.Gold = true
	grant.Amount = binary.LittleEndian.Uint32(payload[3:7])
	return grant, true
}
