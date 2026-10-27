/*
===========================================================================

joinwire.go - party-match and mentor-match join wire

===========================================================================
*/

package match

// The match-JOIN wire family: the party-match and mentor-match join
// requests, the S->C owner notifies (SAME opcode numbers as the C->S
// requests - the retail client registers 0x75BF/0x7592 as INBOUND
// handlers too), the dedicated owner ANSWER opcodes and the joiner acks.
//
// JOIN IS AN OWNER-APPROVAL HANDSHAKE, pinned from the v1.150 client
// alone:
//
//   - the joiner's ack consumer (client
//     CPSMission_OnPartyMatchingJoinResponse0xB5BF /
//     CNetProcess_OnResponse0xB592) reads a three-way detail byte -
//     1 COMPLETE banner, 0 REFUSED guide, 2 NOREPLY guide - a shape
//     only an approval flow with a timeout produces;
//   - the owner's window composes a three-way ANSWER: party accept
//     (the client's accept composer, byte 1), refuse (the client's
//     refuse composer, byte 0), no-reply
//     (CIFPartyMatchReqJoin_OnTimeout, byte 2), all through the
//     0x30FA composer NetClient_Send30FA; the mentor twin rides
//     msgbox kind 0xe -> CGInterface_OnMsgBoxResult case 0xb ->
//     NetClient_Send35D5 on 0x35D5 (accept 1 / refuse 0).
//
// EVIDENCE(v1.188, logic): the retail GameServer forwards match-join
// requests as coordinator jobs carrying a member-info snapshot of the
// joiner (name, race byte, packed HP/MP tenth
// nibbles, region + coords - the same masked member-info shape) and
// separately validates registration/modify purpose-vs-job compatibility
// (error 0x2C23). The opcode numbers are renumbered vs Legend; only
// the logic transfers. This one-process gateway collapses the
// coordinator hop.
//
// The answers ride DEDICATED opcodes (0x30FA / 0x35D5), NOT the shared
// 0x3393 invitation multiplex - the reply carries its own {echoA, echoB}
// correlation pair, so the party lane's ConsentArm router (which exists
// only because 0x3393 replies cannot name their subsystem) is not
// involved: the pending-join table below routes by the echoed pair.

import "opensro.online/server/internal/game/item/wire"

// Join C->S / S->C opcodes.
const (
	// OpPartyJoinRequest is BOTH directions of the party-match join
	// opcode: C->S {u32 entryId} (composer NetClient_Send75BF,
	// caller CIFPartyMatch_SendJoinRequest - the window's JOIN
	// button 0xf) and the S->C owner notify (handler
	// CPSMission_OnPartyMatchingJoinReq0x75BF, registered on
	// CPSMission).
	OpPartyJoinRequest uint16 = 0x75BF
	// OpPartyJoinAnswer: the owner's reply {u32 echoA, u32 echoB,
	// u8 answer} - composer NetClient_Send30FA, fired by the
	// CIFPartyMatchReqJoin pane's accept (the client's accept
	// composer, answer 1), refuse (the client's refuse composer,
	// answer 0) and no-reply (CIFPartyMatchReqJoin_OnTimeout,
	// answer 2) legs; the two u32s echo the notify's first two
	// fields (stored at wnd+0x7b8/+0x7bc by
	// CIFPartyMatchReqJoin_SetRequest).
	OpPartyJoinAnswer uint16 = 0x30FA
	// OpPartyJoinAck: the joiner's ack - client handler
	// CPSMission_OnPartyMatchingJoinResponse0xB5BF.
	OpPartyJoinAck uint16 = 0xB5BF

	// OpMentorJoinRequest: C->S {u32 entryId} (composer
	// NetClient_Send7592, fired by the client's mentor window) AND
	// the S->C owner notify (handler
	// SEH_CNetProcessSecond_RegisterPacketHandlers_769e30,
	// CNetProcessSecond).
	OpMentorJoinRequest uint16 = 0x7592
	// OpMentorJoinAnswer: the owner's reply {u32 echoA, u32 echoB,
	// u8 answer} - composer NetClient_Send35D5, fired from msgbox
	// kind 0xe (CGInterface_OnMsgBoxResult case 0xb: accept pushes
	// 1, refuse 0); the echo pair is the notify blob's +0x2c/+0x30
	// fields the msgbox stashed at interface +0xa7c/+0xa80
	// (CIFMessageBox_ConfigureByType).
	OpMentorJoinAnswer uint16 = 0x35D5
	// OpMentorJoinAck: the joiner's ack - client handler
	// CNetProcess_OnResponse0xB592, CNetProcessSecond.
	OpMentorJoinAck uint16 = 0xB592
)

// Owner answer bytes (the pinned composer callers above).
const (
	JoinAnswerRefuse  uint8 = 0
	JoinAnswerAccept  uint8 = 1
	JoinAnswerNoReply uint8 = 2
)

// Joiner ack detail bytes, pinned from the ack consumers: client
// CPSMission_OnPartyMatchingJoinResponse0xB5BF detail 1 ->
// UIIT_MSG_PARTYMATCH_JOIN_COMPLETE_MASTER banner + auto-queue
// clear, 0 -> UIIT_MSG_PARTYERR_CREATE_PARTY_REFUSED guide, 2 ->
// UIIT_MSG_PARTYMATCH_JOIN_NOREPLY guide; CNetProcess_OnResponse0xB592
// mirrors with the
// TC strings (TC_MACHING_COMPLETE / TC_JOIN_CANCEL / TC_JOIN_NOREPLY).
// The outer==2 error arm carries a category-2 code byte; its keys are the
// client's own notice table (EncodeJoinError), so refusals with a named
// reason (level, duplicate, no party) use it and the rest answer the
// outer-1 refused detail.
const (
	JoinAckRefused  uint8 = 0
	JoinAckComplete uint8 = 1
	JoinAckNoReply  uint8 = 2
)

