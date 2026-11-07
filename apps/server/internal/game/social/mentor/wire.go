/*
===========================================================================

wire.go - the academy lane's wire: opcodes, constants and encoders

The 0x3393 type-9 prompt, the 0x3AC5 status rows and the camp record the
seed and the 0xB2EC opening share.

===========================================================================
*/
// Package mentor is the MENTOR / TRAINING-CAMP (academy) lane: the
// type-9 arm of the shared 0x3393 invitation multiplex - the LAST of the
// three consent arms (party type 1, guild type 5, mentor/TC type 9).
//
// The handshake: the camp MASTER's 0x76B1 invite (the client composer
// NetClient_SendTargetCommand702C90, fired from the Academy pane's
// Invite button (CIFApprenticeShip_OnInviteBtn) behind the
// master-is-me gate) holds a pending invite and prompts the target
// with 0x3393 {u8 9, u32 inviterGid, u16-len ANSI inviterName} (the
// CPSMission_OnConfirmBoxPrompt0x3393 IDCLOSE arm: prefix reads, the
// sized string, simple msgbox type 8 - title UIIT_PAG_PARTYMATCH_
// JOINREQUEST, body UIIT_STT_TC_JOIN_REQUEST formatted with the wire
// string). Membership commits ONLY on the target's 0x3393 {01 01}
// accept (CGInterface_DispatchSimpleMessageBoxResult case 7), routed
// here by the party
// lane's pending-invite ownership - this lane never registers 0x3393.
//
// Camp state persists in the authority store's training_camps /
// training_camp_members tables - the retail server binds camp and
// member records and the training-camp job family persists camps and
// memberships; join/leave ride the camp manager's job requests to the
// shard manager ("can't find training camp to join!").
//
// A camp is opened at the storage NPC (open.go: 0x72EC -> 0xB2EC, the
// client's "Academy Establishment" row 0x2D) and only its master invites
// into it; an invitation or a match join never creates a camp. (An earlier
// note here held that v1.150 had no opening opcode and created the camp
// with the first accepted invitation; CIFNPCTalk_BuildMenuFromCapabilities,\n// CIFNpcTalk_OnSimpleMsgBoxResult and\n// CGInterface_SendTrainingCampOpen72EC show the
// opening, so that path is gone.)
//
// Refusals DEFAULT to SILENT (the guild-invite posture). ONE refusal
// answer is composed: the inviter's rejection notice 0x3AC5
// {u8 10, u8 2, u8 0x11} - the carrier is byte-pinned by the client's
// own fold (CNetProcess_OnNotification0x3AC5 case-10 sub-2 reads ONE
// code byte into CGInterface_ShowSystemNotification)(gi, 0x1d, code))
// and 0x11 is PINNED to
// UIIT_MSG_TC_ERROR_INVITE_REJECTION ("Invitation has been refused.");
// pairing THAT code with THIS
// trigger is a DECISION by string semantics - no other rejection
// carrier exists toward the inviter.
package mentor

import (
	"opensro.online/server/internal/game/item/wire"
)

// OpTCInviteRequest is the C->S mentor/TC invite: {u32 targetRef} - the
// client composer NetClient_SendTargetCommand702C90 appends exactly
// 4 bytes (CMsgStreamBuffer_AppendBytes(&arg_4, 4)) after the
// selected-target / COS-owner resolve and the
// UIIT_STT_TC_PROPOSAL_PENDING notice.
const OpTCInviteRequest uint16 = 0x76B1

// OpTCStatus is the S->C 0x3AC5 training-camp status machine (the
// client fold CNetProcess_OnNotification0x3AC5, the
// CPSMission_RegisterSecondaryPacketHandlers row 29; the one-wire
// gateway relies on the client's T50 ownership
// demux to route it to the third host). This lane composes these of its
// thirteen status legs:
//
//	status 2       one joining-member row -> the sitting members
//	               (case 1 -> the real member add
//	               CCharacterDependentData_InsertAcademyMemberRecord);
//	status 5       one member-field update -> the camp (case 4):
//	               flag 1 the connect/disconnect byte, flag 2 the
//	               level, flag 3 the honor balance;
//	status 10/1    the joiner's own reset + local-membership +
//	               roster seed (case 9 sub 1:
//	               CPSMission_Calls_PartyManager_Shutdown reset then
//	               CTrainingCampData_ReadCampRecord);
//	status 10/2    one category-0x1D notice code byte (case 9 sub 2
//	               -> CGInterface_ShowSystemNotification)(gi, 0x1d, code)).
const OpTCStatus uint16 = 0x3AC5

