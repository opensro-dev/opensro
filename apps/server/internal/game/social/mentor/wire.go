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
// The handshake: the camp MASTER's 0x76B1 invite (client composer
// sub_702c90 @0x00702c90, fired from the Academy pane's Invite button
// sub_5c4fd0 @0x005c4fd0 behind the master-is-me gate) holds a pending
// invite and prompts the target with 0x3393 {u8 9, u32 inviterGid,
// u16-len ANSI inviterName} (the sub_7644e0 IDCLOSE arm @0x00764625:
// prefix reads @0x0076452f/@0x0076453d, the sized string @0x0076466d,
// simple msgbox type 8 @0x007647cd - title UIIT_PAG_PARTYMATCH_
// JOINREQUEST, body UIIT_STT_TC_JOIN_REQUEST formatted with the wire
// string). Membership commits ONLY on the target's 0x3393 {01 01}
// accept (sub_68c4c0 case 7 @0x0068c988), routed here by the party
// lane's pending-invite ownership - this lane never registers 0x3393.
//
// Camp state persists in the authority store's training_camps /
// training_camp_members tables - EVIDENCE(v1.188, logic): the retail
// GameServer's _TRAININGCAMP / _TRAININGCAMPMEMBER CDBRecord binds
// and the
// trs_sm_trainingcampjob DB job family persist camps and
// memberships; join/leave ride the camp manager's job requests to the shard
// manager ("can't find training camp to join!").
//
// A camp is opened at the storage NPC (open.go: 0x72EC -> 0xB2EC, the
// client's "Academy Establishment" row 0x2D) and only its master invites
// into it; an invitation or a match join never creates a camp. (An earlier
// note here held that v1.150 had no opening opcode and created the camp
// with the first accepted invitation; 5D9100 / 5D26F0 / 702BD0 show the
// opening, so that path is gone.)
//
// Refusals DEFAULT to SILENT (the guild-invite posture). ONE refusal
// answer is composed: the inviter's rejection notice 0x3AC5
// {u8 10, u8 2, u8 0x11} - the carrier is byte-pinned by the client's
// own fold (sub_773c60 case-10 sub-2 @0x00774d66 reads ONE code byte
// into sub_689420(gi, 0x1d, code)) and 0x11 is PINNED to
// UIIT_MSG_TC_ERROR_INVITE_REJECTION ("Invitation has been refused.");
// pairing THAT code with THIS
// trigger is a DECISION by string semantics - no other rejection
// carrier exists toward the inviter.
package mentor

import (
	"opensro.online/server/internal/game/item/wire"
)

// OpTCInviteRequest is the C->S mentor/TC invite: {u32 targetRef} - the
// client composer sub_702c90 @0x00702c90 appends exactly 4 bytes
// (@0x00702e1b sub_4c3cf0(&arg_4, 4)) after the selected-target /
// COS-owner resolve and the UIIT_STT_TC_PROPOSAL_PENDING notice
// (@0x00702db2).
const OpTCInviteRequest uint16 = 0x76B1

// OpTCStatus is the S->C 0x3AC5 training-camp status machine (client
// fold sub_773c60 @0x00773c60, CNetProcessThird registrar sub_774f40
// row 29; the one-wire gateway relies on the client's T50 ownership
// demux to route it to the third host). This lane composes THREE of its
// thirteen status legs:
//
//	status 2       one joining-member row -> the sitting members
//	               (case 1 @0x00773cd0 -> REAL member add sub_827890);
//	status 10/1    the joiner's own reset + local-membership +
//	               roster seed (case 9 sub 1 @0x00774cdb: sub_823050
//	               reset then sub_82aa10 read);
//	status 10/2    one category-0x1D notice code byte (case 9 sub 2
//	               @0x00774d66 -> sub_689420(gi, 0x1d, code)).
const OpTCStatus uint16 = 0x3AC5

// OpInvitationProposal is the SHARED invitation multiplex 0x3393. The
// party lane owns the single hub registration; this lane only COMPOSES
// the S->C type-9 prompt and receives routed consents through the party
// lane's consent-arm hookup (the guild lane's constant-duplication
// precedent - the import may not point back through party).
const OpInvitationProposal uint16 = 0x3393

// InvitationTypeTC is the sub_7644e0 type byte for the mentor/TC arm
// (inviteType 9 -> the IDCLOSE case @0x00764625, simple msgbox type 8).
const InvitationTypeTC uint8 = 9

