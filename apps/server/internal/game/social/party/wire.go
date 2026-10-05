// Package party is the party lane of the v1.150 gateway: the in-memory
// party registry with its pending-invitation table (state design: parties
// are session-scoped runtime state that legitimately dies on process
// reboot - never persisted) plus the pinned C->S handlers (0x70D5 /
// 0x751A invite, 0x3393 consent, 0x704F leave, 0x7664 banish) and the
// S->C emissions (0x3393 invite prompt, 0xB0D5 create ack, 0x35D6 bulk
// info, 0x3E58 updates) fanned out with TARGETED s.Send through the
// community presence facade - never a division broadcast. Invites are a
// CONSENT HANDSHAKE: the 0x70D5/0x751A proposal prompts the target with
// 0x3393 {u8 2 or 3, u32 inviterGid, u8 optionBits}; membership commits only on the target's
// 0x3393 {01 01} accept.
//
// Every opcode number and byte layout here is pinned from the v1.150
// CLIENT folds (..\research\wip\browser-1to1\functions\); v1.188 /
// DuckSoup numbers are behavior reference only and never copied. The
// client category-2 byte table is recovered. Existing-member predicates
// are now connected to B0D5/B51A; other refusal producers remain under audit.
package party

import (
	"fmt"

	"opensro.online/server/internal/game/item/wire"
)

// OpPartyJoinInviteAck is client 75B100's response carrier. Failure is
// {2, error u8}; this response does not publish membership.
const OpPartyJoinInviteAck uint16 = 0xB51A

// OpPartyJoinAck is client 75B170's invitee response carrier.
const OpPartyJoinAck uint16 = 0xB452

// partyErrorBusy is the reason a refused transaction submit reports
// (4E7337 pushes 2): the target already has a proposal waiting.
const partyErrorBusy byte = 0x02

// C->S opcodes, pinned from the client composers.
const (
	// OpPartyInviteRequest: sub_6fd830 - the create/join proposal
	// {u32 targetCharRef, u8 optionBits}. The char-ref is the selected
	// target's world gid (sub_67b010 -> sub_852730); the option bits
	// pack the FortressMgr state block's three party-option dwords
	// (+0x00 exp -> 0x1, +0x04 item -> 0x2, +0x08 join-anyone -> 0x4).
	// The client only composes it with NO active party.
	OpPartyInviteRequest uint16 = 0x70D5
	// OpPartyJoinInviteRequest: sub_6fda50 - the in-party proposal
	// {u32 targetCharRef} only. The client dispatch (sub_5b78e0) gates
	// it on leader-or-join-anyone.
	OpPartyJoinInviteRequest uint16 = 0x751A
	// OpPartyLeaveRequest: sub_6fdc30 - EMPTY body; the server splits
	// leave vs dissolve by the sender's leadership.
	OpPartyLeaveRequest uint16 = 0x704F
	// OpPartyBanishRequest: sub_6fdcd0 - {u32 memberId}, the roster
	// record's +0x3c member id (leader-only in the client's slot-exit
	// route sub_5b79b0).
	OpPartyBanishRequest uint16 = 0x7664
)

// OpInvitationProposal is the bidirectional 0x3393 invitation multiplex.
// Native 7644E0 reads type 1 as exchange, 2 as party formation, 3 as
// joining a party, and 5 as guild. Party prompts append an option byte
// after the inviter GID. Do not confuse jump-table indices with types.
// Native 6FDE80/6FE050 send {1,1} on accept, {2,12} on form refusal,
// and {2,23} on join refusal. Exchange 6FECA0 uses a different reply
// family. Shared accept bytes require routing by pending-invite ownership.
const OpInvitationProposal uint16 = 0x3393

// Invitation type bytes (sub_7644e0's jump table, 1-based).
const (
	InvitationTypeParty     uint8 = 2
	InvitationTypePartyJoin uint8 = 3
)

// Internal normalized consent decisions. These are not raw wire bytes;
// the shared router translates the native result/reason pair first.
const (
	ConsentButtonAccept uint8 = 1
	ConsentButtonRefuse uint8 = 2
)

// EncodeInvitationPrompt3393 renders the S->C prompt body the sub_7644e0
// party arm reads: {u8 2 or 3, u32 entityRef, u8 options}. The entityRef is the
// INVITER's world gid - the client resolves the prompt's name from it.
func EncodeInvitationPrompt3393(inviteType uint8, entityRef uint32, options ...uint8) []byte {
	w := wire.NewWriter(6).U8(inviteType).U32(entityRef)
	if inviteType == InvitationTypeParty || inviteType == InvitationTypePartyJoin {
		var value uint8
		if len(options) > 0 {
			value = options[0]
		}
		w.U8(value)
	}
	return w.Payload()
}