// OpInvitationProposal is the SHARED invitation multiplex 0x3393. The
// party lane owns the single hub registration; this lane only COMPOSES
// the S->C type-9 prompt and receives routed consents through the party
// lane's consent-arm hookup (the guild lane's constant-duplication
// precedent - the import may not point back through party).
const OpInvitationProposal uint16 = 0x3393

// OpTCBuffState is the S->C 0x3CC1 saved-buff state frame (the client
// handler CPSMission_OnTrainingCampBuffState0x3CC1): a state byte, and
// for the show states two u32s (window minutes remaining, accumulated
// value) that light magic-state slot 7.
const OpTCBuffState uint16 = 0x3CC1

// OpTCBuffList is the S->C 0xB00C buff list (the client handler
// CNetProcessThird_HandleTrainingCampBuffListB00C): a mode byte and,
// for the row modes, a signed count byte and [minutes][member id]
// [value] rows that render as list kind 8 on the receiver's board
// (slot 8, the received/reserved buffs).
const OpTCBuffList uint16 = 0xB00C

// OpTCPassBuffRequest is the C->S 0x700C pass-to-Guardian request:
// the slot-7 pass confirm's Yes (simple-box type 9's dispatch case)
// sends it with an EMPTY payload. The client offers the confirm only
// when the slot's accumulated time is at least one hour.
const OpTCPassBuffRequest uint16 = 0x700C

// Saved-buff states (0x3CC1). The client legs: 0/1 show slot 7 with
// [minutes][value]; 7 prints UIIT_CTL_TC_SAVE_BUFF_TIME_OVER and shows
// the same; 5/8/9 are the byte-only clears; 2/3/4/6 are no-ops.
const (
	// BuffStateKillRefresh is the kill-path refresh (the first grant
	// of a window sends it; later cadence refreshes reuse it).
	BuffStateKillRefresh uint8 = 0
	// BuffStateEntry is the world-entry show of a live saved buff.
	BuffStateEntry uint8 = 1
	// BuffStateRemoval clears slot 7 (leave, kick, disband, pass).
	BuffStateRemoval uint8 = 5
	// BuffStateExpired reports the run-out window (entry after an
	// offline expiry, or the minute pass reaching zero).
	BuffStateExpired uint8 = 7
	// BuffStateGraduated clears on graduation (the level transition
	// into the graduation band and the graduation itself).
	BuffStateGraduated uint8 = 8
)

// Buff-list modes (0xB00C). Modes 0/1 carry rows; 5/8/9 remove the
// rows they carry; 2 is the byte-only pass refusal (the client prints
// UIIT_MSG_TC_PASS_ERROR).
const (
	BuffListAdd            uint8 = 0
	BuffListRefresh        uint8 = 1
	BuffListPassError      uint8 = 2
	BuffListRemove         uint8 = 5
	BuffListRemoveGraduate uint8 = 8
)

// BuffListRow is one 0xB00C row: the window minutes remaining, the
// camp member id the row keys on (the donor for a received row), and
// the points on it.
type BuffListRow struct {
	Minutes  uint32
	MemberID uint32
	Value    uint32
}

// InvitationTypeTC is the CPSMission_OnConfirmBoxPrompt0x3393 type
// byte for the mentor/TC arm (inviteType 9 -> the IDCLOSE case,
// simple msgbox type 8).
const InvitationTypeTC uint8 = 9