// Consent reply bytes, pinned from sub_68c4c0 case 7 (simple msgbox
// type 8) - NOT the sub_6971b0 router (the type-9 prompt is a simple
// msgbox outside the ConfigureKind system): Accept composes {01 01}
// (@0x0068c9ac..: arg_8=1 @0x0068c9e1, var_45=1 @0x0068c9dc, appended
// result-then-code @0x0068c9e6/@0x0068c9f6) and Refuse composes {02 00}
// (@0x0068ca37..: arg_8=2 @0x0068ca6c, var_45=0 @0x0068ca67) - the
// refuse SECOND byte is 0x00, NOT the guild arm's 0x16. The consent arm
// treats ANYTHING but the exact accept pair as a refusal.
const (
	ConsentResultAccept uint8 = 1
	ConsentCodeAccept   uint8 = 1
	ConsentResultRefuse uint8 = 2
	ConsentCodeRefuse   uint8 = 0
)

// Member kinds - the client member record's +0x44 byte, classified by
// the sub_81e9f0 grade tail (@0x0081ea08..0x0081ea71): kind 0 with
// level >= 0x3c publishes the MASTER name to mgr+0x25c (@0x0081ea66 -
// the master-is-me gate's source), kind 1 is the sub-mentor / assistant
// guardian band, kind 2 the student/apprentice band.
const (
	MemberKindMaster    uint8 = 0
	MemberKindSubMentor uint8 = 1
	MemberKindStudent   uint8 = 2
)

// The camp caps, pinned by the client's own asserts: the roster read
// soft-asserts at 8 rows total ("TraningCampMember is Over than 8"
// @0x0082917b), and the Academy pane's slot walk asserts 2 sub-mentors
// ("SubMentor is Over than 2" @0x005c645f) and 5 students
// ("ApprenticeShip is Over than 5" @0x005c6429) - master + 2 + 5 = 8.
const (
	CampSubMentorCap = 2
	CampStudentCap   = 5
)

// MasterMinLevel is the master's level floor, pinned CLIENT-side: the
// sub_81e9f0 kind-0 leg publishes the master name to mgr+0x25c ONLY at
// level >= 0x3c (@0x0081ea66) - below it the pane's master-is-me gate
// can never arm, so a camp whose master the client cannot recognize
// would be undrivable.
const MasterMinLevel = 0x3c

// StudentMaxLevel is the student join ceiling. DECISION anchored on the
// client's own band: the slot-exit route force-graduates a kind-2
// member whose level EXCEEDS 0x28 (apprenticePlane's sub_5c74e0 band
// @0x005c768e), so admitting a student already past it would create a
// born-graduated member; the cat-0x1D table carries level-gate codes
// (0x22 UIIT_MSG_TC_ERROR_POSIBLE_41L) but no pinned trigger, so the
// refusal stays silent.
const StudentMaxLevel = 0x28

// NoticeInviteRejection is the category-0x1D code 0x11, PINNED to
// UIIT_MSG_TC_ERROR_INVITE_REJECTION ("Invitation has been refused.")
// in the sub_689420 mentor switch (jump-table base 3 @0x0068afd6). Carried by EncodeCampNotice3AC5;
// the trigger pairing (emit to the INVITER on the target's refusal) is
// a DECISION by string semantics.
const NoticeInviteRejection uint8 = 0x11

