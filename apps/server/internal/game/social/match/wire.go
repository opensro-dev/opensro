// Package match is the MATCH LISTING lane of the v1.150 gateway: the
// party-match and mentor-match boards (register / modify / delete /
// page) with their pinned success acks and listing pages, plus the JOIN
// owner-approval handshake (0x75BF/0x7592 requests, 0x30FA/0x35D5 owner
// answers, 0xB5BF/0xB592 joiner acks - join.go / joinwire.go). Board
// state is IN-MEMORY by design: registrations die
// on process reboot, which is correct and expected. Roster / camp
// mutations an accepted join produces belong to internal/game/social/party and
// internal/game/social/mentor and reach them through wiring.go func-field seams only.
//
// Every opcode number and byte layout here is pinned from the v1.150
// CLIENT, never from v1.188/DuckSoup docs - their party-match numbers
// collide with our item plane (e.g. DuckSoup join 0x706D is our
// item-move). Per-frame fold citations sit on each constant.
//
// Deliberately-silent frontier: the flag==2 error arms (party category 2, mentor
// category 0x1D) - the code->message tables are unpinned vs retail, so
// every register/modify/delete refusal stays SILENT (the war-horn /
// 0x766F posture) instead of inventing error bytes. JOIN refusals are
// the exception: their pinned outer-1 detail arms exist and clear the
// joiner's progress pane, so join.go answers them.
//
// Match SEARCH and SORT are GENUINELY ABSENT from the wire: the window's
// search button runs a LOCAL client filter (no CanSendOpcode
// anywhere on the path) and the sort headers run a LOCAL std::sort
// in the client. There is NO server work for them; 0x75BF /
// 0x7592 are the JOIN opcodes, not "search extras".
package match

import (
	"fmt"
	"unicode/utf16"

	"opensro.online/server/internal/game/item/wire"
)

// C->S opcodes, pinned from the v1.150 client composers (the shared
// body serializers are the client's party and mentor body writers).
const (
	// OpPartyRegisterRequest: client NetClient_Send76FF -> the client's
	// party body writer:
	// {u32 entryId, u32 partyNumber, u8 typeBits, u8 purpose, u8 minLv,
	// u8 maxLv, u16-count wide title}.
	OpPartyRegisterRequest uint16 = 0x76FF
	// OpPartyModifyRequest: client NetClient_Send73DC - the same
	// client party body as register.
	OpPartyModifyRequest uint16 = 0x73DC
	// OpPartyDeleteRequest: client NetClient_Send7535 {u32 entryId}.
	OpPartyDeleteRequest uint16 = 0x7535
	// OpPartyPageRequest: client NetClient_Send7588 {u8 page}.
	OpPartyPageRequest uint16 = 0x7588

	// OpMentorRegisterRequest: client NetClient_Send755D -> the client's
	// mentor body writer:
	OpMentorRegisterRequest uint16 = 0x755D
	// OpMentorModifyRequest: client NetClient_Send713E - the same
	// client mentor body as register.
	OpMentorModifyRequest uint16 = 0x713E
	// OpMentorDeleteRequest: client NetClient_Send770B {u32 entryId}.
	OpMentorDeleteRequest uint16 = 0x770B
	// OpMentorPageRequest: client NetClient_Send7701 {u8 page}.
	OpMentorPageRequest uint16 = 0x7701
)