// Consent reply bytes, pinned from
// CGInterface_DispatchSimpleMessageBoxResult case 7 (simple msgbox
// type 8) - NOT the CGInterface_OnMsgBoxResult router (the type-9
// prompt is a simple msgbox outside the ConfigureKind system):
// Accept composes {01 01} (result, then code appended) and Refuse
// composes {02 00} - the refuse SECOND byte is 0x00, NOT the guild
// arm's 0x16. The consent arm treats ANYTHING but the exact accept
// pair as a refusal.
const (
	ConsentResultAccept uint8 = 1
	ConsentCodeAccept   uint8 = 1
	ConsentResultRefuse uint8 = 2
	ConsentCodeRefuse   uint8 = 0
)

// Member kinds - the client member record's +0x44 byte, classified
// by the TrainingCampMember_ClassifyGrade grade tail: kind 0 with
// level >= 0x3c publishes the MASTER name to mgr+0x25c (the
// master-is-me gate's source), kind 1 is the sub-mentor / assistant
// guardian band, kind 2 the student/apprentice band.
const (
	MemberKindMaster    uint8 = 0
	MemberKindSubMentor uint8 = 1
	MemberKindStudent   uint8 = 2
)

// The camp caps, pinned by the client's own asserts: the roster read
// (CTrainingCampData_ReadRoster) soft-asserts at 8 rows total
// ("TraningCampMember is Over than 8"), and the Academy pane's slot
// walk asserts 2 sub-mentors ("SubMentor is Over than 2") and 5
// students ("ApprenticeShip is Over than 5") - master + 2 + 5 = 8.
const (
	CampSubMentorCap = 2
	CampStudentCap   = 5
)

// MasterMinLevel is the master's level floor, pinned CLIENT-side:
// the TrainingCampMember_ClassifyGrade kind-0 leg publishes the
// master name to mgr+0x25c ONLY at level >= 0x3c - below it the
// pane's master-is-me gate can never arm, so a camp whose master the
// client cannot recognize would be undrivable.
const MasterMinLevel = 0x3c

// StudentMaxLevel is the student join ceiling. DECISION anchored on the
// client's own band: the slot-exit route force-graduates a kind-2
// member whose level EXCEEDS 0x28 (the apprentice pane's level
// band), so admitting a student already past it would create a
// born-graduated member; the cat-0x1D table carries level-gate codes
// (0x22 UIIT_MSG_TC_ERROR_POSIBLE_41L) but no pinned trigger, so the
// refusal stays silent.
const StudentMaxLevel = 0x28

// NoticeInviteRejection is the category-0x1D code 0x11, PINNED to
// UIIT_MSG_TC_ERROR_INVITE_REJECTION ("Invitation has been refused.")
// in the CGInterface_ShowSystemNotification mentor switch (jump-table
// base 3). Carried by EncodeCampNotice3AC5;
// the trigger pairing (emit to the INVITER on the target's refusal) is
// a DECISION by string semantics.
const NoticeInviteRejection uint8 = 0x11

/*
================
DecodeInviteRequest

DecodeInviteRequest strict-decodes the 0x76B1 body {u32 targetRef}
(the NetClient_SendTargetCommand702C90 composer shape) with no trailing
bytes.
================
*/
func DecodeInviteRequest(payload []byte) (uint32, error) {
	reader := wire.NewReader(payload)
	targetRef, err := reader.U32()
	if err != nil {
		return 0, err
	}
	if err := reader.Done(); err != nil {
		return 0, err
	}
	return targetRef, nil
}

/*
================
writeCampString

writeCampString appends the sized-ANSI layout {u16 byte length, the
bytes} - the CMsgStreamBuffer_ReadAsciiString wire shape every TC
string read takes (the prompt's requester name, the seed's knowledge
pair, the member rows' name/location strings).
================
*/
func writeCampString(writer *wire.Writer, value string) {
	encoded := wire.EncodeWindows1252(value)
	writer.U16(uint16(len(encoded)))
	for _, b := range encoded {
		writer.U8(b)
	}
}

/*
================
EncodeInvitePrompt3393

EncodeInvitePrompt3393 renders the S->C mentor/TC prompt body the
CPSMission_OnConfirmBoxPrompt0x3393 IDCLOSE arm reads: {u8 9, u32
inviterGid, u16-len ANSI inviterName}. Unlike the party/guild arms
the display name arrives ON THE WIRE (the arm never resolves the
entityRef to a name - CMsgStreamBuffer_ReadAsciiString feeds the
UIIT_STT_TC_JOIN_REQUEST "%s" body format directly).
================
*/
func EncodeInvitePrompt3393(inviterGid uint32, inviterName string) []byte {
	writer := wire.NewWriter(7 + len(inviterName))
	writer.U8(InvitationTypeTC)
	writer.U32(inviterGid)
	writeCampString(writer, inviterName)
	return writer.Payload()
}