// InvitationConsent is one decoded C->S 0x3393 frame.
type InvitationConsent struct {
	InviteType uint8
	Button     uint8
}

// DecodeInvitationConsent3393 reads the shared two-byte consent envelope.
// The historical field names do not imply that its first byte is a type.
// The pending lane interprets the result/reason pair; no trailing bytes.
func DecodeInvitationConsent3393(payload []byte) (InvitationConsent, error) {
	reader := wire.NewReader(payload)
	inviteType, err := reader.U8()
	if err != nil {
		return InvitationConsent{}, err
	}
	button, err := reader.U8()
	if err != nil {
		return InvitationConsent{}, err
	}
	if err := reader.Done(); err != nil {
		return InvitationConsent{}, err
	}
	return InvitationConsent{InviteType: inviteType, Button: button}, nil
}

// S->C opcodes, pinned from the client inbound folds.
const (
	// OpCreatePartyAck: sub_75b070 - u8 result; 1 = {u32 myPartyMemberId}
	// into stateBlock+0x18 (the value every is-me split compares
	// against); 2 = u8 errorCode (category-2 codes UNPINNED - never
	// composed). Both arms clear the +0x4f9 request-pending latch.
	OpCreatePartyAck uint16 = 0xB0D5
	// OpPartyInfo: sub_75e0f0 - u8 flags; bit 1 SETTINGS {u32 leaderId,
	// u8 optionBits} (clears the roster first), bit 2 ROSTER {u8 count,
	// count x masked member rows}. Both bits may ride one frame.
	OpPartyInfo uint16 = 0x35D6
	// OpPartyUpdate: sub_761870 - u8 type; 2 JOIN {masked row}, 3 LEAVE
	// {u32 id, u8 reason}, 6 MEMBER UPDATE {u32 id, masked row}, 1
	// BROKEN {u8}, 9 LEADER DELEGATE {u32 id}.
	OpPartyUpdate uint16 = 0x3E58
)

// PartyInfo flags bits (sub_75e0f0).
const (
	PartyInfoFlagSettings uint8 = 1
	PartyInfoFlagRoster   uint8 = 2
)

// PartyUpdate type bytes (sub_761870's jump table).
const (
	PartyUpdateBroken uint8 = 1
	PartyUpdateJoin   uint8 = 2
	PartyUpdateLeave  uint8 = 3
)

// PartyUpdate type-3 leave reasons (sub_761870 @0x00761aeb jump table:
// 1 UIIT_MSG_PARTY_LOGOUT, 2 _SECEDE, 3 SERVER_MIGRATION debug-only, 4
// _BOOTED).
const (
	PartyLeaveReasonLogout uint8 = 1
	PartyLeaveReasonSecede uint8 = 2
	PartyLeaveReasonBooted uint8 = 4
)

// Party option bits (sub_6fd830's flags byte / sub_75e0f0's unpack into
// sub_818710(exp, item, joinAnyone)).
const (
	PartyOptionExpShare   uint8 = 0x1
	PartyOptionItemShare  uint8 = 0x2
	PartyOptionJoinAnyone uint8 = 0x4
	PartyOptionMask       uint8 = 0x7
)

// Masked member-row bits (sub_75db30's MemberInfoFlag). The server
// composes the full-info subset: id + name/model + level + hp/mp status
// + region/position. Bits 0x40 (secondary/guild name) and 0x80 (+0x41 byte)
// stay unset - no guild/war state exists to fill them, and the client's
// ctor defaults are the honest empty. Bit 0x08 (+0x50/+0x54 pair) is the
// member's primary and secondary mastery, inferred from the read order
// 75DB30 shares with the 75BF join notify; it rides only while
// SRO_PARTY_MASTERIES is on (see masteries.go).
const (
	MemberMaskID       uint8 = 0x10
	MemberMaskName     uint8 = 0x01
	MemberMaskLevel    uint8 = 0x02
	MemberMaskStatus   uint8 = 0x04
	MemberMaskPosition uint8 = 0x20
	MemberMaskMastery  uint8 = 0x08
	// MemberMaskFull is the composed subset above.
	MemberMaskFull = MemberMaskID | MemberMaskName | MemberMaskLevel | MemberMaskStatus | MemberMaskPosition
)

