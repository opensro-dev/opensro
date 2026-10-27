// Package chat is the chat-routing lane of the v1.150 gateway: the C->S
// 0x7367 chat request, its S->C 0xB367 ack, and the S->C 0x3667 chat
// broadcast - All/GM chat to the division cohort, whisper with the
// target-side blocked-whisperer enforcement, party and guild chat as
// presence-targeted member sends.
//
// Every opcode number and byte layout here is pinned from the v1.150
// CLIENT folds (the source of truth for what it parses/composes), never
// from the v1.188 GameServer dump - that version renumbers chat opcodes
// wholesale (its 0x7025/0xB025/0x3026 collide with our war-horn lane).
// Per-frame fold citations sit on each constant.
//
// WHISPER IS CHAT TYPE 2 - settled against three independent v1.150
// client legs (a v1.188-reading mapper claimed 4; the v1.150 dump wins):
//   - SEND: CIFChatViewer_HandleChatInputKey's client prefix switch on
//     firstChar-0x23 writes this+0x108 = 2 for '$' and 4 for '#' -
//     whisper is 2, party is 4.
//   - RECEIVE: CPSMission_HandleChatBroadcast0x3667 parses type 2
//     through the narrow-name slot and type 4 through its own
//     narrow-name arm; both
//     carry {ANSI name + sized wide text}.
//   - PRESENT: ClientChat_PresentChannelMessage case 2 resolves
//     UIIT_CHATERR_WHISPER_TO/FROM_MESSAGE, consults the
//     whisper-display option byte and latches the whisper REPLY
//     target (CGInterface_RecordWhisperPartner); case 4 renders the
//     "(" + UIIT_CTL_PARTY + "):" prefix. Type 4 = party.
package chat

import (
	"fmt"
	"unicode/utf16"

	"opensro.online/server/internal/game/item/wire"
)

// Opcodes, pinned from the v1.150 client.
const (
	// OpChatRequest is the C->S chat send:
	// CIFChatViewer_HandleChatInputKey (the client chat input key
	// handler) composes it through the shared CMsgStreamBuffer
	// machinery - {u8 chatType, u8 second, [whisper: u16 len + ANSI
	// target], u16 wcharCount + UTF-16LE text}.
	OpChatRequest uint16 = 0x7367
	// OpChatAck is the S->C send acknowledgement, client handler
	// CPSMission_OnChatSendErrorResponse0xB367 (registered by
	// CPSMission_RegisterPacketHandlers table index 54): u8 result;
	// result 1 pops the pending outgoing-chat record keyed {u8 chatType,
	// u8 second} and presents it locally; result 2 reads {u8 errCode,
	// u8 chatType, u8 second} and maps errCode to a UIIT_CHATERR_*
	// guide line. All four fields are single bytes (the client fold
	// reads them one byte at a time).
	OpChatAck uint16 = 0xB367
	// OpChatBroadcast is the S->C inbound chat line, client handler
	// CPSMission_HandleChatBroadcast0x3667 (table index 55): u8
	// chatType, then a per-type body - types 1/3 carry {u32 senderGid,
	// sized wide text} (the slot-0 arm); types 2/4/5/0x0B carry
	// {u16 len + ANSI sender name, sized wide text} (the narrow-name
	// slot and the type-4 arm).
	OpChatBroadcast uint16 = 0x3667
	// OpChatHistory is a browser extension, not a retail opcode: the beta
	// public transcript replayed at world admission, one frame of
	// {u8 version=2, u8 count, count x (u64 sentAt Unix ms, u16 length,
	// 0x3667 payload)}. Version 2 added the send time (BUG-067). Retail
	// replays nothing; sending the lines as live 0x3667 made the client show
	// them as just said (an old line over its speaker's head on entry).
	OpChatHistory uint16 = 16
	// chatHistoryVersion is the extension frame's layout version.
	chatHistoryVersion uint8 = 2
)

// Chat types on the 0x7367/0x3667 wire (the
// CIFChatViewer_HandleChatInputKey client prefix switch and the
// CPSMission_HandleChatBroadcast0x3667 parse table;
// ClientChat_PresentChannelMessage presents each).
const (
	// ChatTypeAll: no prefix, non-GM speaker
	// (CIFChatViewer_HandleChatInputKey default arm).
	ChatTypeAll uint8 = 1
	// ChatTypeWhisper: the '$' prefix (the client input handler writes 2).
	ChatTypeWhisper uint8 = 2
	// ChatTypeGM: no prefix with the CICPlayer+0x1890 bit-0 GM mark set.
	// The server FORCES the broadcast type byte to 3 for a privileged
	// speaker (the v1.188 chat request handler's vt+0x53c()==1 arm - the SHAPE we
	// port; the client renders it with the case-3 pink 0xffffaec3).
	ChatTypeGM uint8 = 3
	// ChatTypeParty: the '#' prefix (the client input handler writes 4).
	ChatTypeParty uint8 = 4
	// ChatTypeGuild: the '@' prefix.
	ChatTypeGuild uint8 = 5
	// ChatTypeUnion: the '%' prefix (guild alliance chat).
	ChatTypeUnion uint8 = 0x0B
	// ChatTypeStall: the stall window's own chat box composes it
	// (CIFChatModule_HandleInputKey 545EF0 writes the module's type).
	ChatTypeStall uint8 = 9
)