/*
================
MemberWireRow

MemberWireRow is one 0x3AC5 member row as the client's real member
add (CCharacterDependentData_InsertAcademyMemberRecord) stores it -
field names carry the client record offsets.
================
*/
type MemberWireRow struct {
	// MemberID lands at record +0x00/+0x04 (the wire's FIRST u32 is
	// read and DISCARDED - both call sites pass the SECOND for both
	// id args). The world gid
	// (enterworld.ObjectIDForCharacter) - the party MemberID posture.
	MemberID uint32
	// Name lands at +0x0c (the roster name the is-me split compares).
	Name string
	// Kind lands at +0x44 (0 master / 1 sub-mentor / 2 student).
	Kind uint8
	// Level lands at +0x59 (the grade-band classifier's input and the
	// pane's "%d(%d)" second half); LevelByte58 at +0x58 (the pair's
	// first half - the student's level at camp entry).
	LevelByte58 uint8
	Level       uint8
	// HonorPoints lands at +0x5c (AccumulatedBuffPoint / usable honor
	// point balance on the master row, updated live by status 5 flag 3).
	HonorPoints uint32
	// Location lands at +0x28 (the pane's location column; empty until
	// a status-13 coord row updates it).
	Location string
}

/*
================
writeMemberRow

writeMemberRow appends one member row in the pinned
CNetProcess_OnNotification0x3AC5 case-1 /
CTrainingCampData_ReadRoster field order: {u32 discarded}
{u32 memberId}{u32 f08}{str name}{u8 kind44}{u8 b46}{16-byte blob48}
{u8 b58}{u8 level59}{u32 f5c}{u8 b74}{u32 f78}{u16 x4 coords}
{str location} (the argument-to-offset mapping is the
CCharacterDependentData_InsertAcademyMemberRecord store block).
================
*/
func writeMemberRow(writer *wire.Writer, row MemberWireRow) {
	writer.U32(row.MemberID)
	writer.U32(row.MemberID)
	writer.U32(0)
	writeCampString(writer, row.Name)
	writer.U8(row.Kind)
	writer.U8(0)
	for i := 0; i < 16; i++ {
		writer.U8(0)
	}
	writer.U8(row.LevelByte58)
	writer.U8(row.Level)
	writer.U32(row.HonorPoints)
	writer.U8(0)
	writer.U32(0)
	writer.U16(0)
	writer.U16(0)
	writer.U16(0)
	writer.U16(0)
	writeCampString(writer, row.Location)
}

/*
================
EncodeCampJoinRow3AC5

EncodeCampJoinRow3AC5 renders the status-2 joining-member push the
SITTING members receive: {u8 2} + one member row
(CNetProcess_OnNotification0x3AC5 case 1 -> the real
CCharacterDependentData_InsertAcademyMemberRecord add + the
UIIT_MSG_TC_MACHING_JOINING_MEMBER notice formatted with the row's
name).
================
*/
func EncodeCampJoinRow3AC5(row MemberWireRow) []byte {
	writer := wire.NewWriter(64 + len(row.Name) + len(row.Location))
	writer.U8(2)
	writeMemberRow(writer, row)
	return writer.Payload()
}

/*
================
EncodeCampSeed3AC5

EncodeCampSeed3AC5 renders the status-10 sub-1 join confirmation - the
receiver's OWN camp seed: {u8 10}{u8 1}{u32 localMemberId}
{16-byte blob}{u8 statusByte}{str subject}{str contents}
{u8 rosterCount}{member rows} (CNetProcess_OnNotification0x3AC5
case 9 sub 1).
================
*/
func EncodeCampSeed3AC5(localMemberID uint32, subject, contents string, rows []MemberWireRow) []byte {
	return EncodeCampSeed3AC5WithEvaluation(localMemberID, 0, subject, contents, rows)
}