// PartyMaxMembers is the native roster bound: the CIFParty pane owns the
// leader header + 7 slot rows and asserts usernum > 8 ("Party Memeber N
// - Error", ifparty.cpp line 0x1e4).
const PartyMaxMembers = 8

// partyCapacity is admission capacity, distinct from the eight-slot wire/UI
// storage bound. Research server 514560 tests party option bit 0: four members
// without experience sharing, eight with it. Client category 2 codes 14/13
// independently distinguish the two full-party refusals.
func partyCapacity(options uint8) int {
	if options&PartyOptionExpShare != 0 {
		return PartyMaxMembers
	}
	return 4
}

// PartyInviteRequest is one decoded 0x70D5 frame.
type PartyInviteRequest struct {
	TargetRef  uint32
	OptionBits uint8
}

// DecodePartyInviteRequest strict-decodes the sub_6fd830 body
// {u32 targetCharRef, u8 optionBits} with no trailing bytes.
func DecodePartyInviteRequest(payload []byte) (PartyInviteRequest, error) {
	reader := wire.NewReader(payload)
	targetRef, err := reader.U32()
	if err != nil {
		return PartyInviteRequest{}, err
	}
	optionBits, err := reader.U8()
	if err != nil {
		return PartyInviteRequest{}, err
	}
	if err := reader.Done(); err != nil {
		return PartyInviteRequest{}, err
	}
	return PartyInviteRequest{TargetRef: targetRef, OptionBits: optionBits}, nil
}