// S->C opcodes, pinned from the client inbound folds. The party four ride
// the CPSMission packet table (CPSMission_RegisterPacketHandlers);
// the mentor four ride the CNetProcessSecond table
// (CNetProcessSecond_RegisterPacketHandlers) - on this gateway
// both arrive over the one live transport session.
const (
	// OpPartyRegisterAck: client CPSMission_OnPartyMatchingRecordResponse0xB6FF -
	// flag 1 + the registration ack blob (PartyMatchRecord_ReadWire).
	OpPartyRegisterAck uint16 = 0xB6FF
	// OpPartyModifyAck: client CPSMission_OnPartyMatchingModifyResponse0xB3DC -
	// the same blob, MODIFY banner.
	OpPartyModifyAck uint16 = 0xB3DC
	// OpPartyDeleteAck: client CPSMission_OnPartyMatchingDeleteResponse0xB535 -
	// flag 1 + {u32 deletedId}.
	OpPartyDeleteAck uint16 = 0xB535
	// OpPartyListingPage: client CPSMission_OnPartyMatchingListResponse0xB588 -
	// flag 1 + page header + rows.
	OpPartyListingPage uint16 = 0xB588

	// OpMentorRegisterAck: client CNetProcess_OnAcademyMatchingEntryResponse -
	// flag 1 + {u32 id, u32 dword04,
	// u8 kind, wide detail, u32 dword14}.
	OpMentorRegisterAck uint16 = 0xB55D
	// OpMentorModifyAck: client CNetProcess_OnResponse0xB13E - the
	// register blob WITHOUT the
	// trailing u32.
	OpMentorModifyAck uint16 = 0xB13E
	// OpMentorDeleteAck: client CNetProcess_OnResponse0xB70B - flag 1 +
	// {u32 deletedId}.
	OpMentorDeleteAck uint16 = 0xB70B
	// OpMentorListingPage: client CNetProcess_OnResponse0xB701 - flag 1 +
	// page header + rows.
	OpMentorListingPage uint16 = 0xB701
)

// PartyMatchRequest is the shared client party body of 0x76FF (register)
// and 0x73DC (modify). EntryID is the client's own-snapshot state00: 0
// before a registration exists, the live entry id on modify. The four
// bytes land in the snapshot as flag0b/flag0a/field0c/flag0d (the
// PartyMatchManager_SetOwnRecord mapping the re-harness
// matchingWindowListingParity pins).
type PartyMatchRequest struct {
	EntryID     uint32
	PartyNumber uint32
	TypeBits    uint8
	Purpose     uint8
	MinLevel    uint8
	MaxLevel    uint8
	Title       string
}

// DecodePartyMatchRequest strict-decodes the client's party wire body.
func DecodePartyMatchRequest(payload []byte) (PartyMatchRequest, error) {
	reader := wire.NewReader(payload)
	var out PartyMatchRequest
	var err error
	if out.EntryID, err = reader.U32(); err != nil {
		return PartyMatchRequest{}, err
	}
	if out.PartyNumber, err = reader.U32(); err != nil {
		return PartyMatchRequest{}, err
	}
	if out.TypeBits, err = reader.U8(); err != nil {
		return PartyMatchRequest{}, err
	}
	if out.Purpose, err = reader.U8(); err != nil {
		return PartyMatchRequest{}, err
	}
	if out.MinLevel, err = reader.U8(); err != nil {
		return PartyMatchRequest{}, err
	}
	if out.MaxLevel, err = reader.U8(); err != nil {
		return PartyMatchRequest{}, err
	}
	if out.Title, err = readSizedWideString(reader); err != nil {
		return PartyMatchRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return PartyMatchRequest{}, err
	}
	return out, nil
}

// MentorMatchRequest is the shared client mentor body of 0x755D (register)
// and 0x713E (modify).
type MentorMatchRequest struct {
	EntryID uint32
	Dword04 uint32
	Kind    uint8
	Detail  string
}

// DecodeMentorMatchRequest strict-decodes the client's mentor wire body.
func DecodeMentorMatchRequest(payload []byte) (MentorMatchRequest, error) {
	reader := wire.NewReader(payload)
	var out MentorMatchRequest
	var err error
	if out.EntryID, err = reader.U32(); err != nil {
		return MentorMatchRequest{}, err
	}
	if out.Dword04, err = reader.U32(); err != nil {
		return MentorMatchRequest{}, err
	}
	if out.Kind, err = reader.U8(); err != nil {
		return MentorMatchRequest{}, err
	}
	if out.Detail, err = readSizedWideString(reader); err != nil {
		return MentorMatchRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return MentorMatchRequest{}, err
	}
	return out, nil
}