/*
================
EncodeCampSeed3AC5WithEvaluation

EncodeCampSeed3AC5WithEvaluation renders the status-10 sub-1 camp seed
carrying the camp's EvaluationRank (0 none, 1 crown, 2 gold, 3 silver,
4 bronze) at the statusByte position read by
CTrainingCampData_ReadCampRecord.
================
*/
func EncodeCampSeed3AC5WithEvaluation(localMemberID uint32, evaluationRank uint8, subject, contents string, rows []MemberWireRow) []byte {
	writer := wire.NewWriter(128)
	writer.U8(10)
	writer.U8(1)
	writeCampRecordWithEvaluation(writer, localMemberID, evaluationRank, subject, contents, rows)
	return writer.Payload()
}

/*
================
writeCampRecord

writeCampRecord appends the camp record CTrainingCampData_ReadCampRecord
reads, shared by the
status-10 seed and the 0xB2EC opening answer: {u32 localMemberId}
{16-byte blob}{u8 statusByte}{str subject}{str contents}{u8 rosterCount}
{member rows}.
================
*/
func writeCampRecord(writer *wire.Writer, localMemberID uint32, subject, contents string, rows []MemberWireRow) {
	writeCampRecordWithEvaluation(writer, localMemberID, 0, subject, contents, rows)
}

/*
================
writeCampRecordWithEvaluation

writeCampRecordWithEvaluation writes the camp header and roster rows with
an explicit EvaluationRank byte.
================
*/
func writeCampRecordWithEvaluation(writer *wire.Writer, localMemberID uint32, evaluationRank uint8, subject, contents string, rows []MemberWireRow) {
	writer.U32(localMemberID)
	for i := 0; i < 16; i++ {
		writer.U8(0)
	}
	writer.U8(evaluationRank)
	writeCampString(writer, subject)
	writeCampString(writer, contents)
	writer.U8(uint8(len(rows)))
	for _, row := range rows {
		writeMemberRow(writer, row)
	}
}

/*
================
EncodeCampNotice3AC5

EncodeCampNotice3AC5 renders the status-10 sub-2 notice: {u8 10}
{u8 2}{u8 code} - the pinned category-0x1D carrier
(CNetProcess_OnNotification0x3AC5 case 9 sub 2 reads ONE byte into
CGInterface_ShowSystemNotification(gi, 0x1d, code)).
================
*/
func EncodeCampNotice3AC5(code uint8) []byte {
	return wire.NewWriter(3).U8(10).U8(2).U8(code).Payload()
}

/*
================
EncodeCampDisband3AC5

EncodeCampDisband3AC5 renders the status-1 camp dissolution push
(the server's camp disband; client CNetProcess_OnNotification0x3AC5
case 0): {u8 1}.
================
*/
func EncodeCampDisband3AC5() []byte {
	return []byte{1}
}

/*
================
EncodeCampMemberLeave3AC5

EncodeCampMemberLeave3AC5 renders the status-3 member removal push
(the server's owner timed job; client
CNetProcess_OnNotification0x3AC5 case 2): {u8 3}{u32 memberID}
{u8 flag}, where flag=1 is a voluntary leave and flag=2 is an
expulsion/ban.
================
*/
func EncodeCampMemberLeave3AC5(memberID uint32, flag uint8) []byte {
	return wire.NewWriter(6).U8(3).U32(memberID).U8(flag).Payload()
}

/*
================
EncodeCampMemberGraduate3AC5

EncodeCampMemberGraduate3AC5 renders the status-4 graduation push
(the server's graduation point update; client
CNetProcess_OnNotification0x3AC5 case 3): {u8 4}{u32 memberID}
{u8 flag}, where flag=0 is a voluntary graduation and flag=1 is a
compulsory graduation by the master.
================
*/
func EncodeCampMemberGraduate3AC5(memberID uint32, flag uint8) []byte {
	return wire.NewWriter(6).U8(4).U32(memberID).U8(flag).Payload()
}

