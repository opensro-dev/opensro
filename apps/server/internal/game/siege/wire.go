/*
===========================================================================

wire.go - the 0x3887 fortress-war frames the v1.150 client reads

0x3887 (CNetProcessSecond_OnFortressWarState 76C870) is the client half of
SR_GameServer's 0x385F: CSiegeFortressMgr_OnShardMessage (62EE90) relays
each schedule edge with the same subtype byte, and the per-player fortress
list is CGObjPC_SendFortressList (4E04D0 -> 62EBE0 -> 61D540). The subtype
numbering is shared by both versions (0x0C/0x0D applications, 7 NPC
refresh, 8 conquest, 0x10 war guilds all pair up).

Subtypes this file writes:

	0     the fortress list: rows, the period flags, the guild's fortress
	1     war begins in 30 minutes        2    war begins
	3/4/5 war ends in 30/20/10 minutes    9    war ends in 1 minute
	6     war ends
	8     a fortress changed hands (the conquest notice)
	0x0A  the last guard tower fell: the stone's countdown starts
	0x0B  a structure's state changed (destroyed)
	0x10  the guilds registered for the war (clear and replace)
	0x31/0x32  tax period on/off          0x33/0x34  request period on/off

Strings are the sized narrow layout (u16 byte length, the bytes).

===========================================================================
*/
package siege

import (
	"opensro.online/server/internal/game/item/wire"
)

// OpFortressWarState is the S->C fortress-war broadcast (v1.188 0x385F).
const OpFortressWarState uint16 = 0x3887

const (
	SubtypeFortressList      uint8 = 0x00
	SubtypeWarSoon           uint8 = 0x01
	SubtypeWarBegin          uint8 = 0x02
	SubtypeWarEnds30         uint8 = 0x03
	SubtypeWarEnds20         uint8 = 0x04
	SubtypeWarEnds10         uint8 = 0x05
	SubtypeWarEnd            uint8 = 0x06
	SubtypeConquest          uint8 = 0x08
	SubtypeWarEnds1          uint8 = 0x09
	SubtypeTowersFallen      uint8 = 0x0a
	SubtypeStructureState    uint8 = 0x0b
	SubtypeWarGuildRegistry  uint8 = 0x10
	SubtypeTaxPeriodBegin    uint8 = 0x31
	SubtypeTaxPeriodEnd      uint8 = 0x32
	SubtypeRequestPeriodOpen uint8 = 0x33
	SubtypeRequestPeriodEnd  uint8 = 0x34
)

// OpSiegeRelationList is the S->C 0x341E alliance list (client 75AB60 ->
// 82A560), the relation block the fortress status reads for allies.
const OpSiegeRelationList uint16 = 0x341e

/*
================
FortressRow

One fortress of the list, in CSiegeFortress_WriteListRow (61D540) order.
Discarded are four u32s the v1.150 client reads and drops (76C980..); the
server writes the occupying guild id first. CaptureWait is the siege
world's post-capture wait in seconds (sent only while the gates are closed,
slot 41/42); EndCountdown the war-end countdown (sent while it runs, slots
44/45).
================
*/
type FortressRow struct {
	FortressID      uint32
	OwnerName       string
	Discarded       [4]uint32
	HasCaptureWait  bool
	CaptureWait     uint32
	HasEndCountdown bool
	EndCountdown    uint32
}

/*
================
EncodeFortressList3887

Subtype 0: u8 count, the rows, u8 period flags (war 1, request 2, tax 4),
u32 the fortress the player's guild owns or applied to (client guild data
+0x14), zero for none.
================
*/
func EncodeFortressList3887(rows []FortressRow, periods uint8, guildFortressID uint32) []byte {
	writer := wire.NewWriter(7 + len(rows)*32)
	writer.U8(SubtypeFortressList)
	writer.U8(uint8(len(rows)))
	for _, row := range rows {
		writer.U32(row.FortressID)
		writeSizedString(writer, row.OwnerName)
		for _, value := range row.Discarded {
			writer.U32(value)
		}
		if row.HasCaptureWait {
			writer.U8(1).U32(row.CaptureWait)
		} else {
			writer.U8(0)
		}
		if row.HasEndCountdown {
			writer.U8(1).U32(row.EndCountdown)
		} else {
			writer.U8(0)
		}
	}
	writer.U8(periods)
	writer.U32(guildFortressID)
	return writer.Payload()
}