// 0xB367 result bytes (client CPSMission_OnChatSendErrorResponse0xB367:
// 1 = present the pending record, 2 = the error leg, anything else
// falls to the epilogue).
const (
	ChatAckResultSuccess uint8 = 1
	ChatAckResultError   uint8 = 2
)

// 0xB367 error codes, mapped by the client
// CPSMission_OnChatSendErrorResponse0xB367 switch (errCode - 3) to
// UIIT_CHATERR_* strings. Codes this
// server composes; the squelch (6), invalid-command (8), cant-chat (0x0D)
// and union-limit (0x0E) arms exist client-side but only InvalidCommand
// is reachable here (see the handler's type gate).
const (
	// ChatErrCantFindTarget: "Cannot find [%s]." (%s = the
	// pending whisper target name).
	ChatErrCantFindTarget uint8 = 3
	// ChatErrInvalidCommand: UIIT_CHATERR_INVALID_COMMAND.
	ChatErrInvalidCommand uint8 = 8
	// ChatErrNotPartyMember: UIIT_CHATERR_NOT_A_PARTY_MEMBER.
	ChatErrNotPartyMember uint8 = 0x0A
	// ChatErrNoGuild / ChatErrNoUnion both render
	// UIIT_CHATERR_ALLIANCE_PERMISSION_DENIED (the client's shared
	// 0x0B/0x0C arm). The v1.188 shape acks its guild-membership miss as
	// 0x200B and the union-permission miss as 0x200C; the low byte is
	// what the v1.150 client reads.
	ChatErrNoGuild uint8 = 0x0B
	ChatErrNoUnion uint8 = 0x0C
	// ChatErrNoStall: a stall line from a player at no stall (server
	// the reference server's chat request, type 9); the client shows no text.
	ChatErrNoStall uint8 = 5
)

// ChatMessageMaxWideChars is the client-side compose cap:
// CIFChatViewer_HandleChatInputKey's shared composer truncates the
// message at 0x64 UTF-16 code units. A
// frame carrying more can only come from a non-retail client, so the
// decode REFUSES it (silent, the lane's malformed-frame posture) rather
// than trusting the client or truncating server-side.
const ChatMessageMaxWideChars = 0x64

// ChatTargetNameLenBound mirrors WhisperBlockNameLenBound: the retail
// GameServer rejects names of length >= 0x80 before any lookup.
const ChatTargetNameLenBound = 0x80

// Request is one decoded 0x7367 frame. Message is the UTF-16-decoded
// text; TargetName rides only when ChatType is ChatTypeWhisper.
type Request struct {
	ChatType   uint8
	Second     uint8
	TargetName string
	Message    string
}

// DecodeChatRequest strict-decodes the client
// CIFChatViewer_HandleChatInputKey composer's 0x7367 body:
// {u8 chatType, u8 second, [whisper only: u16 nameLen + ANSI target],
// u16 wcharCount + wcharCount*2 UTF-16LE bytes}, no trailing bytes.
//
// The SECOND byte is accepted verbatim (0x00..0xFF) and echoed into the
// ack. The v1.150 client always writes 0xFF (the shared composer
// the shared composer), and the client's ack handler
// CPSMission_OnChatSendErrorResponse0xB367 pops its pending
// outgoing-chat record keyed on {chatType, second} - so the only
// correct server posture is accept-and-echo.
// A v1.188 mapper read the reference chat handler as "abort if second >= 0xFF"; that
// sense would refuse EVERY retail frame, so the branch must be the
// opposite arm (or a forward-queue index check with no v1.150
// equivalent). Rejected.
func DecodeChatRequest(payload []byte) (Request, error) {
	reader := wire.NewReader(payload)
	chatType, err := reader.U8()
	if err != nil {
		return Request{}, err
	}
	second, err := reader.U8()
	if err != nil {
		return Request{}, err
	}
	request := Request{ChatType: chatType, Second: second}
	if chatType == ChatTypeWhisper {
		nameLen, err := reader.U16()
		if err != nil {
			return Request{}, err
		}
		if nameLen >= ChatTargetNameLenBound {
			return Request{}, fmt.Errorf("chat: whisper target length %d breaches the 0x80 bound", nameLen)
		}
		nameBytes, err := reader.Bytes(int(nameLen))
		if err != nil {
			return Request{}, err
		}
		request.TargetName = string(nameBytes)
	}
	wideCount, err := reader.U16()
	if err != nil {
		return Request{}, err
	}
	if wideCount > ChatMessageMaxWideChars {
		return Request{}, fmt.Errorf("chat: message %d wchars breaches the client's 0x64 compose cap", wideCount)
	}
	textBytes, err := reader.Bytes(int(wideCount) * 2)
	if err != nil {
		return Request{}, err
	}
	if err := reader.Done(); err != nil {
		return Request{}, err
	}
	units := make([]uint16, wideCount)
	for i := range units {
		units[i] = uint16(textBytes[i*2]) | uint16(textBytes[i*2+1])<<8
	}
	request.Message = string(utf16.Decode(units))
	return request, nil
}