/*
================
EncodeCampMemberOnlineUpdate3AC5

EncodeCampMemberOnlineUpdate3AC5 renders the status-5 flag-1
connect/disconnect push (client CNetProcess_OnNotification0x3AC5
case 4 flag 1 - the member record's offline byte, the state its
connect/disconnect latch prints from): {u8 5}{u32 memberID}{u8 1}
{u8 offline}, offline 1 when the member's session has ended, 0
when it has entered the world. The frame and byte are the client's
own; the emission points (the world-bound and session-closed
hooks, invite.go) are the port's inference - no native server
rule for them has been located.
================
*/
func EncodeCampMemberOnlineUpdate3AC5(memberID uint32, offline bool) []byte {
	offlineByte := uint8(0)
	if offline {
		offlineByte = 1
	}
	return wire.NewWriter(7).U8(5).U32(memberID).U8(1).U8(offlineByte).Payload()
}

/*
================
EncodeCampMemberLevelUpdate3AC5

EncodeCampMemberLevelUpdate3AC5 renders the status-5 flag-2 level
update push (the server's member status update to the shard
manager and the camp's status update from it; client
CNetProcess_OnNotification0x3AC5 case 4 flag 2): {u8 5}
{u32 memberID}{u8 2}{u8 level}.
================
*/
func EncodeCampMemberLevelUpdate3AC5(memberID uint32, level uint8) []byte {
	return wire.NewWriter(7).U8(5).U32(memberID).U8(2).U8(level).Payload()
}

/*
================
EncodeCampMemberHonorUpdate3AC5

EncodeCampMemberHonorUpdate3AC5 renders the status-5 flag-3 honor
point balance push (the server's member status update to the shard
manager, the camp's status update from it and its graduation point
update; client CNetProcess_OnNotification0x3AC5 case 4 flag 3):
{u8 5}{u32 memberID}{u8 3}{u32 honorPoints}.
================
*/
func EncodeCampMemberHonorUpdate3AC5(memberID uint32, honorPoints uint32) []byte {
	return wire.NewWriter(10).U8(5).U32(memberID).U8(3).U32(honorPoints).Payload()
}

/*
================
EncodeCampEvaluationUpdate3AC5

EncodeCampEvaluationUpdate3AC5 renders the status-12 camp
evaluation rank update push (client
CNetProcess_OnNotification0x3AC5 case 11): {u8 12}
{u8 evaluationRank}.
================
*/
func EncodeCampEvaluationUpdate3AC5(evaluationRank uint8) []byte {
	return []byte{12, evaluationRank}
}

/*
================
EncodeBuffStateShow3CC1

EncodeBuffStateShow3CC1 renders a 0x3CC1 show state (0, 1 or 7):
{u8 state}{u32 minutes}{u32 value} - the client stores the value and
lights slot 7 with a duration of minutes*60000+1 ms (1 ms at zero).
================
*/
func EncodeBuffStateShow3CC1(state uint8, minutes, value uint32) []byte {
	return wire.NewWriter(9).U8(state).U32(minutes).U32(value).Payload()
}

/*
================
EncodeBuffStateClear3CC1

EncodeBuffStateClear3CC1 renders a 0x3CC1 clear state (5, 8 or 9):
{u8 state} and nothing else - the client's clear arm reads no body,
clears the stored value and removes the slot-7 row.
================
*/
func EncodeBuffStateClear3CC1(state uint8) []byte {
	return []byte{state}
}

/*
================
EncodeBuffListB00C

EncodeBuffListB00C renders one 0xB00C buff-list frame: {u8 mode} and,
for the row modes, {i8 count}{rows of u32 minutes, u32 member id,
u32 value}. Mode 2 (the pass refusal) is the mode byte alone. The
client's count test is signed, so the row count must stay under 128;
a camp's received rows can never approach it (one per camp member).
================
*/
func EncodeBuffListB00C(mode uint8, rows []BuffListRow) []byte {
	if mode == BuffListPassError {
		return []byte{mode}
	}
	writer := wire.NewWriter(2 + 12*len(rows))
	writer.U8(mode)
	writer.U8(uint8(len(rows)))
	for _, row := range rows {
		writer.U32(row.Minutes).U32(row.MemberID).U32(row.Value)
	}
	return writer.Payload()
}
