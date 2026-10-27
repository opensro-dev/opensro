// Package community is the social-window lane of the v1.150 gateway: the
// whisper-block mutate handler (0x766F), the community bootstrap seeds
// (friend roster 0x3769, letter list 0xB3CD), the persisted friend
// subsystem (0x7164 add / 0x75DB delete / 0x3F9A events, presence-aware),
// and the LIVE persisted letter subsystem (0x7261 send / 0x73F2 read /
// 0x70CC delete, with the 0x3F9A case-8 push to an online recipient).
//
// Every opcode number and byte layout here is pinned from the v1.150
// CLIENT (the source of truth for what it parses/composes), never from
// v1.188/DuckSoup docs - those renumber social opcodes wholesale (e.g.
// DuckSoup friend-add 0x7302 vs our 0x7164; their party-match 0x706D
// collides with our item-move). Per-frame fold citations sit on each
// constant.
package community

import (
	"errors"
	"fmt"
	"unicode/utf8"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// C->S opcodes, pinned from the client composers (re-harness
// communitySendersParity.test.ts proves each byte-exact against the REAL
// send chain).
const (
	// OpWhisperBlockRequest is the whisper-block mutate: client
	// NetClient_Send766F composes {u8 mode, u16 len, ANSI name}
	// (register mode 1 from the Entry input's 0x8075 client receiver,
	// cancel mode 2 from the Removal confirm's 0x8077 client receiver).
	OpWhisperBlockRequest uint16 = 0x766F
	// OpFriendAddRequest: client NetClient_Send7164 {u16 len, ANSI name}.
	OpFriendAddRequest uint16 = 0x7164
	// OpFriendDeleteRequest: client NetClient_Send75DB {u32 friendJid}.
	OpFriendDeleteRequest uint16 = 0x75DB
	// OpLetterDeleteRequest: client NetClient_Send70CC {u8 index}.
	OpLetterDeleteRequest uint16 = 0x70CC
	// OpLetterReadFetchRequest: client NetClient_Send73F2 {u8 index}.
	OpLetterReadFetchRequest uint16 = 0x73F2
	// OpLetterSendRequest is the letter write composer: client
	// NetClient_Send7261 {u16 len + ANSI receiver}{u16 len + ANSI body} -
	// two chained CGInterface_Calls_CMsgStreamBuffer_WriteBytes
	// length-prefixed writes, receiver FIRST.
	OpLetterSendRequest uint16 = 0x7261
)

// S->C opcodes, pinned from the client inbound folds.
const (
	// OpFriendRosterPush: client CPSMission_OnPartyMatchingInvite0x3769
	// hands the body to the client's roster reader (a server PUSH, no
	// result byte) - u8 friendCount, per friend {u32 jid, u16 len + ANSI
	// name, u32 modelRefId, u8 state (0 online / 1 offline)}.
	OpFriendRosterPush uint16 = 0x3769
	// OpLetterListAnswer: client CPSMission_OnLetterListResponse0xB3CD -
	// u8 result; result 1 = the CharacterDependentData_LoadLetterList
	// blob {u8 count, per letter u16 len + ANSI sender, u32
	// senderModelRefId, u32 packedReceiveTime, u8 readFlag}; result 2 =
	// u8 errorCode.
	OpLetterListAnswer uint16 = 0xB3CD
	// OpFriendLetterEventPush: client CPSMission_OnFriendConnectNotice0x3F9A -
	// the friend/letter live-event
	// multiplex {u8 eventType, ...}. Cases 2 ADD / 3 DELETE / 4 STATE are
	// the friend lane (encoders in friend.go); cases 8/9 are the letter
	// lane (case-8 encoder in letter.go). Events 5/6/7 and anything
	// outside 2..9 ride the client's netprocessininterface.cpp:0x130e
	// assert - never compose them.
	OpFriendLetterEventPush uint16 = 0x3F9A
	// OpLetterSendAckAnswer: client CPSMission_OnLetterSendResponse0xB261 -
	// u8 result; 1 = the UIIT_MSG_LETTER_SENDOK banner, 2 = u8 errorCode
	// notice (codes unpinned - this server never composes the error
	// arm), anything else = no read, no effect.
	OpLetterSendAckAnswer uint16 = 0xB261
	// OpLetterReadBodyAnswer: client CPSMission_OnLetterReadResponse0xB3F2 -
	// u8 result; 1 = {u8 letterIndex, u16 len + ANSI body} (the client's
	// CharacterDependentData_StoreLetterBodyAndMarkRead store + read-flag
	// flip, then the CIFLetterRead open); 2 = u8
	// errorCode notice (unpinned, never composed).
	OpLetterReadBodyAnswer uint16 = 0xB3F2
	// OpLetterDeleteAckAnswer: client CPSMission_OnLetterDeleteResponse0xB0CC -
	// u8 result; 1 = {u8 letterIndex} (the client's
	// CharacterDependentData_EraseLetterAt erase-at-index + panel
	// repopulate); anything else returns with no further read.
	OpLetterDeleteAckAnswer uint16 = 0xB0CC
	// OpWhisperBlockAck: client CPSMission_OnAliasActionResponse0xB66F - the
	// 0x766F answer {u8 mode (1 add
	// / 2 remove - drives which panel apply runs), u8 result}; result 0
	// appends {u16 len + ANSI name} and applies the add/remove to the
	// blocking panel LIVE. Results 1/2/3 raise the msgboxes "Name
	// already exists." / "User does not exist." / "Cannot register any
	// more users to block list."; 4 is a no-op (composed here for the
	// quote-scan refuse: a quoted name draws the no-op result).
	OpWhisperBlockAck uint16 = 0xB66F
)

// Whisper-block request modes (client NetClient_Send766F's mode byte).
const (
	WhisperBlockModeRegister uint8 = 1
	WhisperBlockModeCancel   uint8 = 2
)

// Whisper-block 0xB66F result bytes (client
// CPSMission_OnAliasActionResponse0xB66F's result switch): 0
// applies the add/remove live, 1/2/3 raise the client msgboxes quoted on
// each constant, 4 is the no-op arm (composed for the quote refuse).
const (
	WhisperBlockResultSuccess uint8 = 0
	// WhisperBlockResultAlreadyExists: "Name already exists."
	WhisperBlockResultAlreadyExists uint8 = 1
	// WhisperBlockResultNoSuchUser: "User does not exist."
	WhisperBlockResultNoSuchUser uint8 = 2
	// WhisperBlockResultListFull: "Cannot register any more users to
	// block list."
	WhisperBlockResultListFull uint8 = 3
	// WhisperBlockResultNoEffect: the client's no-op arm - sub_771550
	// reads the byte and does nothing (no msgbox, no panel change). The
	// v1.188 GameServer sends it for the quote-scan hit and
	// for job-enqueue failures (its ADD caller maps every internal
	// result > 3 to wire 4); this server composes
	// it for the quote refuse only.
	WhisperBlockResultNoEffect uint8 = 4
)

// WhisperBlockMaxCount is the client panel's row cap: the literal 0x14 in
// the client ClientChat_WriteBlockCountLabel ": %d / %d" count rewrite.
const WhisperBlockMaxCount = 0x14

// WhisperBlockNameLenBound: the retail GameServer (its whisper-blocking job,
// v1.188 dump) rejects names of length >= 0x80 before SQL; the decode
// applies the same bound (a silent refusal like every decode error).
const WhisperBlockNameLenBound = 0x80

// WhisperBlockRequest is one decoded 0x766F frame.
type WhisperBlockRequest struct {
	Mode uint8
	Name string
}

// DecodeWhisperBlockRequest strict-decodes the client NetClient_Send766F
// wire body
// {u8 mode, u16 len, ANSI name}: exactly one register/cancel mode, a
// non-empty name, and no trailing bytes.
func DecodeWhisperBlockRequest(payload []byte) (WhisperBlockRequest, error) {
	reader := wire.NewReader(payload)
	mode, err := reader.U8()
	if err != nil {
		return WhisperBlockRequest{}, err
	}
	if mode != WhisperBlockModeRegister && mode != WhisperBlockModeCancel {
		return WhisperBlockRequest{}, fmt.Errorf("community: whisper-block mode %d not register(1)/cancel(2)", mode)
	}
	nameLen, err := reader.U16()
	if err != nil {
		return WhisperBlockRequest{}, err
	}
	nameBytes, err := reader.Bytes(int(nameLen))
	if err != nil {
		return WhisperBlockRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return WhisperBlockRequest{}, err
	}
	if nameLen == 0 {
		return WhisperBlockRequest{}, fmt.Errorf("community: whisper-block name is empty")
	}
	if nameLen >= WhisperBlockNameLenBound {
		return WhisperBlockRequest{}, fmt.Errorf("community: whisper-block name length %d breaches the 0x80 GameServer bound", nameLen)
	}
	return WhisperBlockRequest{Mode: mode, Name: string(nameBytes)}, nil
}

// EncodeWhisperBlockAckB66F renders the 0xB66F body the client
// CPSMission_OnAliasActionResponse0xB66F reads:
// u8 mode (1 add / 2 remove), u8 result, and on result 0 ONLY {u16 len +
// ANSI name} - the name the client applies to the blocking panel live.
// Callers pass the STORED casing (for register the two coincide by
// construction): the client's erase-by-name is case-sensitive (a raw
// UTF-16 code-unit compare in the client), so the echo must
// match the displayed row exactly - see the cancel arm in
// HandleWhisperBlock.
func EncodeWhisperBlockAckB66F(mode, result uint8, name string) []byte {
	if result != WhisperBlockResultSuccess {
		return []byte{mode, result}
	}
	writer := wire.NewWriter(4 + len(name))
	writer.U8(mode)
	writer.U8(result)
	writeSizedString(writer, name)
	return writer.Payload()
}

// FriendRosterEntry is one 0x3769 roster row as the client's roster
// reader reads it.
type FriendRosterEntry struct {
	JID        uint32
	Name       string
	ModelRefID uint32
	// State is 0 online / 1 offline (the client's roster reader).
	State uint8
}

// EncodeFriendRoster3769 renders the 0x3769 push body: u8 count, per
// friend {u32 jid, u16 len + ANSI name, u32 modelRefId, u8 state}. An
// empty roster is the single count byte 0x00 - the seed the friend
// window opens empty from.
func EncodeFriendRoster3769(friends []FriendRosterEntry) []byte {
	writer := wire.NewWriter(1 + len(friends)*16)
	writer.U8(uint8(len(friends)))
	for _, friend := range friends {
		writer.U32(friend.JID)
		writeSizedString(writer, friend.Name)
		writer.U32(friend.ModelRefID)
		writer.U8(friend.State)
	}
	return writer.Payload()
}

// LetterListEntry is one 0xB3CD result-1 row as the client's
// CharacterDependentData_LoadLetterList reads it.
type LetterListEntry struct {
	Sender            string
	SenderModelRefID  uint32
	PackedReceiveTime uint32
	ReadFlag          uint8
}

// EncodeLetterListB3CD renders the 0xB3CD result-1 body: u8 result = 1,
// then the CharacterDependentData_LoadLetterList blob {u8 count, per
// letter u16 len + ANSI sender,
// u32 senderModelRefId, u32 packedReceiveTime, u8 readFlag}. An empty
// list is [0x01, 0x00] - the seed the letter window opens empty from.
// (Result 2 is the error arm; this server never composes it - the error
// codes are unpinned vs retail.)
func EncodeLetterListB3CD(letters []LetterListEntry) []byte {
	writer := wire.NewWriter(2 + len(letters)*16)
	writer.U8(1)
	writer.U8(uint8(len(letters)))
	for _, letter := range letters {
		writeSizedString(writer, letter.Sender)
		writer.U32(letter.SenderModelRefID)
		writer.U32(letter.PackedReceiveTime)
		writer.U8(letter.ReadFlag)
	}
	return writer.Payload()
}

// CommunityEventLetterReceived is the client
// CPSMission_OnFriendConnectNotice0x3F9A case-8 event type byte
// (LETTER RECEIVED).
const CommunityEventLetterReceived uint8 = 8

// Letter caps, pinned from the CIFLetterWrite edit controls and the
// CIFLetter panel count literal.
const (
	// LetterReceiverMaxBytes is the receiver CIFEdit max length
	// (client CIFEdit_SetMaximumTextLength(0xc)).
	LetterReceiverMaxBytes = domain.CharacterNameMaxBytes
	// LetterBodyMaxBytes is the contents CIFEdit max length
	// (client CIFEdit_SetMaximumTextLength(0x100)).
	LetterBodyMaxBytes = domain.LetterBodyMaxBytes
	// LetterMailboxMaxCount is the CIFLetter panel's ": %d / %d" cap
	// literal 0x14 (the client's letter panel count write).
	LetterMailboxMaxCount = domain.LetterMailboxMaxCount
)

// LetterSendRequest is one decoded 0x7261 frame.
type LetterSendRequest struct {
	Receiver string
	Body     string
}

// DecodeLetterSendRequest strict-decodes the client NetClient_Send7261
// wire body {u16 len + ANSI receiver}{u16 len + ANSI body} (receiver
// first, the client composer's write order) with no trailing bytes.
// Emptiness and the
// client edit caps are POLICY, judged in the handler - this is layout
// only.
func DecodeLetterSendRequest(payload []byte) (LetterSendRequest, error) {
	reader := wire.NewReader(payload)
	receiver, err := readSizedString(reader)
	if err != nil {
		return LetterSendRequest{}, err
	}
	body, err := readSizedString(reader)
	if err != nil {
		return LetterSendRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return LetterSendRequest{}, err
	}
	return LetterSendRequest{Receiver: receiver, Body: body}, nil
}

// DecodeLetterIndexRequest strict-decodes the shared client
// NetClient_Send70CC (0x70CC) / NetClient_Send73F2 (0x73F2) body
// {u8 index}.
func DecodeLetterIndexRequest(payload []byte) (uint8, error) {
	reader := wire.NewReader(payload)
	index, err := reader.U8()
	if err != nil {
		return 0, err
	}
	if err := reader.Done(); err != nil {
		return 0, err
	}
	return index, nil
}

// EncodeLetterSendAckB261 renders the 0xB261 result-1 body: the single
// byte [0x01] - client CPSMission_OnLetterSendResponse0xB261 reads no
// further and raises the
// UIIT_MSG_LETTER_SENDOK banner. (Result 2's error codes are unpinned;
// refusals stay silent instead.)
func EncodeLetterSendAckB261() []byte {
	return []byte{0x01}
}

// EncodeLetterReadBodyB3F2 renders the 0xB3F2 result-1 body: u8 result =
// 1, u8 letterIndex, u16 len + ANSI body - client
// CPSMission_OnLetterReadResponse0xB3F2 stores the body into the
// index-th record (CharacterDependentData_StoreLetterBodyAndMarkRead,
// which also flips the read
// flag) and opens the CIFLetterRead window.
func EncodeLetterReadBodyB3F2(index uint8, body string) []byte {
	writer := wire.NewWriter(4 + len(body))
	writer.U8(1)
	writer.U8(index)
	writeSizedString(writer, body)
	return writer.Payload()
}

// EncodeLetterDeleteAckB0CC renders the 0xB0CC result-1 body: u8 result
// = 1, u8 letterIndex - client CPSMission_OnLetterDeleteResponse0xB0CC
// erases the index-th record (CharacterDependentData_EraseLetterAt)
// and repopulates the panel.
func EncodeLetterDeleteAckB0CC(index uint8) []byte {
	return []byte{0x01, index}
}

// EncodeLetterReceivedEvent3F9A renders the 0x3F9A case-8 body: u8
// eventType = 8, u16 len + ANSI sender, u32 senderModelRefId, u32
// packedReceiveTime - client CPSMission_OnFriendConnectNotice0x3F9A
// tail-appends the record with readFlag 0
// (unread) and arms the new-letter icon.
func EncodeLetterReceivedEvent3F9A(sender string, senderModelRefID, packedReceiveTime uint32) []byte {
	writer := wire.NewWriter(12 + len(sender))
	writer.U8(CommunityEventLetterReceived)
	writeSizedString(writer, sender)
	writer.U32(senderModelRefID)
	writer.U32(packedReceiveTime)
	return writer.Payload()
}

// writeSizedString appends the client
// CGInterface_Calls_CMsgStreamBuffer_WriteBytes sized-ANSI layout:
// u16 byte
// length + the bytes.
func writeSizedString(writer *wire.Writer, value string) {
	bytes := []byte(value)
	writer.U16(uint16(len(bytes)))
	writer.Bytes(bytes)
}

// errInvalidText refuses player text that is not valid UTF-8.
var errInvalidText = errors.New("text is not valid UTF-8")

// readSizedString reads the client CMsgStreamBuffer_ReadAsciiString
// sized-ANSI layout: u16 byte
// length + the bytes.
func readSizedString(reader *wire.Reader) (string, error) {
	length, err := reader.U16()
	if err != nil {
		return "", err
	}
	bytes, err := reader.Bytes(int(length))
	if err != nil {
		return "", err
	}
	// Player text is stored and replayed to other players at world entry:
	// only valid UTF-8 is admitted, so no stored text can fail a reader.
	if !utf8.Valid(bytes) {
		return "", errInvalidText
	}
	return string(bytes), nil
}
