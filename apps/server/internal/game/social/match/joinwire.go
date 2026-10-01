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
//   - the joiner's ack consumer (sub_75ebd0 @0x0075ebd0 / sub_769ca0
//     @0x00769ca0) reads a three-way detail byte - 1 COMPLETE banner,
//     0 REFUSED guide, 2 NOREPLY guide - a shape only an approval flow
//     with a timeout produces;
//   - the owner's window composes a three-way ANSWER: party accept
//     sub_63c410 @0x0063c410 (byte 1), refuse sub_63c460 @0x0063c460
//     (byte 0), no-reply sub_63c340 @0x0063c340 (byte 2), all through
//     the 0x30FA composer sub_6fe370 @0x006fe370; the mentor twin rides
//     msgbox kind 0xe -> sub_6971b0 case 0xb -> sub_6fe690 @0x006fe690
//     on 0x35D5 (accept 1 / refuse 0).
//
// EVIDENCE(v1.188, logic): the retail GameServer forwards match-join
// requests as coordinator jobs carrying a member-info snapshot of the
// joiner (sub_5bae80 @0x005bae80: name, race byte, packed HP/MP tenth
// nibbles, region + coords - the same masked member-info shape) and
// separately validates registration/modify purpose-vs-job compatibility
// (sub_5bf2a0 @0x005bf362
// -> error 0x2C23). The opcode numbers are renumbered vs Legend; only
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
	// opcode: C->S {u32 entryId} (composer sub_6fe2b0 @0x006fe31d,
	// caller sub_6353f0 - the window's JOIN button 0xf) and the S->C
	// owner notify (handler sub_75ea70 @0x0075ea70, registered on
	// CPSMission @0x0074e25e).
	OpPartyJoinRequest uint16 = 0x75BF
	// OpPartyJoinAnswer: the owner's reply {u32 echoA, u32 echoB,
	// u8 answer} - composer sub_6fe370 @0x006fe370, fired by the
	// CIFPartyMatchReqJoin pane's accept (sub_63c410, answer 1),
	// refuse (sub_63c460, answer 0) and no-reply (sub_63c340,
	// answer 2) legs; the two u32s echo the notify's first two fields
	// (stored at wnd+0x7b8/+0x7bc by sub_63cbc0).
	OpPartyJoinAnswer uint16 = 0x30FA
	// OpPartyJoinAck: the joiner's ack - handler sub_75ebd0
	// @0x0075ebd0, registered @0x0074e27f.
	OpPartyJoinAck uint16 = 0xB5BF

	// OpMentorJoinRequest: C->S {u32 entryId} (composer sub_6fe5d0
	// @0x006fe63d, caller sub_672820) AND the S->C owner notify
	// (handler sub_769e30 @0x00769e30, CNetProcessSecond @0x0076eccb).
	OpMentorJoinRequest uint16 = 0x7592
	// OpMentorJoinAnswer: the owner's reply {u32 echoA, u32 echoB,
	// u8 answer} - composer sub_6fe690 @0x006fe690, fired from msgbox
	// kind 0xe (sub_6971b0 case 0xb: accept pushes 1, refuse 0); the
	// echo pair is the notify blob's +0x2c/+0x30 fields the msgbox
	// stashed at interface +0xa7c/+0xa80 (ConfigureKind @0x0053021a).
	OpMentorJoinAnswer uint16 = 0x35D5
	// OpMentorJoinAck: the joiner's ack - handler sub_769ca0
	// @0x00769ca0, CNetProcessSecond @0x0076ecec.
	OpMentorJoinAck uint16 = 0xB592
)

// Owner answer bytes (the pinned composer callers above).
const (
	JoinAnswerRefuse  uint8 = 0
	JoinAnswerAccept  uint8 = 1
	JoinAnswerNoReply uint8 = 2
)

// Joiner ack detail bytes, pinned from the ack consumers: sub_75ebd0
// detail 1 -> UIIT_MSG_PARTYMATCH_JOIN_COMPLETE_MASTER banner + auto-
// queue clear, 0 -> UIIT_MSG_PARTYERR_CREATE_PARTY_REFUSED guide, 2 ->
// UIIT_MSG_PARTYMATCH_JOIN_NOREPLY guide; sub_769ca0 mirrors with the
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

// DecodeJoinRequest strict-decodes the shared sub_6fe2b0 / sub_6fe5d0
// body {u32 entryId}.
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

// DecodeJoinAnswer strict-decodes the shared sub_6fe370 / sub_6fe690
// body {u32 echoA, u32 echoB, u8 answer}.
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

EncodePartyJoinNotify75BF follows 75EA70 -> 63CBC0: echoed a/b,
opaque +7c0, mastery references +7c4/+7c8, active job class +7cc,
then 75DB30 member data. 63C6A0 resolves the mastery icons/names.
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
kind-0xe prompt consumes. READ order PINNED (sub_769e30
@0x00769e7d/@0x00769e8b reads the echo pair into the record's
+0x2c/+0x30 - the stack layout var_44+0x2c == var_18 proves the
offsets - then sub_768200 @0x00768200 reads {u32 f00, u32 f04,
u8 f08, u8 f09, u32 f0c, sized-narrow name}). Consumer-pinned
semantics (ConfigureKind case for kind 0xe, @0x00530511..0x00530595):

	f08/f09 = the joiner's level pair, rendered "%d(%d)" (@0x00530533 -
	          the mentor listing's byte09/dword08 posture: both carry
	          the level)
	f0c     = the joiner's model RefObjID - the race label resolves
	          through sub_7efeb0(f0c)+0x9c (@0x0053054a)
	name    = the joiner's name - the UIIT_STT_TC_JOIN_REQUEST body
	          format (@0x00530365)
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

// EncodeJoinError is the outer-2 arm {u8 2, u8 code}: sub_75ebd0 shows the
// code's category-2 notice instead of the generic refusal.
func EncodeJoinError(code uint8) []byte {
	return wire.NewWriter(2).U8(2).U8(code).Payload()
}