// DecodeJoinRequest strict-decodes the shared NetClient_Send75BF /
// NetClient_Send7592 body {u32 entryId}.
func DecodeJoinRequest(payload []byte) (uint32, error) {
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

// JoinAnswer is one decoded 0x30FA / 0x35D5 owner reply.
type JoinAnswer struct {
	EchoA  uint32
	EchoB  uint32
	Answer uint8
}

// DecodeJoinAnswer strict-decodes the shared NetClient_Send30FA /
// NetClient_Send35D5 body {u32 echoA, u32 echoB, u8 answer}.
func DecodeJoinAnswer(payload []byte) (JoinAnswer, error) {
	reader := wire.NewReader(payload)
	var out JoinAnswer
	var err error
	if out.EchoA, err = reader.U32(); err != nil {
		return JoinAnswer{}, err
	}
	if out.EchoB, err = reader.U32(); err != nil {
		return JoinAnswer{}, err
	}
	if out.Answer, err = reader.U8(); err != nil {
		return JoinAnswer{}, err
	}
	if err := reader.Done(); err != nil {
		return JoinAnswer{}, err
	}
	return out, nil
}

/*
==================
EncodePartyJoinNotify75BF

EncodePartyJoinNotify75BF follows the client
CPSMission_OnPartyMatchingJoinReq0x75BF ->
CIFPartyMatchReqJoin_SetRequest flow: echoed a/b, opaque +7c0,
mastery references +7c4/+7c8, active job class +7cc, then
CPartyMemberInfo_ReadMasked member data. The client's mastery
resolver resolves the mastery icons/names.
==================
*/
func EncodePartyJoinNotify75BF(requestID, entryID uint32, applicant PartyApplicant, memberInfo []byte) []byte {
	writer := wire.NewWriter(21 + len(memberInfo))
	writer.U32(requestID)
	writer.U32(entryID)
	writer.U32(0)
	writer.U32(applicant.Primary)
	writer.U32(applicant.Secondary)
	writer.U8(applicant.JobClass)
	writer.Bytes(memberInfo)
	return writer.Payload()
}

/*
==================
EncodeMentorJoinNotify7592

EncodeMentorJoinNotify7592 renders the S->C owner notify the msgbox
kind-0xe prompt consumes. READ order PINNED (client
SEH_CNetProcessSecond_RegisterPacketHandlers_769e30 reads the echo
pair into the record's +0x2c/+0x30 - the stack layout
var_44+0x2c == var_18 proves the offsets - then
CNetProcess_ReadMentorJoinNotify_768200 reads {u32 f00, u32 f04,
u8 f08, u8 f09, u32 f0c, sized-narrow name}). Consumer-pinned
semantics (CIFMessageBox_ConfigureByType case for kind 0xe):

	f08/f09 = the joiner's level pair, rendered "%d(%d)" (the
	          mentor listing's byte09/dword08 posture: both carry
	          the level)
	f0c     = the joiner's model RefObjID - the race label resolves
	          through GlobalDataManager_GetCharCosDataById(f0c)+0x9c
	name    = the joiner's name - the UIIT_STT_TC_JOIN_REQUEST body
	          format
	f00/f04 = 0 (DECISION: unread by the msgbox configure - zero floor)

==================
*/
func EncodeMentorJoinNotify7592(requestID, entryID uint32, level uint8, refObjID uint32, joinerName string) []byte {
	writer := wire.NewWriter(24 + len(joinerName))
	writer.U32(requestID)
	writer.U32(entryID)
	writer.U32(0)
	writer.U32(0)
	writer.U8(level)
	writer.U8(level)
	writer.U32(refObjID)
	writeSizedString(writer, joinerName)
	return writer.Payload()
}

/*
==================
EncodeJoinAck

EncodeJoinAck renders the shared 0xB5BF / 0xB592 outer-1 arm:
{u8 1, u8 detail}. The outer-2 error arm is never composed (see the
detail-byte constants above).
==================
*/
func EncodeJoinAck(detail uint8) []byte {
	return wire.NewWriter(2).U8(1).U8(detail).Payload()
}

// Join refusal codes of the outer-2 arm: the client's own category-2 notice
// table (constantNativeNotice(2, code), 6895xx), pinned in the client data.
const (
	JoinErrorCantFindParty uint8 = 0x1C // UIIT_MSG_PARTYERR_CANT_FIND_PARTY
	JoinErrorLevel         uint8 = 0x1E // UIIT_MSG_PARTYMATCH_JOIN_ERROR_LEVEL
	JoinErrorDuplicate     uint8 = 0x1F // UIIT_MSG_PARTYMATCH_JOIN_ERROR_DUPLE
)

// EncodeJoinError is the outer-2 arm {u8 2, u8 code}: client
// CPSMission_OnPartyMatchingJoinResponse0xB5BF shows the
// code's category-2 notice instead of the generic refusal.
func EncodeJoinError(code uint8) []byte {
	return wire.NewWriter(2).U8(2).U8(code).Payload()
}