/*
================
DecodeInviteRequest

DecodeInviteRequest strict-decodes the 0x76B1 body {u32 targetRef}
(the sub_702c90 composer shape) with no trailing bytes.
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
bytes} - the sub_4b1710 wire shape every TC string read takes (the
prompt's requester name @0x0076466d, the seed's knowledge pair
@0x0082aaa1/@0x0082aaad, the member rows' name/location strings
@0x008291db/@0x0082933b).
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
sub_7644e0 IDCLOSE arm reads (@0x00764625): {u8 9, u32 inviterGid,
u16-len ANSI inviterName}. Unlike the party/guild arms the display
name arrives ON THE WIRE (the arm never resolves the entityRef to a
name - sub_4b1710 @0x0076466d feeds the UIIT_STT_TC_JOIN_REQUEST
"%s" body format directly).
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

MemberWireRow is one 0x3AC5 member row as the client's REAL member
add (sub_827890) stores it - field names carry the client record
offsets.
================
*/
type MemberWireRow struct {
	// MemberID lands at record +0x00/+0x04 (the wire's FIRST u32 is
	// read and DISCARDED - both call sites pass the SECOND for both id
	// args, @0x00773d45/@0x00773d53). The world gid
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

writeMemberRow appends one member row in the pinned sub_773c60
case-1 / sub_8290e0 field order: {u32 discarded}{u32 memberId}
{u32 f08}{str name}{u8 kind44}{u8 b46}{16-byte blob48}{u8 b58}
{u8 level59}{u32 f5c}{u8 b74}{u32 f78}{u16 x4 coords}{str location}
(reads @0x00773d45..0x00773ee2 / @0x008291b1..0x0082933b; the
argument-to-offset mapping is the sub_827890 store block
@0x008278ed..0x008279be).
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
SITTING members receive: {u8 2} + one member row (sub_773c60 case 1
@0x00773cd0 -> REAL sub_827890 add + the UIIT_MSG_TC_MACHING_
JOINING_MEMBER notice formatted with the row's name).
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
{u8 rosterCount}{member rows} (sub_773c60 case 9 sub 1 @0x00774cdb).
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
4 bronze) at the statusByte position read by 82AA10 (@0x0082aa95).
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

writeCampRecord appends the camp record 82AA10 reads, shared by the
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
{u8 2}{u8 code} - the pinned category-0x1D carrier (sub_773c60 case 9
sub 2 @0x00774d66 reads ONE byte into sub_689420(gi, 0x1d, code)).
================
*/
func EncodeCampNotice3AC5(code uint8) []byte {
	return wire.NewWriter(3).U8(10).U8(2).U8(code).Payload()
}

/*
================
EncodeCampDisband3AC5

EncodeCampDisband3AC5 renders the status-1 camp dissolution push
(the reference server's camp disband and its shard-manager disband; client sub_773c60
case 0 @0x00773cac): {u8 1}.
================
*/
func EncodeCampDisband3AC5() []byte {
	return []byte{1}
}

/*
================
EncodeCampMemberLeave3AC5

EncodeCampMemberLeave3AC5 renders the status-3 member removal push
(the reference server's owner timed job; client sub_773c60
case 2 @0x00774010): {u8 3}{u32 memberID}{u8 flag}, where flag=1 is a
voluntary leave and flag=2 is an expulsion/ban.
================
*/
func EncodeCampMemberLeave3AC5(memberID uint32, flag uint8) []byte {
	return wire.NewWriter(6).U8(3).U32(memberID).U8(flag).Payload()
}

/*
================
EncodeCampMemberGraduate3AC5

EncodeCampMemberGraduate3AC5 renders the status-4 graduation push
(the reference server's graduation point update; client
sub_773c60 case 3 @0x007741a2): {u8 4}{u32 memberID}{u8 flag}, where flag=0 is a
voluntary graduation and flag=1 is a compulsory graduation by the master.
================
*/
func EncodeCampMemberGraduate3AC5(memberID uint32, flag uint8) []byte {
	return wire.NewWriter(6).U8(4).U32(memberID).U8(flag).Payload()
}

/*
================
EncodeCampMemberLevelUpdate3AC5

EncodeCampMemberLevelUpdate3AC5 renders the status-5 flag-2 level update
push (the reference server's member status update to the shard manager and
the camp's status update from it;
client sub_773c60 case 4 flag 2 @0x00774427): {u8 5}{u32 memberID}{u8 2}{u8 level}.
================
*/
func EncodeCampMemberLevelUpdate3AC5(memberID uint32, level uint8) []byte {
	return wire.NewWriter(7).U8(5).U32(memberID).U8(2).U8(level).Payload()
}

/*
================
EncodeCampMemberHonorUpdate3AC5

EncodeCampMemberHonorUpdate3AC5 renders the status-5 flag-3 honor point
balance push (the reference server's member status update to the shard manager,
the camp's status update from it
and its graduation point update;
client sub_773c60 case 4 flag 3 @0x00774568):
{u8 5}{u32 memberID}{u8 3}{u32 honorPoints}.
================
*/
func EncodeCampMemberHonorUpdate3AC5(memberID uint32, honorPoints uint32) []byte {
	return wire.NewWriter(10).U8(5).U32(memberID).U8(3).U32(honorPoints).Payload()
}

/*
================
EncodeCampEvaluationUpdate3AC5

EncodeCampEvaluationUpdate3AC5 renders the status-12 camp evaluation rank
update push (client sub_773c60 case 11 @0x00774dc2): {u8 12}{u8 evaluationRank}.
================
*/
func EncodeCampEvaluationUpdate3AC5(evaluationRank uint8) []byte {
	return []byte{12, evaluationRank}
}
