// Package guild is the GUILD lane: the store-backed 0x32C4 enter-world
// seed, the CONSENT-FREE mutators - create (0x7663 -> 0xB663), notice
// edit (0x777A -> 0xB77A + 0x3B29 subOp 5), kick (0x74B1 -> 0x3B29
// subOp 3), leave (0x756E -> 0xB56E + 0x3B29 subOp 3 kind 1), break
// (0x766E -> 0xB66E + 0x3B29 subOp 1 dissolve), name grant (0x72BC ->
// 0xB2BC + 0x3B29 subOp 6 &0x20), fortress-position grant (0x765F ->
// 0xB65F + 0x3B29 subOp 6 &0x40) and GP donate (0x740F -> 0xB40F +
// 0x3B29 subOp 5 &0x08 + subOp 6 &0x08) - plus the INVITE HANDSHAKE
// (0x73AD -> 0x3393 type-5 prompt -> consent -> 0x32C4/0x3B29 subOp-2
// commit, invite.go) over the authority store's guild door.
//
// The v1.150 client renders the retail no-guild UI from ABSENCE: the
// local player's guild entry stays the empty block
// (makeEmptyGuildEntryBlock, hasGuildFlag04=0) unless a 0x32C4
// guild-info frame arrives, and the client's guild-block deserializer
// (fold sub_826610) ALWAYS sets hasGuildFlag04=1 - so even an "empty"
// synthetic 0x32C4 would paint a guild onto a guildless character.
// No-guild therefore means ZERO guild bytes: for a character WITHOUT a
// guild this package emits no frame on any path and no guild frame
// rides the enter-world seed (the Phase A pin, noguildentry_test.go).
// Every guild block on the wire encodes real store state: the 0x32C4
// enter-world seed of a character whose GuildID FK resolves to a stored
// guild, and the 0xB663 create ack of a guild the atomic create door
// just installed - never an empty or synthetic frame.
//
// Refusals DEFAULT to SILENT (the war-horn / friend-refusal posture:
// strict decode, structured log, zero bytes), and only the arms whose
// trigger/code pair is PINNED in the category 0x10 error table answer
// result=2: create name-length -> 0xB663 {2, 0x18}, notice empty
// subject/contents -> 0xB77A {2, 0x22}/{2, 0x23} (errors.go carries the
// full evidence table). Kick refusals stay FULLY silent - the pinned
// 0x1F code has no pinned S->C carrier frame. Every unpinned
// trigger/code pair stays silent rather than invented.
//
// The invite handshake (invite.go) rides the SHARED 0x3393 invitation
// multiplex the party lane owns on the hub: 0x73AD holds a pending
// invite and prompts the target with 0x3393 {u8 5, u32 inviterRef};
// membership commits only on the target's 0x3393 {01 01} accept
// (sub_6971b0 case 0xc), and every invite refusal stays silent on the
// wire.
//
// Deliberately-absent frontiers:
//   - the 0x3B29 subOp-0x16 BATCH member delta (the multi-jid twin of
//     subOp 6, client arm PINNED @0x0076278b) has NO emit site here:
//     the v1.188 batch emitters (0x38F5 @0x005c4c38/@0x005c4e4b -
//     permission batches; @0x005c482a - a grade+perm+role batch) fire
//     from doors that mutate masked fields on SEVERAL members at once,
//     and no such door exists in this gateway (no permMask mutator
//     exists at all - the JoinerPermMask floor). The client fold and
//     its harness parity are landed; a batch encoder without a caller
//     would be dead code, so the batch stays a documented frontier
//     until a multi-member masked mutation exists.
//
// 0x740F (GP donate) is LIVE: donate menu {1,0x2d} sub_5e4db0
// @0x005e4db0 (in-guild arm sub_8188b0, NO permission-mask bit) ->
// CIFGuildPointUp OK sub_5f49f0 @0x005f49f0 -> composer sub_700a90
// @0x00700a90 {u32 amount}, handled by HandleGpDonate over the ATOMIC
// DonateGuildPoints store door (SP debit + guild GP + member DonatedGP,
// one commit) and answered by the second-host 0xB40F handler sub_766ad0
// (registrar @0x0076eb1e). The "war horn" label this opcode carried
// before 2026-07-29 was a MISNOMER - war horn is 0x7025, the movement
// lane.
//
// 0x756E (leave) left the absent set when its body was PINNED as
// {u32 from CGInterface+0x620} - the same selected-target slot create
// reads: the u32 is
// decoded and logged, never validated (the create npcGid posture; its
// runtime MEANING during a leave stays PROBABLE-stale/zero). 0x766E
// (break) followed on the same pin - its composer sub_7006f0 writes the
// SAME +0x620 slot. 0x72BC/0x765F (the grants) left the refuse set when
// the 0x3B29 subOp-6 delta layout, the &0x20 string destination
// (member+0x04 - resolved from the sub_5e9d20 applicator @0x005ea0e9)
// and the second-host acks 0xB2BC/0xB65F were pinned
// (the dump extracts are cited on the encoders below).
//
// 0x7663 (create) has NO client trigger yet (the NPC-chat entry point
// is not folded), which is why the e2e suite composes it synthetically.
// Anything arriving on a refused opcode is hostile or future - logged,
// never answered.
package guild

