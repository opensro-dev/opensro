/*
===========================================================================

union_wire.go - the guild union frames

Requests (CIFAllianceGuild 5F7860 / 5F5690): 0x7379 [u32 target gid]
invites the selected player's guild (NetClient_SendUnionInvite7379),
0x7795 leaves (no body), 0x7680 [u32 guild id] expels. Each answers
[u8 1] or [u8 2][u8 code] on 0xB379 / 0xB795 / 0xB680, the code a
category 0x10 notice (75CCA0, 75CCF0, 75CD40): the low byte of the
GameServer's 0x4Cxx guild error.

The union list 0x341E (82A560) is [u32 union id][u32 union emblem][u32
leading guild id][u8 count] and a row per guild (CAlliance_WriteList
5B8EB0). A row is CGuild vt+0x18 (5D0BC0): [u32 id], then by mask 1 the
name, 2 the level, 4 the master's name and model, 8 the member count.
0x3B29 subop 0x0D adds a full row (5C44F0), 0x0E patches one [u8 mask]
[row] (761F62), 0x12 removes [u8 1 left | 2 expelled][u32 guild] or
dissolves [u8 3] (5C45B0, 5C4650).

===========================================================================
*/
package guild

import (
	"errors"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

const (
	OpUnionInvite       uint16 = 0x7379
	OpUnionInviteResult uint16 = 0xB379
	OpUnionLeave        uint16 = 0x7795
	OpUnionLeaveResult  uint16 = 0xB795
	OpUnionKick         uint16 = 0x7680
	OpUnionKickResult   uint16 = 0xB680
	OpUnionList         uint16 = 0x341E

	// unionPromptType is the 0x3393 kind of a union proposal (7644E0
	// case 6 opens confirm box 0x1D with the inviter's guild name).
	unionPromptType uint8 = 6

	unionRowName    uint8 = 1
	unionRowLevel   uint8 = 2
	unionRowMaster  uint8 = 4
	unionRowMembers uint8 = 8
	unionRowAll           = unionRowName | unionRowLevel | unionRowMaster | unionRowMembers

	guildUpdateAllyJoined  uint8 = 0x0D
	guildUpdateAllyPatch   uint8 = 0x0E
	guildUpdateAllyRemoved uint8 = 0x12

	UnionRemovedLeft      uint8 = 1
	UnionRemovedExpelled  uint8 = 2
	UnionRemovedDissolved uint8 = 3
)

// Category 0x10 codes the union requests answer with (the 0x4Cxx low byte
// GuildManager_RequestUnionInvite 5C6550, _RequestUnionLeave 5C6710 and
// _RequestUnionKick 5C67A0 return).
const (
	unionErrTargetInvalid    uint8 = 0x03
	unionErrNoGuild          uint8 = 0x0D
	unionErrPermission       uint8 = 0x1E
	unionErrTargetNotMaster  uint8 = 0x24
	unionErrTargetNoGuild    uint8 = 0x25
	unionErrOwnGuild         uint8 = 0x26
	unionErrTargetHasUnion   uint8 = 0x28
	unionErrFull             uint8 = 0x29
	unionErrLevelTooLow      uint8 = 0x2A
	unionErrTargetLevelLow   uint8 = 0x2B
	unionErrNotInUnion       uint8 = 0x2F
	unionErrNotAlly          uint8 = 0x30
	unionErrInviteDuringWar  uint8 = 0x75
	unionErrLeaveDuringWar   uint8 = 0x76
	unionErrKickDuringWar    uint8 = 0x77
	unionResultOK            uint8 = 1
	unionResultRefused       uint8 = 2
	unionMinimumGuildLevel   uint8 = 2
	unionGuildRowMemberLimit       = 0xFF
)

/*
================
UnionGuildRow

What a union row says about one guild.
================
*/
type UnionGuildRow struct {
	GuildID     int64
	Name        string
	Level       uint8
	MasterName  string
	MasterModel uint32
	Members     int
}

/*
================
unionGuildRow

The row of a stored guild: its leader (grade 0) is the master.
================
*/
func unionGuildRow(guild domain.GuildRecord, members []domain.GuildMemberRecord) UnionGuildRow {
	row := UnionGuildRow{GuildID: guild.ID, Name: guild.Name, Level: guild.Level, Members: len(members)}
	for _, member := range members {
		if member.Grade == 0 {
			row.MasterName, row.MasterModel = member.Name, member.RefObjID
			break
		}
	}
	return row
}

/*
================
writeUnionRow
================
*/
func writeUnionRow(w *wire.Writer, row UnionGuildRow, mask uint8) {
	w.U32(uint32(row.GuildID))
	if mask&unionRowName != 0 {
		writeGuildString(w, row.Name)
	}
	if mask&unionRowLevel != 0 {
		w.U8(row.Level)
	}
	if mask&unionRowMaster != 0 {
		writeGuildString(w, row.MasterName)
		w.U32(row.MasterModel)
	}
	if mask&unionRowMembers != 0 {
		w.U8(uint8(min(row.Members, unionGuildRowMemberLimit)))
	}
}

/*
================
EncodeUnionList341E
================
*/
func EncodeUnionList341E(record domain.AllianceRecord, rows []UnionGuildRow) []byte {
	w := wire.NewWriter(16 + 48*len(rows))
	w.U32(uint32(record.AllianceID))
	w.U32(record.Crest)
	w.U32(uint32(record.Guilds[0]))
	w.U8(uint8(len(rows)))
	for _, row := range rows {
		writeUnionRow(w, row, unionRowAll)
	}
	return w.Payload()
}

/*
================
EncodeAllyJoined3B29
================
*/
func EncodeAllyJoined3B29(row UnionGuildRow) []byte {
	w := wire.NewWriter(48)
	w.U8(guildUpdateAllyJoined)
	writeUnionRow(w, row, unionRowAll)
	return w.Payload()
}

/*
================
EncodeAllyPatch3B29
================
*/
func EncodeAllyPatch3B29(row UnionGuildRow, mask uint8) []byte {
	w := wire.NewWriter(48)
	w.U8(guildUpdateAllyPatch)
	w.U8(mask)
	writeUnionRow(w, row, mask)
	return w.Payload()
}

/*
================
EncodeAllyRemoved3B29

A dissolution carries no guild.
================
*/
func EncodeAllyRemoved3B29(mode uint8, guildID int64) []byte {
	if mode == UnionRemovedDissolved {
		return []byte{guildUpdateAllyRemoved, mode}
	}
	return wire.NewWriter(6).U8(guildUpdateAllyRemoved).U8(mode).U32(uint32(guildID)).Payload()
}

/*
================
encodeUnionResult
================
*/
func encodeUnionResult(code uint8) []byte {
	if code == 0 {
		return []byte{unionResultOK}
	}
	return []byte{unionResultRefused, code}
}

var errUnionBody = errors.New("guild: malformed union request")

/*
================
decodeUnionU32

The invite and expel bodies: one u32.
================
*/
func decodeUnionU32(payload []byte) (uint32, error) {
	r := wire.NewReader(payload)
	value, err := r.U32()
	if err != nil || r.Done() != nil {
		return 0, errUnionBody
	}
	return value, nil
}

/*
================
EncodeUnionPrompt3393
================
*/
func EncodeUnionPrompt3393(inviterRef uint32) []byte {
	return wire.NewWriter(5).U8(unionPromptType).U32(inviterRef).Payload()
}