// DecodeDeleteRequest strict-decodes the shared NetClient_Send7535 /
// NetClient_Send770B body {u32 entryId}.
func DecodeDeleteRequest(payload []byte) (uint32, error) {
	reader := wire.NewReader(payload)
	entryID, err := reader.U32()
	if err != nil {
		return 0, err
	}
	if err := reader.Done(); err != nil {
		return 0, err
	}
	return entryID, nil
}

// DecodePageRequest strict-decodes the shared NetClient_Send7588 /
// NetClient_Send7701 body {u8 page}.
func DecodePageRequest(payload []byte) (uint8, error) {
	reader := wire.NewReader(payload)
	page, err := reader.U8()
	if err != nil {
		return 0, err
	}
	if err := reader.Done(); err != nil {
		return 0, err
	}
	return page, nil
}

// EncodePartyRegAck renders the shared 0xB6FF / 0xB3DC success arm: u8
// flag = 1 + the registration ack blob (PartyMatchRecord_ReadWire: two
// u32, four u8 in
// wire order = ack field order +0x08..+0x0b, then the wide title). The
// client maps +0x08->flag0b, +0x09->flag0a, +0x0a->field0c,
// +0x0b->flag0d (PartyMatchManager_SetOwnRecord) - the SAME four bytes
// the request carried.
func EncodePartyRegAck(entry PartyEntry) []byte {
	writer := wire.NewWriter(16 + len(entry.Title)*2)
	writer.U8(1)
	writer.U32(entry.EntryID)
	writer.U32(entry.PartyNumber)
	writer.U8(entry.TypeBits)
	writer.U8(entry.Purpose)
	writer.U8(entry.MinLevel)
	writer.U8(entry.MaxLevel)
	writeSizedWideString(writer, entry.Title)
	return writer.Payload()
}

// EncodeDeleteAck renders the shared 0xB535 / 0xB70B success arm: u8
// flag = 1 + {u32 deletedId}. The client clears its own snapshot when
// the id matches it, else erases the foreign listing row (client
// CPSMission_OnPartyMatchingDeleteResponse0xB535 /
// CNetProcess_OnResponse0xB70B).
func EncodeDeleteAck(entryID uint32) []byte {
	writer := wire.NewWriter(5)
	writer.U8(1)
	writer.U32(entryID)
	return writer.Payload()
}

// EncodePartyListingB588 renders the 0xB588 success arm: u8 flag = 1,
// the page header {u8 curPage, u8 pageCount, u8 rowCount} (curPage rides
// FIRST on the wire; the client manager stores the swapped pair),
// then the client CPSMission_OnPartyMatchingListResponse0xB588 rows.
// The requester's own row must be
// row 0 of its page - the client's row-0 name split rebuilds the own
// snapshot from it instead of listing it.
func EncodePartyListingB588(curPage, pageCount uint8, rows []PartyEntry) []byte {
	writer := wire.NewWriter(4 + len(rows)*32)
	writer.U8(1)
	writer.U8(curPage)
	writer.U8(pageCount)
	writer.U8(uint8(len(rows)))
	for _, row := range rows {
		writer.U32(row.EntryID)
		writer.U32(row.PartyNumber)
		writeSizedString(writer, row.MasterName)
		writer.U8(row.RaceByte)
		writer.U8(row.MemberCount)
		// The pinned wire order puts flag0b (type bits) BEFORE flag0a
		// (purpose) - client CPSMission_OnPartyMatchingListResponse0xB588.
		writer.U8(row.TypeBits)
		writer.U8(row.Purpose)
		writer.U8(row.MinLevel)
		writer.U8(row.MaxLevel)
		writeSizedWideString(writer, row.Title)
	}
	return writer.Payload()
}