import (
	"opensro.online/server/internal/game/item/wire"
)

// C->S opcodes, pinned from the v1.150 client composers.
const (
	// OpGuildCreateRequest: {u32 selectedTargetGid}{u16-len ANSI name}.
	// The gid is the client's selected NPC target at compose time; no
	// NPC-interaction plane exists server-side, so the handler decodes
	// and logs it without validating it against an NPC.
	OpGuildCreateRequest uint16 = 0x7663
	// OpGuildInviteRequest: {u32 targetRef} - the client composer
	// sub_700b50 @0x00700b50 (the selected target's world gid; the
	// client refuses a missing and a self target before composing,
	// @0x00700ba7).
	OpGuildInviteRequest uint16 = 0x73AD
	// OpGuildKickRequest: {u16-len ANSI name}.
	OpGuildKickRequest uint16 = 0x74B1
	// OpGuildLeaveRequest: {u32 selectedTargetGid} - the client
	// composer sub_7007b0 @0x007007b0 writes 4 bytes from the
	// CGInterface+0x620 selected-target slot (helper sub_67b010, the
	// SAME slot create's composer reads). The slot's value during a
	// leave is PROBABLE-stale/zero (the msgbox confirm path never
	// seeds it), so the handler decodes and logs it without validating
	// it - the create npcGid posture.
	OpGuildLeaveRequest uint16 = 0x756E
	// OpGuildBreakRequest: {u32 selectedTargetGid} - the client
	// composer sub_7006f0 @0x007006f0 writes 4 bytes from the SAME
	// CGInterface+0x620 selected-target slot leave's composer reads
	// (helper sub_67b010, write @0x00700764) - byte-for-byte the leave
	// body shape, so the u32 takes the same decode-and-log posture.
	OpGuildBreakRequest uint16 = 0x766E
	// OpGuildNameGrantRequest: {u32 targetJid}{u16-len ANSI name} - the
	// client composer sub_700870 @0x00700870 (u32 write @0x00700907,
	// sized-ANSI write @0x00700925/@0x00700935 after the sub_4b6a40
	// wide->ANSI convert). The u32 is resolved as the target MEMBER's
	// jid: the pinned 0xB2BC ack echoes a jid the client's sub_768690
	// looks up in the member map (@0x0076876d sub_826080).
	OpGuildNameGrantRequest uint16 = 0x72BC
	// OpGuildPositionGrantRequest: {u32 targetJid}{u8 position} - the
	// client composer sub_7009c0 @0x007009c0 (u32 @0x00700a2d, u8
	// @0x00700a3d). Same jid resolve rationale as the name grant (the
	// 0xB65F ack's jid feeds sub_826080 @0x00768a36).
	OpGuildPositionGrantRequest uint16 = 0x765F
	// OpGuildGpDonateRequest: {u32 amount} - the client composer
	// sub_700a90 @0x00700a90 (CanSend @0x00700abc, the 4 amount bytes
	// @0x00700afd). The amount is the CIFGuildPointUp edit's _wtoi
	// (sub_5f49f0 @0x005f49f0); the donate button arms IN-GUILD ONLY
	// (sub_5e4db0 @0x005e4db8 gates on sub_8188b0 - NO permission-mask
	// bit, unlike every sub_5e1e80 command arm). The donation is an
	// SP -> GP exchange: the v1.150 UIIT donate family pins the source
	// ("Will you exchange [%d]of SP into GP and contribute?" /
	// UIIT_MSG_GUILD_ERROR_GP_SUBSCRIPION_SP "Cannot contribute more
	// than SP available to you"), and the v1.188 member schema
	// persists the GP_Donation column (@0x0084c427).
	OpGuildGpDonateRequest uint16 = 0x740F
	// OpGuildNoticeEditRequest: {u16-len ANSI subject}{u16-len ANSI
	// contents}.
	OpGuildNoticeEditRequest uint16 = 0x777A
)

// OpGuildInfo is the S->C 0x32C4 guild-info frame (client fold
// sub_826610 - always sets hasGuildFlag04=1 on receipt). Emitted ONLY
// from real store membership: the enter-world seed of a character whose
// GuildID FK resolves to a stored guild row (EncodeGuildInfo32C4). Never
// emit it EMPTY or synthetic - the client's deserializer would paint a
// guild onto a guildless character.
const OpGuildInfo uint16 = 0x32C4