/*
================
EncodeWarGuildRegistry3887

Subtype 0x10: u32 the fortress, u8 count, the guild ids. The client clears
its registry first and skips id 0.
================
*/
func EncodeWarGuildRegistry3887(fortressID uint32, guildIDs []uint32) []byte {
	writer := wire.NewWriter(6 + len(guildIDs)*4)
	writer.U8(SubtypeWarGuildRegistry)
	writer.U32(fortressID)
	writer.U8(uint8(len(guildIDs)))
	for _, id := range guildIDs {
		writer.U32(id)
	}
	return writer.Payload()
}

/*
================
EncodeEdge3887

A schedule edge the client acts on from its subtype alone (1-6, 9 and
0x31-0x34 read nothing further).
================
*/
func EncodeEdge3887(subtype uint8) []byte {
	return []byte{subtype}
}

/*
================
EncodeConquest3887

Subtype 8 (76C870 case 8): u32 the fortress, the holding guild's name and
the row's four discarded u32s, the guild id first as in the list; the
client prints UIIT_MSG_FORT_WAR_CONQUER and refreshes the fortress.
================
*/
func EncodeConquest3887(row FortressRow) []byte {
	writer := wire.NewWriter(23 + len(row.OwnerName))
	writer.U8(SubtypeConquest)
	writer.U32(row.FortressID)
	writeSizedString(writer, row.OwnerName)
	for _, value := range row.Discarded {
		writer.U32(value)
	}
	return writer.Payload()
}

/*
================
EncodeTowersFallen3887

Subtype 0x0A (case 0xA): u32 the fortress. The client shows
UIIT_MSG_FORT_STRUCTURE_STATUS_CANCEL and counts down 0xB4 seconds.
================
*/
func EncodeTowersFallen3887(fortressID uint32) []byte {
	return wire.NewWriter(5).U8(SubtypeTowersFallen).U32(fortressID).Payload()
}

/*
================
StructureState

One structure's state change (CGObjSiegeStruct_BroadcastState385F_0B
4CF9A0): its fortress, object, event zone and state word; a headquarters
also names its guild.
================
*/
type StructureState struct {
	FortressID    uint32
	GID           uint32
	EventStructID uint32
	State         uint16
	Headquarters  bool
	GuildName     string
}

/*
================
EncodeStructureState3887

Subtype 0x0B (case 0xB): u32 fortress, u32 object, u32 event zone, u16
state, and for a headquarters the guild's name.
================
*/
func EncodeStructureState3887(state StructureState) []byte {
	writer := wire.NewWriter(17 + len(state.GuildName))
	writer.U8(SubtypeStructureState)
	writer.U32(state.FortressID).U32(state.GID).U32(state.EventStructID).U16(state.State)
	if state.Headquarters {
		writeSizedString(writer, state.GuildName)
	}
	return writer.Payload()
}

/*
================
AllianceRow

One 0x341E entry in the client's read order (82A560).
================
*/
type AllianceRow struct {
	ID         uint32
	Name       string
	Flag       uint8
	MasterName string
	RefObjID   uint32
	Byte44     uint8
}

/*
================
EncodeSiegeRelationList341E

u32 -> relation block +0x238, u32 -> +0x234, u32 the alliance master guild,
u8 count, then each row { u32 id, name, u8 flag, master name, u32 refObjId,
u8 byte }. The client asserts on a duplicate id.
================
*/
func EncodeSiegeRelationList341E(dword238, dword234, masterGuildID uint32, rows []AllianceRow) []byte {
	writer := wire.NewWriter(13 + len(rows)*16)
	writer.U32(dword238)
	writer.U32(dword234)
	writer.U32(masterGuildID)
	writer.U8(uint8(len(rows)))
	for _, row := range rows {
		writer.U32(row.ID)
		writeSizedString(writer, row.Name)
		writer.U8(row.Flag)
		writeSizedString(writer, row.MasterName)
		writer.U32(row.RefObjID)
		writer.U8(row.Byte44)
	}
	return writer.Payload()
}

/*
================
writeSizedString

The client's sized narrow string: u16 byte length, then the bytes.
================
*/
func writeSizedString(writer *wire.Writer, value string) {
	bytes := []byte(value)
	writer.U16(uint16(len(bytes)))
	writer.Bytes(bytes)
}