// EncodeMentorRegisterAckB55D renders the 0xB55D success arm: u8 flag =
// 1 + {u32 id, u32 dword04, u8 kind, wide detail, u32 dword14} - the
// trailing u32 lands in the pending head's dword14 (the client's
// pending-head store, the window's SLOT_STUDENT count).
func EncodeMentorRegisterAckB55D(entry MentorEntry) []byte {
	writer := wire.NewWriter(16 + len(entry.Detail)*2)
	writer.U8(1)
	writer.U32(entry.EntryID)
	writer.U32(entry.Dword04)
	writer.U8(entry.Kind)
	writeSizedWideString(writer, entry.Detail)
	writer.U32(entry.Dword14)
	return writer.Payload()
}

// EncodeMentorModifyAckB13E renders the 0xB13E success arm: the register
// blob WITHOUT the trailing u32 (client CNetProcess_OnResponse0xB13E).
func EncodeMentorModifyAckB13E(entry MentorEntry) []byte {
	writer := wire.NewWriter(12 + len(entry.Detail)*2)
	writer.U8(1)
	writer.U32(entry.EntryID)
	writer.U32(entry.Dword04)
	writer.U8(entry.Kind)
	writeSizedWideString(writer, entry.Detail)
	return writer.Payload()
}

// EncodeMentorListingB701 renders the 0xB701 success arm: the same page
// header as 0xB588, then the client CNetProcess_OnResponse0xB701 rows
// (the ~0x5C MatchingCandidateEntry parse). The second u32 of every
// row is read and DROPPED by the client; this encoder writes 0 there.
// The row's race byte is NOT on the wire - the client derives it from
// RefObjID through its own record catalog
// (GlobalDataManager_GetCharCosDataById +0x9c).
func EncodeMentorListingB701(curPage, pageCount uint8, rows []MentorEntry) []byte {
	writer := wire.NewWriter(4 + len(rows)*48)
	writer.U8(1)
	writer.U8(curPage)
	writer.U8(pageCount)
	writer.U8(uint8(len(rows)))
	for _, row := range rows {
		writer.U32(row.EntryID)
		writer.U32(0)
		writer.U8(row.Kind)
		writeSizedWideString(writer, row.Detail)
		writer.U32(row.Dword04)
		writer.U8(row.LevelAlt)
		writer.U8(row.Level)
		writer.U32(row.RefObjID)
		writeSizedString(writer, row.Requester)
		writer.U32(row.Dword14)
		writer.U8(row.Grade)
		writer.U32(row.Dword18)
		writer.U32(row.Dword1C)
	}
	return writer.Payload()
}

// writeSizedString appends the client
// CGInterface_Calls_CMsgStreamBuffer_WriteBytes sized-ANSI layout:
// u16 byte length + the bytes (the client widens it via
// ConvertAnsiToWideString).
func writeSizedString(writer *wire.Writer, value string) {
	bytes := []byte(value)
	writer.U16(uint16(len(bytes)))
	writer.Bytes(bytes)
}

// writeSizedWideString appends the client
// CMsgStreamBuffer_ReadSizedUTF16String sized-wide layout: u16
// UTF-16 code-unit count + count*2 bytes of UTF-16LE.
func writeSizedWideString(writer *wire.Writer, value string) {
	units := utf16.Encode([]rune(value))
	writer.U16(uint16(len(units)))
	for _, unit := range units {
		writer.U16(unit)
	}
}

// readSizedWideString consumes the client
// CMsgStreamBuffer_ReadSizedUTF16String layout: u16 code-unit
// count, then count*2 bytes of UTF-16LE.
func readSizedWideString(reader *wire.Reader) (string, error) {
	count, err := reader.U16()
	if err != nil {
		return "", err
	}
	units := make([]uint16, count)
	for i := range units {
		if units[i], err = reader.U16(); err != nil {
			return "", fmt.Errorf("match: wide string cut short at unit %d: %w", i, err)
		}
	}
	return string(utf16.Decode(units)), nil
}