// S->C opcodes the mutator handlers emit - always from REAL store state,
// never empty or synthetic, and always presence-targeted (never division
// broadcast).
const (
	// OpGuildCreateAck is the 0xB663 create answer: {u8 1} followed by
	// the SAME guild block bytes 0x32C4 carries (the client's 0xB663
	// handler reads the result byte then falls into the sub_826610
	// block deserializer), or {u8 2}{u8 code} on the evidenced
	// name-length refusal (errors.go). Emitted to the creating actor
	// ONLY.
	OpGuildCreateAck uint16 = 0xB663
	// OpGuildNoticeEditAck is the 0xB77A notice-edit answer: {u8 1}
	// with no payload beyond the result byte, or {u8 2}{u8 code} on the
	// evidenced empty-field refusals (errors.go). Emitted to the acting
	// editor.
	OpGuildNoticeEditAck uint16 = 0xB77A
	// OpGuildLeaveAck is the 0xB56E leave answer: {u8 1} with no
	// payload beyond the result byte, emitted to the leaving actor ONLY
	// on success (client handler sub_75c8c0 @0x0075c8c0, mission
	// registry index 165: result 1 -> light UI refresh sub_5d97e0 -
	// the STATE reset rides the subOp-3 kind-1 push, not this frame).
	// The handler also parses {u8 2}{u8 code} into the category-0x10
	// error sink, but NO leave trigger->code pair is pinned for Legend
	// (0x36 leader-refuse and 0x1E no-guild are classic-v1.188
	// PROBABLE only), so result=2 is NEVER emitted here - every leave
	// refusal stays silent (errors.go posture).
	OpGuildLeaveAck uint16 = 0xB56E
	// OpGuildKickAck is 0x74B1's answer (75CB30): only [2][code] is read.
	OpGuildKickAck uint16 = 0xB4B1
	// OpGuildBreakAck is the 0xB66E break answer: {u8 1} with no
	// payload beyond the result byte, emitted to the dissolving leader
	// ONLY on success (client handler sub_75c730 @0x0075c730, mission
	// registry index 160: result 1 -> the light sub_5d97e0 interaction
	// refresh @0x0075c765 - the guild-state wipe rides the subOp-1
	// push, not this frame). The handler also parses {u8 2}{u8 code}
	// into the category-0x10 sink (@0x0075c787), but NO break
	// trigger->code pair is pinned for Legend, so result=2 is NEVER
	// emitted - every break refusal stays silent (the leave posture).
	OpGuildBreakAck uint16 = 0xB66E
	// OpGuildNameGrantAck is the 0xB2BC name-grant answer to the
	// GRANTING ACTOR - a SECOND-HOST frame (CNetProcessSecond registrar
	// sub_76e850 @0x0076eabb -> handler sub_768690): result 1 is
	// {u8 1}{u32 discarded}{u32 jid}{u16-len ANSI grantName} - the
	// handler reads TWO dwords into the same local (@0x00768727/
	// @0x00768735, the first overwritten), finds the member by the
	// surviving jid (sub_826080 @0x0076876d) and writes the string to
	// member+0x40 grantName (@0x0076877f). result 2 is {u8 2}{u8 code}
	// -> the cat-0x10 sink (@0x00768927) - never emitted (the 0x4D/0x52
	// codes are pinned at client-LOCAL call sites only, so the server
	// trigger->code mapping would be invented).
	OpGuildNameGrantAck uint16 = 0xB2BC
	// OpGuildPositionGrantAck is the 0xB65F fortress-position answer to
	// the granting actor - second host too (registrar @0x0076ed0d ->
	// handler sub_7689a0): result 1 is {u8 1}{u32 discarded}{u32 jid}
	// {u8 role} - two dwords into one local (@0x00768a07/@0x00768a15),
	// member by jid (@0x00768a36), role byte to member+0x5c
	// (@0x00768a3c) + the UIIT_STT_FORT_GUILD_* title switch into
	// member+0x60 (@0x00768a52..; role 0 clears). result 2 {u8 2}
	// {u8 code} (@0x00768e25) - never emitted (same rationale).
	OpGuildPositionGrantAck uint16 = 0xB65F
	// OpGuildGpDonateAck is the 0xB40F GP-donate answer to the DONATING
	// ACTOR - a second-host frame (CNetProcessSecond registrar sub_76e850
	// @0x0076eb1e -> handler sub_766ad0): result 1 is {u8 1}{u32 amount},
	// where the u32 is DISPLAY-ONLY - the handler formats it into
	// UIIT_MSG_GUILD_GP_SUBSCRIPION_RESULT ("Contributed [%d]GP.",
	// textuisystem L1538 - the retail SUBSCRIPION typo is on the asset)
	// as a type-0 guide line (sub_67a600 @0x00766b52) and writes NO
	// guild/member twin, so the byte-honest value is the donated amount
	// itself (the confirm string UIIT_MSG_QUESTION_GUILD_GP_SUBSCRIPION_
	// CONFIRM uses the same %d for the entered SP). State truth rides the
	// 0x3B29 deltas below. result 2 is {u8 2}{u8 code} -> the cat-0x10
	// sink (@0x00766b8e) - never emitted: no donate trigger->code pair is
	// pinned in either dump (errors.go posture), so every donate refusal
	// stays wire-silent.
	OpGuildGpDonateAck uint16 = 0xB40F
	// OpGuildUpdatePush is the 0x3B29 guild update push, dispatched on
	// its leading subOp byte: subOp 1 (break/dissolve), subOp 3 (member
	// leave/kick), subOp 5 (guild-record flags update) and subOp 6
	// (member field-mask delta) are the four this lane composes.
	OpGuildUpdatePush uint16 = 0x3B29
)

