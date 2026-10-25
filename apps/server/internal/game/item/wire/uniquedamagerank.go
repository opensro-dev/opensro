/*
===========================================================================

uniquedamagerank.go - the unique kill damage ranking (port-only, not native)

When a unique dies, the operator can show everyone who damaged it the
players who did the most damage. 0x3FBC is a port-only opcode: no v1.150
client or v1.188 server uses it, and only this port's browser client
consumes it. A row's rank is its place in the frame; a party rank of 0
means the player was in no party.

Body, little-endian:

	u32 refObjID
	u64 totalDamage
	u8  count
	count x { u8 nameLength, name bytes, u32 damage, u8 partyRank }
	u16 selfRank (0 = not ranked)
	u32 selfDamage
	u8  selfPartyRank

===========================================================================
*/

package wire

import "encoding/binary"

// OpUniqueDamageRank is the port-only S->C unique kill damage ranking.
const OpUniqueDamageRank uint16 = 0x3FBC

// UniqueDamageRankMaxRows bounds the ranked rows one frame carries.
const UniqueDamageRankMaxRows = 12

// uniqueDamageRankMaxName bounds a name's bytes (a u8 length).
const uniqueDamageRankMaxName = 64

// UniqueDamageRow is one ranked player.
type UniqueDamageRow struct {
	Name      string
	Damage    uint32
	PartyRank uint8
}

// UniqueDamageRank is one recipient's view of a unique kill: the shared
// top rows and the recipient's own place.
type UniqueDamageRank struct {
	RefObjID      uint32
	Total         uint64
	Rows          []UniqueDamageRow
	SelfRank      uint16
	SelfDamage    uint32
	SelfPartyRank uint8
}

/*
================
EncodeUniqueDamageRank
================
*/
func EncodeUniqueDamageRank(r UniqueDamageRank) []byte {
	rows := r.Rows
	if len(rows) > UniqueDamageRankMaxRows {
		rows = rows[:UniqueDamageRankMaxRows]
	}
	out := binary.LittleEndian.AppendUint32(nil, r.RefObjID)
	out = binary.LittleEndian.AppendUint64(out, r.Total)
	out = append(out, uint8(len(rows)))
	for _, row := range rows {
		name := row.Name
		if len(name) > uniqueDamageRankMaxName {
			name = name[:uniqueDamageRankMaxName]
		}
		out = append(out, uint8(len(name)))
		out = append(out, name...)
		out = binary.LittleEndian.AppendUint32(out, row.Damage)
		out = append(out, row.PartyRank)
	}
	out = binary.LittleEndian.AppendUint16(out, r.SelfRank)
	out = binary.LittleEndian.AppendUint32(out, r.SelfDamage)
	return append(out, r.SelfPartyRank)
}