// EncodeChatAckSuccess renders the 0xB367 result-1 body {0x01, chatType,
// second}: client CPSMission_OnChatSendErrorResponse0xB367 pops the
// pending outgoing-chat record keyed on exactly these two echoed
// bytes and presents it locally.
// Both bytes MUST echo the request verbatim - a rewrite (e.g. the GM
// type-force, which applies to the 0x3667 broadcast only) would strand
// the client's pending record.
func EncodeChatAckSuccess(chatType, second uint8) []byte {
	return []byte{ChatAckResultSuccess, chatType, second}
}

// EncodeChatAckError renders the 0xB367 result-2 body {0x02, errCode,
// chatType, second}: client CPSMission_OnChatSendErrorResponse0xB367
// reads the error byte, maps it through its switch to the
// UIIT_CHATERR_* guide line, then pops (and resets) the pending
// record keyed on the two echoed bytes. All fields are u8 - the v1.188
// dump's 0x200A-style codes are that server's internal constants; only
// the low byte fits the client's single-byte read.
func EncodeChatAckError(errCode, chatType, second uint8) []byte {
	return []byte{ChatAckResultError, errCode, chatType, second}
}

// EncodeChatBroadcastGid renders the 0x3667 gid-authored body for types
// 1 (All) and 3 (GM): {u8 type, u32 senderGid, u16 wcharCount +
// UTF-16LE text} - client CPSMission_HandleChatBroadcast0x3667's
// slot 0 resolves the name from its gid registry and DROPS the line
// when the gid is the local
// player (the 0xB367 ack already presented the sender's own line).
func EncodeChatBroadcastGid(chatType uint8, senderGid uint32, message string) []byte {
	units := utf16.Encode([]rune(message))
	writer := wire.NewWriter(7 + len(units)*2)
	writer.U8(chatType)
	writer.U32(senderGid)
	writeSizedWideString(writer, units)
	return writer.Payload()
}

// EncodeChatBroadcastNamed renders the 0x3667 name-authored body for
// types 2 (whisper), 4 (party), 5 (guild) and 0x0B (union): {u8 type,
// u16 nameLen + ANSI sender name, u16 wcharCount + UTF-16LE text} -
// client CPSMission_HandleChatBroadcast0x3667 widens the narrow name
// (ConvertAnsiToWideString) and drops own-name echoes. The sender
// name MUST carry the sender's STORED casing: the client's own-echo
// compare is raw code-unit equality (std_wstring_compare_substring),
// and the whisper reply-target latch (CGInterface_RecordWhisperPartner)
// stores the name verbatim.
func EncodeChatBroadcastNamed(chatType uint8, senderName, message string) []byte {
	units := utf16.Encode([]rune(message))
	nameBytes := []byte(senderName)
	writer := wire.NewWriter(5 + len(nameBytes) + len(units)*2)
	writer.U8(chatType)
	writer.U16(uint16(len(nameBytes)))
	writer.Bytes(nameBytes)
	writeSizedWideString(writer, units)
	return writer.Payload()
}

/*
================
DecodeChatBroadcastNamed

The inverse of EncodeChatBroadcastNamed, for the server's own payloads
(the website feed, chatfeed). ok is false on any short or trailing frame.
================
*/
func DecodeChatBroadcastNamed(payload []byte) (chatType uint8, senderName, message string, ok bool) {
	reader := wire.NewReader(payload)
	chatType, err := reader.U8()
	if err != nil {
		return 0, "", "", false
	}
	nameLen, err := reader.U16()
	if err != nil {
		return 0, "", "", false
	}
	name, err := reader.Bytes(int(nameLen))
	if err != nil {
		return 0, "", "", false
	}
	wideCount, err := reader.U16()
	if err != nil {
		return 0, "", "", false
	}
	text, err := reader.Bytes(int(wideCount) * 2)
	if err != nil || reader.Done() != nil {
		return 0, "", "", false
	}
	units := make([]uint16, wideCount)
	for i := range units {
		units[i] = uint16(text[i*2]) | uint16(text[i*2+1])<<8
	}
	return chatType, string(name), string(utf16.Decode(units)), true
}

// writeSizedWideString appends the native sized-wide layout: u16
// wide-char count + count*2 UTF-16LE bytes.
func writeSizedWideString(writer *wire.Writer, units []uint16) {
	writer.U16(uint16(len(units)))
	for _, unit := range units {
		writer.U16(unit)
	}
}