// S->C opcodes that must stay ABSENT from a guildless character's stream
// - named so the Phase A absence test (noguildentry_test.go) can sweep
// them on the no-guild path. The client renders the retail no-guild UI
// from the ABSENCE of these frames; every emission above happens only on
// a real mutation of real store state.
const (
	// OpGuildInfoAbsent aliases OpGuildInfo for the no-guild absence
	// sweep: on a guildless character's stream the frame must not
	// appear at all.
	OpGuildInfoAbsent = OpGuildInfo
	// OpGuildAckB663Absent aliases the create ack; OpGuildAckB6B8Absent
	// stays an unpinned frame no path composes.
	OpGuildAckB663Absent        = OpGuildCreateAck
	OpGuildAckB6B8Absent uint16 = 0xB6B8
	// OpGuildDeltaPushAbsent aliases the update push for the sweep.
	OpGuildDeltaPushAbsent = OpGuildUpdatePush
)

// OpInvitationProposal is the SHARED, bidirectional invitation multiplex
// 0x3393 (registrar sub_74d330 @0x0074e2c1 -> handler sub_7644e0). The
// PARTY lane owns the single hub registration and routes C->S replies by
// pending-invite ownership; this lane only COMPOSES the S->C guild
// prompt and receives routed consents through the party lane's consent
// arm hookup (wiring.go). The constant is duplicated from internal/game/social/party
// because the import must point the other way (party -> community ->
// guild), the GuildJID precedent.
const OpInvitationProposal uint16 = 0x3393

// InvitationTypeGuild is the sub_7644e0 type byte for the guild arm
// (inviteType 5 -> msgbox kind 0xf).
const InvitationTypeGuild uint8 = 5

// Consent reply bytes, pinned from the sub_6971b0 case-0xc (msgbox kind
// 0xf) INLINE composers - there is no shared consent composer for the
// guild arm (sub_6feca0 belongs to exchange). The FIRST byte of the reply is
// a RESULT code, NOT the proposal type: Accept is {01 01} (@0x006975c3:
// var_139=1 @0x006975f7, var_13a=1 @0x006975f3) and Refuse is {02 16}
// (@0x00697616: var_139=2 @0x0069764b, var_13a=0x16 @0x0069763c) - so
// a reply can never be routed by reading a type byte back off it.
const (
	// ConsentResultAccept / ConsentCodeAccept: the {01 01} accept pair.
	ConsentResultAccept uint8 = 1
	ConsentCodeAccept   uint8 = 1
	// ConsentResultRefuse / ConsentCodeRefuse: the {02 16} refuse pair
	// (documentation - the consent arm treats ANYTHING but the exact
	// accept pair as a refusal).
	ConsentResultRefuse uint8 = 2
	ConsentCodeRefuse   uint8 = 0x16
)

// EncodeInvitePrompt3393 renders the S->C guild prompt body the
// sub_7644e0 guild arm reads (the IDRETRY case @0x00764936):
// {u8 5, u32 inviterRef}. The ref is the INVITER's world gid - the
// client resolves the prompt's guild name from that entity's +0x788
// GuildBannerLabel (sub_8687e0 @0x00764952) and opens the kind-0xf
// msgbox.
func EncodeInvitePrompt3393(inviterRef uint32) []byte {
	return wire.NewWriter(5).U8(InvitationTypeGuild).U32(inviterRef).Payload()
}