// DecodePartyJoinInviteRequest strict-decodes the sub_6fda50 body
// {u32 targetCharRef}.
func DecodePartyJoinInviteRequest(payload []byte) (uint32, error) {
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

// DecodePartyLeaveRequest strict-decodes the sub_6fdc30 body: EMPTY. Any
// trailing byte is malformed, not a refusal.
func DecodePartyLeaveRequest(payload []byte) error {
	if len(payload) != 0 {
		return fmt.Errorf("party: 0x704F carries %d byte(s), want empty body", len(payload))
	}
	return nil
}

// DecodePartyBanishRequest strict-decodes the sub_6fdcd0 body
// {u32 memberId}.
func DecodePartyBanishRequest(payload []byte) (uint32, error) {
	reader := wire.NewReader(payload)
	memberID, err := reader.U32()
	if err != nil {
		return 0, err
	}
	if err := reader.Done(); err != nil {
		return 0, err
	}
	return memberID, nil
}

// MemberRow is one masked member row as sub_75db30 reads the
// MemberMaskFull subset. MemberID lands at record+0x00 (and is patched
// into +0x3c by the insert legs - the id the pane's banish route sends
// back); ModelRefID is the bit-0x01 trailing u32 the 0x35D6/0x3E58 legs
// patch into record+0x38 (the race-mark chardata lookup key). The
// status byte packs hp tenths in the LOW nibble and mp tenths in the
// HIGH nibble against the insert's pushed 0xa/0xa denominators.
type MemberRow struct {
	MemberID      uint32
	Name          string
	ModelRefID    uint32
	Level         uint8
	StatusNibbles uint8
	Region        uint16
	PosX          int16
	PosY          int16
	PosZ          int16
	War           uint32
	// Masteries says the primary/secondary pair rides the row (bit 0x08).
	Masteries        bool
	PrimaryMastery   uint32
	SecondaryMastery uint32
}

// EncodeMaskedMemberRow renders one standalone masked member-info
// record (the sub_75db30 wire shape) - the match lane's 0x75BF join
// notify carries the JOINER's record as its tail (sub_75ea70
// @0x0075eb26 reads it with the same deserializer as the 0x35D6/0x3E58
// rows), and this export keeps that ONE encoder authoritative (match
// reaches it through the wiring.go seam, never an import).
func EncodeMaskedMemberRow(row MemberRow) []byte {
	writer := wire.NewWriter(32 + len(row.Name))
	appendMemberRow(writer, row)
	return writer.Payload()
}

// appendMemberRow writes the masked row in sub_75db30's read order:
// mask, bit 0x10 id, bit 0x01 name + model dword, bit 0x02 level, bit
// 0x04 status nibbles, bit 0x20 region + x/y/z int16 triplet + war, and
// last bit 0x08's mastery pair when the row carries it.
func appendMemberRow(writer *wire.Writer, row MemberRow) {
	mask := MemberMaskFull
	if row.Masteries {
		mask |= MemberMaskMastery
	}
	writer.U8(mask)
	writer.U32(row.MemberID)
	writeSizedString(writer, row.Name)
	writer.U32(row.ModelRefID)
	writer.U8(row.Level)
	writer.U8(row.StatusNibbles)
	writer.U16(row.Region)
	writer.U16(uint16(row.PosX))
	writer.U16(uint16(row.PosY))
	writer.U16(uint16(row.PosZ))
	writer.U32(row.War)
	if row.Masteries {
		writer.U32(row.PrimaryMastery)
		writer.U32(row.SecondaryMastery)
	}
}

// EncodeCreatePartyAckB0D5 renders the 0xB0D5 result-1 body: u8 1, u32
// myPartyMemberId (sub_75b070 @0x0075b0c8 -> stateBlock+0x18). The
// result-2 error arm is never composed - its codes are unpinned.
func EncodeCreatePartyAckB0D5(myPartyMemberID uint32) []byte {
	return wire.NewWriter(5).U8(1).U32(myPartyMemberID).Payload()
}

// EncodePartyInfo35D6 renders the 0x35D6 body with BOTH flag bits set:
// u8 flags=3, u32 leaderId, u8 optionBits (the settings leg clears the
// client roster before the rows insert), u8 count, count masked rows.
func EncodePartyInfo35D6(leaderID uint32, optionBits uint8, rows []MemberRow) []byte {
	writer := wire.NewWriter(7 + len(rows)*32)
	writer.U8(PartyInfoFlagSettings | PartyInfoFlagRoster)
	writer.U32(leaderID)
	writer.U8(optionBits)
	writer.U8(uint8(len(rows)))
	for _, row := range rows {
		appendMemberRow(writer, row)
	}
	return writer.Payload()
}

// EncodePartyJoin3E58 renders the 0x3E58 type-2 JOIN body: u8 2, one
// masked member row (sub_761870 @0x007618c8 inserts it and clears the
// request-pending latch).
func EncodePartyJoin3E58(row MemberRow) []byte {
	writer := wire.NewWriter(1 + 32)
	writer.U8(PartyUpdateJoin)
	appendMemberRow(writer, row)
	return writer.Payload()
}

// EncodePartyLeave3E58 renders the 0x3E58 type-3 LEAVE body: u8 3, u32
// memberId, u8 reason. The receiver's is-me split (@0x00761bda vs
// stateBlock+0x18) full-clears the leaver's own client and single-removes
// on everyone else's.
func EncodePartyLeave3E58(memberID uint32, reason uint8) []byte {
	return wire.NewWriter(6).U8(PartyUpdateLeave).U32(memberID).U8(reason).Payload()
}

// EncodePartyBroken3E58 renders the 0x3E58 type-1 BROKEN body: u8 1
// plus the trailing u8 the client reads and discards (@0x00761e78).
// Effect: full roster clear + the UIIT_MSG_PARTY_BROKEN guide.
func EncodePartyBroken3E58() []byte {
	return []byte{PartyUpdateBroken, 0}
}

// VitalStatusNibbles packs the wire status byte: hp tenths low, mp
// tenths high, against the client insert's 0xa/0xa denominators
// (sub_75e0f0 @0x0075e2a0; the type-6 unpack sub_821fd0(id, s & 0xf,
// 0xa, s >> 4, 0xa)). The tenths derivation is SERVER policy (the wire
// pins only the packing): rounded to nearest with a floor of 1 for any
// living vital so a member never renders an empty gauge while alive.
func VitalStatusNibbles(currentHP, maxHP, currentMP, maxMP int64) uint8 {
	return vitalTenths(currentMP, maxMP)<<4 | vitalTenths(currentHP, maxHP)
}

// vitalTenths maps one vital pair onto 0..10.
func vitalTenths(current, max int64) uint8 {
	if max <= 0 || current <= 0 {
		return 0
	}
	if current >= max {
		return 10
	}
	tenths := (current*10 + max/2) / max
	if tenths < 1 {
		tenths = 1
	}
	if tenths > 10 {
		tenths = 10
	}
	return uint8(tenths)
}

// writeSizedString appends the sub_4fd5f0 sized-ANSI layout: u16 byte
// length + the bytes (the sub_75d830 read twin).
func writeSizedString(writer *wire.Writer, value string) {
	bytes := []byte(value)
	writer.U16(uint16(len(bytes)))
	writer.Bytes(bytes)
}