// readSizedString consumes the sized-ANSI layout {u16 byte length, the
// bytes} the guild bodies carry (the same shape the friend lane
// decodes).
func readSizedString(reader *wire.Reader) (string, error) {
	length, err := reader.U16()
	if err != nil {
		return "", err
	}
	raw, err := reader.Bytes(int(length))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

// DecodeInviteRequest strict-decodes the 0x73AD body {u32 targetRef}
// (the sub_700b50 composer shape) with no trailing bytes.
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

// CreateRequest is one decoded 0x7663 body.
type CreateRequest struct {
	// SelectedTargetGid is the client's selected NPC target at compose
	// time. Decoded and logged only: no NPC-interaction plane exists
	// server-side, so there is nothing honest to validate it against.
	SelectedTargetGid uint32
	// Name is the requested guild name (ANSI bytes verbatim).
	Name string
}

// DecodeCreateRequest strict-decodes the 0x7663 body
// {u32 selectedTargetGid}{u16-len ANSI name}. An empty name is
// well-formed at this layer (the length prefix legally says 0); the
// handler refuses it.
func DecodeCreateRequest(payload []byte) (CreateRequest, error) {
	reader := wire.NewReader(payload)
	gid, err := reader.U32()
	if err != nil {
		return CreateRequest{}, err
	}
	name, err := readSizedString(reader)
	if err != nil {
		return CreateRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return CreateRequest{}, err
	}
	return CreateRequest{SelectedTargetGid: gid, Name: name}, nil
}

// KickRequest is one decoded 0x74B1 body.
type KickRequest struct {
	// MemberName is the target member's name (ANSI bytes verbatim).
	MemberName string
}

// DecodeKickRequest strict-decodes the 0x74B1 body {u16-len ANSI name}.
func DecodeKickRequest(payload []byte) (KickRequest, error) {
	reader := wire.NewReader(payload)
	name, err := readSizedString(reader)
	if err != nil {
		return KickRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return KickRequest{}, err
	}
	return KickRequest{MemberName: name}, nil
}

// LeaveRequest is one decoded 0x756E body.
type LeaveRequest struct {
	// SelectedTargetGid is the CGInterface+0x620 slot's value at
	// compose time. Decoded and logged only - never validated: the
	// slot's content during a leave is PROBABLE-stale/zero and no
	// NPC-interaction plane exists server-side (the create npcGid
	// posture).
	SelectedTargetGid uint32
}

// DecodeLeaveRequest strict-decodes the 0x756E body
// {u32 selectedTargetGid} with no trailing bytes (the sub_7007b0
// composer writes exactly 4 bytes).
func DecodeLeaveRequest(payload []byte) (LeaveRequest, error) {
	reader := wire.NewReader(payload)
	gid, err := reader.U32()
	if err != nil {
		return LeaveRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return LeaveRequest{}, err
	}
	return LeaveRequest{SelectedTargetGid: gid}, nil
}

// BreakRequest is one decoded 0x766E body.
type BreakRequest struct {
	// SelectedTargetGid is the CGInterface+0x620 slot's value at
	// compose time - the SAME slot leave's composer reads (sub_7006f0
	// @0x00700764). Decoded and logged only, never validated (the
	// leave/create npcGid posture).
	SelectedTargetGid uint32
}

// DecodeBreakRequest strict-decodes the 0x766E body
// {u32 selectedTargetGid} with no trailing bytes (the sub_7006f0
// composer writes exactly 4 bytes - the leave body shape).
func DecodeBreakRequest(payload []byte) (BreakRequest, error) {
	reader := wire.NewReader(payload)
	gid, err := reader.U32()
	if err != nil {
		return BreakRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return BreakRequest{}, err
	}
	return BreakRequest{SelectedTargetGid: gid}, nil
}

// NameGrantRequest is one decoded 0x72BC body.
type NameGrantRequest struct {
	// TargetJID is the grant target's member jid (the wire-space id the
	// 0xB2BC ack echoes back into the client's sub_826080 member find).
	TargetJID uint32
	// GrantName is the granted title (ANSI bytes verbatim - the
	// composer's sub_4b6a40 wide->ANSI output).
	GrantName string
}

// DecodeNameGrantRequest strict-decodes the 0x72BC body
// {u32 targetJid}{u16-len ANSI name} (the sub_700870 composer shape).
func DecodeNameGrantRequest(payload []byte) (NameGrantRequest, error) {
	reader := wire.NewReader(payload)
	targetJID, err := reader.U32()
	if err != nil {
		return NameGrantRequest{}, err
	}
	name, err := readSizedString(reader)
	if err != nil {
		return NameGrantRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return NameGrantRequest{}, err
	}
	return NameGrantRequest{TargetJID: targetJID, GrantName: name}, nil
}

// PositionGrantRequest is one decoded 0x765F body.
type PositionGrantRequest struct {
	// TargetJID is the grant target's member jid (the 0xB65F ack echo
	// rationale).
	TargetJID uint32
	// Position is the fortress-role byte (the client's pinned domain:
	// 0 clears, 1/2/4/8/0x10/0x20 name the six UIIT_STT_FORT_GUILD_*
	// titles).
	Position uint8
}

// DecodePositionGrantRequest strict-decodes the 0x765F body
// {u32 targetJid}{u8 position} (the sub_7009c0 composer shape).
func DecodePositionGrantRequest(payload []byte) (PositionGrantRequest, error) {
	reader := wire.NewReader(payload)
	targetJID, err := reader.U32()
	if err != nil {
		return PositionGrantRequest{}, err
	}
	position, err := reader.U8()
	if err != nil {
		return PositionGrantRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return PositionGrantRequest{}, err
	}
	return PositionGrantRequest{TargetJID: targetJID, Position: position}, nil
}

// GpDonateRequest is one decoded 0x740F body.
type GpDonateRequest struct {
	// Amount is the SP the donor exchanges into guild GP - the
	// CIFGuildPointUp edit's _wtoi (sub_5f49f0 @0x005f4a09), written as
	// the composer's 4 body bytes (sub_700a90 @0x00700afd).
	Amount uint32
}

// DecodeGpDonateRequest strict-decodes the 0x740F body {u32 amount}
// (the sub_700a90 composer shape) with no trailing bytes.
func DecodeGpDonateRequest(payload []byte) (GpDonateRequest, error) {
	reader := wire.NewReader(payload)
	amount, err := reader.U32()
	if err != nil {
		return GpDonateRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return GpDonateRequest{}, err
	}
	return GpDonateRequest{Amount: amount}, nil
}

// NoticeEditRequest is one decoded 0x777A body.
type NoticeEditRequest struct {
	Subject  string
	Contents string
}

// DecodeNoticeEditRequest strict-decodes the 0x777A body
// {u16-len ANSI subject}{u16-len ANSI contents}.
func DecodeNoticeEditRequest(payload []byte) (NoticeEditRequest, error) {
	reader := wire.NewReader(payload)
	subject, err := readSizedString(reader)
	if err != nil {
		return NoticeEditRequest{}, err
	}
	contents, err := readSizedString(reader)
	if err != nil {
		return NoticeEditRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return NoticeEditRequest{}, err
	}
	return NoticeEditRequest{Subject: subject, Contents: contents}, nil
}

// EncodeNoticeEditAckB77A composes the 0xB77A success body: the result
// byte 1 and nothing else - the frame carries NO notice payload, which
// is why the actor also needs the subOp-5 push for data correctness.
func EncodeNoticeEditAckB77A() []byte {
	return []byte{1}
}

// EncodeNoticeUpdate3B29 composes the 0x3B29 subOp-5 notice update the
// client's guild store applies to its notice fields:
// {u8 5}{u8 0x10}{u16-len ANSI subject}{u16-len ANSI contents}. Sent to
// every ONLINE member of the guild INCLUDING the acting editor: the
// actor's client does not write its notice fields locally on compose and
// 0xB77A carries no payload, so without this frame the actor's own view
// would go stale.
func EncodeNoticeUpdate3B29(subject, contents string) []byte {
	writer := wire.NewWriter(6 + len(subject) + len(contents))
	writer.U8(5)
	writer.U8(0x10)
	writeGuildString(writer, subject)
	writeGuildString(writer, contents)
	return writer.Payload()
}

// EncodeMemberKick3B29 composes the 0x3B29 subOp-3 membership removal:
// {u8 3}{u32 jid}{u8 2}. ONE frame serves every recipient - the client
// branches its me-vs-not-me handling by looking the jid up in its own
// member map - so the same bytes go to every online member of the guild
// including the kicked player and the acting kicker.
func EncodeMemberKick3B29(jid uint32) []byte {
	writer := wire.NewWriter(6)
	writer.U8(3)
	writer.U32(jid)
	writer.U8(2)
	return writer.Payload()
}

// EncodeLeaveAckB56E composes the 0xB56E success body: the result byte
// 1 and nothing else. The client's sub_75c8c0 handler runs only a light
// UI refresh on result 1 - the guild-state wipe rides the subOp-3
// kind-1 push, so the ack alone would leave the leaver's client stale.
// Result=2 bodies are deliberately never composed (wire.go constant
// comment: no Legend leave trigger->code pair is pinned).
func EncodeLeaveAckB56E() []byte {
	return []byte{1}
}

// EncodeMemberLeave3B29 composes the 0x3B29 subOp-3 membership removal
// for a VOLUNTARY exit: {u8 3}{u32 jid}{u8 1} - kind 1 = exit (guide
// UIIT_MSG_GUILD_EXIT_RESULT), NOT kick's kind 2 (expulsion). ONE frame
// serves every recipient: the client branches its me-vs-not-me handling
// by looking the jid up in its own member map, and the LEAVER's own
// client takes the jid==me arm into the full guild reset (fold
// sub_762040 case 3) - which is why the leaver must be included in the
// fan-out.
func EncodeMemberLeave3B29(jid uint32) []byte {
	writer := wire.NewWriter(6)
	writer.U8(3)
	writer.U32(jid)
	writer.U8(1)
	return writer.Payload()
}

// EncodeBreakAckB66E composes the 0xB66E success body: the result byte
// 1 and nothing else. The client's sub_75c730 handler runs only the
// light sub_5d97e0 interaction refresh on result 1 (@0x0075c765) - the
// guild-state wipe rides the subOp-1 push. Result=2 bodies are
// deliberately never composed (no Legend break trigger->code pair is
// pinned - the leave-ack rationale).
func EncodeBreakAckB66E() []byte {
	return []byte{1}
}

// EncodeGuildBreak3B29 composes the 0x3B29 subOp-1 dissolve announce:
// {u8 1} and NOTHING else - the client's break arm (fold sub_762040
// case 1 @0x007620e1) reads ZERO wire fields beyond the subOp byte;
// the announced guild name comes from the receiver's own local state
// (FortressMgr+0x150 via sub_8188a0 @0x007620e1), then the
// UIIT_MSG_GUILD_BREAK_RESULT guide + banner fire and the whole guild
// block drops (sub_828be0 @0x00762172). ONE frame serves every online
// member of the dissolved guild including the breaking leader.
func EncodeGuildBreak3B29() []byte {
	return []byte{1}
}

// EncodeNameGrantAckB2BC composes the 0xB2BC success body for the
// granting actor: {u8 1}{u32 jid}{u32 jid}{u16-len ANSI grantName}.
// The client handler (sub_768690, second host) reads TWO dwords into
// the SAME local - the first is dead the moment the second lands
// (@0x00768727/@0x00768735) - so the first dword's native meaning is
// unpinned; emitting the jid twice keeps every read byte honest
// (DECISION: no invented value can leak through a discarded read).
// The surviving jid drives the member find and the string writes
// member+0x40 grantName (@0x0076877f).
func EncodeNameGrantAckB2BC(jid uint32, grantName string) []byte {
	writer := wire.NewWriter(11 + len(grantName))
	writer.U8(1)
	writer.U32(jid)
	writer.U32(jid)
	writeGuildString(writer, grantName)
	return writer.Payload()
}

// EncodePositionGrantAckB65F composes the 0xB65F success body for the
// granting actor: {u8 1}{u32 jid}{u32 jid}{u8 role}. Same discarded
// first-dword shape as 0xB2BC (@0x00768a07/@0x00768a15 - the jid rides
// twice, DECISION as above); the role byte writes member+0x5c and
// resolves the UIIT_STT_FORT_GUILD_* title (0 clears) on the client
// (sub_7689a0 @0x00768a3c/@0x00768a52).
func EncodePositionGrantAckB65F(jid uint32, role uint8) []byte {
	writer := wire.NewWriter(10)
	writer.U8(1)
	writer.U32(jid)
	writer.U32(jid)
	writer.U8(role)
	return writer.Payload()
}

// EncodeMemberGrantName3B29 composes the 0x3B29 subOp-6 member delta
// carrying ONLY the &0x20 string leg:
// {u8 6}{u32 jid}{u8 0x20}{u16-len ANSI grantName}. Frame layout pinned
// by the case-6 dispatcher (fold sub_762040 @0x007626e3: u32 jid, u8
// mask, masked fields in bit order) and the sub_5e9d20 applicator's
// &0x20 arm (sized read sub_4b1710 @0x005ea0d0). NOTE the applicator's
// DESTINATION is member+0x04 - the member NAME wstring
// (sub_4b67f0(src, dst) @0x005ea0e9; the dst-second argument order is
// proven by sub_768690's use of the same helper @0x0076875b into a
// local later assigned to +0x40) - NOT the +0x40 grantName slot the
// actor's ack writes. That asymmetry is the NATIVE contract; receiving
// rosters render the granted title in the name column until the next
// full 0x32C4 seed re-converges them. Sent to every online member
// EXCEPT the granting actor (DECISION: the actor's answer is the ack,
// whose handler deliberately leaves member+0x04 alone - double-serving
// the actor would rename their own roster row where native's ack does
// not).
func EncodeMemberGrantName3B29(jid uint32, grantName string) []byte {
	writer := wire.NewWriter(10 + len(grantName))
	writer.U8(6)
	writer.U32(jid)
	writer.U8(0x20)
	writeGuildString(writer, grantName)
	return writer.Payload()
}

// EncodeGpDonateAckB40F composes the 0xB40F success body for the
// donating actor: {u8 1}{u32 amount}. The u32 is the donated amount -
// the client handler (sub_766ad0, second host) reads it @0x00766b0b
// solely as the format arg of the "Contributed [%d]GP." guide line and
// writes no state, so echoing the request's amount is the only
// byte-honest value (the OpGuildGpDonateAck constant carries the full
// evidence).
func EncodeGpDonateAckB40F(amount uint32) []byte {
	writer := wire.NewWriter(5)
	writer.U8(1)
	writer.U32(amount)
	return writer.Payload()
}

// EncodeGuildGp3B29 composes the 0x3B29 subOp-5 guild-record delta
// carrying ONLY the &0x08 GP leg: {u8 5}{u8 0x08}{u32 newGuildGp}. The
// client's flags applicator (fold sub_762040 slot 4 -> the sub_5e4710
// slice) writes the dword to entry+0x0c (@0x005e4954) with NO banner or
// guide side effect, so the frame is safe for every online member
// INCLUDING the donating actor - whose own pane GP display would
// otherwise go stale (the 0xB40F ack carries no state; the notice-edit
// actor-inclusion rationale, DECISION).
func EncodeGuildGp3B29(newGuildGp uint32) []byte {
	writer := wire.NewWriter(6)
	writer.U8(5)
	writer.U8(0x08)
	writer.U32(newGuildGp)
	return writer.Payload()
}

// EncodeGuildLevel3B29 composes the 0x3B29 subOp-5 guild-record delta
// carrying the &0x04 level and &0x08 GP legs, in the applicator's read
// order (5E4710): {u8 5}{u8 0x0C}{u8 level}{u32 gp}. The &0x04 arm prints
// UIIT_MSG_GUILD_LEVEL_UP_RESULT and refreshes the member cap gauge.
func EncodeGuildLevel3B29(level uint8, gp uint32) []byte {
	writer := wire.NewWriter(7)
	writer.U8(5)
	writer.U8(0x04 | 0x08)
	writer.U8(level)
	writer.U32(gp)
	return writer.Payload()
}

// EncodeMemberDonatedGp3B29 composes the 0x3B29 subOp-6 member delta
// carrying ONLY the &0x08 donated-GP leg:
// {u8 6}{u32 jid}{u8 0x08}{u32 newDonatedGp}. The applicator's &0x08 arm
// writes member+0x28 (fold sub_5e9d20 @0x005e9e00 slice - the roster
// row's GP column) with no banner, so the donor is INCLUDED in the
// fan-out (the EncodeGuildGp3B29 rationale - the actor's ack writes no
// member twin, unlike the grant acks whose actor is excluded).
func EncodeMemberDonatedGp3B29(jid uint32, newDonatedGp uint32) []byte {
	writer := wire.NewWriter(10)
	writer.U8(6)
	writer.U32(jid)
	writer.U8(0x08)
	writer.U32(newDonatedGp)
	return writer.Payload()
}

// EncodeMemberGrade3B29 composes the 0x3B29 subOp-6 member delta carrying
// the &0x04 grade and &0x10 permission legs, in the applicator's read order
// (5E9D20): {u8 6}{u32 jid}{u8 0x14}{u8 grade}{u32 permMask}. Grade 0 makes
// the member the master the guild pane names.
func EncodeMemberGrade3B29(jid uint32, grade uint8, permMask uint32) []byte {
	writer := wire.NewWriter(11)
	writer.U8(6)
	writer.U32(jid)
	writer.U8(0x04 | 0x10)
	writer.U8(grade)
	writer.U32(permMask)
	return writer.Payload()
}

// EncodeMemberFortressRole3B29 composes the 0x3B29 subOp-6 member delta
// carrying ONLY the &0x40 role leg: {u8 6}{u32 jid}{u8 0x40}{u8 role}.
// The applicator's &0x40 arm writes member+0x5c and resolves the
// UIIT_STT_FORT_GUILD_* title into member+0x60 (fold sub_5e9d20
// @0x005ea128..; role 0 clears the text) - the same writes the actor's
// 0xB65F ack performs, so the actor is excluded from this fan-out too
// (the EncodeMemberGrantName3B29 decision, symmetric). The exclusive
// single-bit mask matches the v1.188 emitter posture (sub_5c57c0
// composes {6, jid, 0x40, role}).
func EncodeMemberFortressRole3B29(jid uint32, role uint8) []byte {
	writer := wire.NewWriter(7)
	writer.U8(6)
	writer.U32(jid)
	writer.U8(0x40)
	writer.U8(role)
	return writer.Payload()
}
