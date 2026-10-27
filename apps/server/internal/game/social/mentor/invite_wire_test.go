package mentor

// Table pins for the mentor/TC invite handshake's wire pieces (invite.go
// / wire.go): the 0x76B1 strict decode, the 0x3393 type-9 prompt bytes,
// the 0x3AC5 status-2 join row / status-10 sub-1 seed / status-10 sub-2
// notice bytes (hand-rolled oracles, never the production writers), and
// the pending-invitation table's consume/replace/drop semantics
// including the consent refuse arms that run without any store
// collaborator.

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/transport"
)

// offlinePresence satisfies the lane's Presence seam with nobody online -
// the refuse arm's notice leg (presence.SessionByName) must be reachable
// without a transport in these table tests.
type offlinePresence struct{}

func (offlinePresence) SessionByName(string, string) (*transport.Session, bool) { return nil, false }
func (offlinePresence) OnlineByName(string, string) bool                        { return false }

// invitePayload renders the 0x76B1 body {u32 targetRef} little-endian.
func invitePayload(targetRef uint32) []byte {
	buf := &bytes.Buffer{}
	binary.Write(buf, binary.LittleEndian, targetRef)
	return buf.Bytes()
}

// TestDecodeInviteRequest pins the 0x76B1 strict decoder
// {u32 targetRef} (the NetClient_SendTargetCommand702C90 composer
// shape - exactly 4 bytes appended): short, empty and trailing-byte
// payloads error.
func TestDecodeInviteRequest(t *testing.T) {
	t.Parallel()
	got, err := DecodeInviteRequest(invitePayload(42))
	if err != nil || got != 42 {
		t.Fatalf("decode = %d (%v), want 42", got, err)
	}
	max, err := DecodeInviteRequest(invitePayload(0xFFFFFFFF))
	if err != nil || max != 0xFFFFFFFF {
		t.Fatalf("max decode = %d (%v), want 4294967295", max, err)
	}
	for name, malformed := range map[string][]byte{
		"nil":      nil,
		"short":    {0x01, 0x02},
		"trailing": append(invitePayload(42), 0x00),
	} {
		if _, err := DecodeInviteRequest(malformed); err == nil {
			t.Errorf("%s: malformed invite body % X decoded without error", name, malformed)
		}
	}
}

// campOracle hand-rolls 0x3393/0x3AC5 bodies with encoding/binary.
type campOracle struct{ buf bytes.Buffer }

func (o *campOracle) u8(v uint8)   { o.buf.WriteByte(v) }
func (o *campOracle) u16(v uint16) { binary.Write(&o.buf, binary.LittleEndian, v) }
func (o *campOracle) u32(v uint32) { binary.Write(&o.buf, binary.LittleEndian, v) }
func (o *campOracle) str(v string) {
	binary.Write(&o.buf, binary.LittleEndian, uint16(len(v)))
	o.buf.WriteString(v)
}
func (o *campOracle) zeros(n int) {
	for i := 0; i < n; i++ {
		o.buf.WriteByte(0)
	}
}

// memberRowOracle appends one member row in the pinned
// CNetProcess_OnNotification0x3AC5 case-1 /
// CTrainingCampData_ReadRoster read order: {u32 discarded}{u32 memberId}{u32 f08}
// {str name}{u8 kind}{u8 b46}{16-byte blob}{u8 b58}{u8 level59}{u32 f5c}
// {u8 b74}{u32 f78}{u16 x4 coords}{str location}.
func memberRowOracle(o *campOracle, row MemberWireRow) {
	o.u32(row.MemberID)
	o.u32(row.MemberID)
	o.u32(0)
	o.str(row.Name)
	o.u8(row.Kind)
	o.u8(0)
	o.zeros(16)
	o.u8(row.LevelByte58)
	o.u8(row.Level)
	o.u32(0)
	o.u8(0)
	o.u32(0)
	o.u16(0)
	o.u16(0)
	o.u16(0)
	o.u16(0)
	o.str(row.Location)
}

// TestEncodeInvitePrompt3393 pins the S->C type-9 prompt bytes the
// CPSMission_OnConfirmBoxPrompt0x3393 IDCLOSE arm reads: {u8 9, u32
// inviterGid, u16-len ANSI inviterName} little-endian - the name
// rides the WIRE (CMsgStreamBuffer_ReadAsciiString), unlike the
// party/guild arms.
func TestEncodeInvitePrompt3393(t *testing.T) {
	t.Parallel()
	got := EncodeInvitePrompt3393(0x000186A1, "Chun")
	want := []byte{0x09, 0xA1, 0x86, 0x01, 0x00, 0x04, 0x00, 'C', 'h', 'u', 'n'}
	if !bytes.Equal(got, want) {
		t.Fatalf("prompt = % X, want % X", got, want)
	}
}

// TestEncodeCampNotice3AC5 pins the status-10 sub-2 category-0x1D notice
// carrier (CNetProcess_OnNotification0x3AC5 case 9 sub 2 reads ONE
// code byte):
// {u8 10}{u8 2}{u8 code}.
func TestEncodeCampNotice3AC5(t *testing.T) {
	t.Parallel()
	if got, want := EncodeCampNotice3AC5(NoticeInviteRejection), []byte{0x0A, 0x02, 0x11}; !bytes.Equal(got, want) {
		t.Fatalf("notice = % X, want % X", got, want)
	}
}

// TestEncodeCampJoinRow3AC5 pins the status-2 joining-member push against
// the hand-rolled oracle (CNetProcess_OnNotification0x3AC5 case 1
// -> the real CCharacterDependentData_InsertAcademyMemberRecord
// member add).
func TestEncodeCampJoinRow3AC5(t *testing.T) {
	t.Parallel()
	row := MemberWireRow{MemberID: 0x000186A5, Name: "Doran", Kind: MemberKindStudent, LevelByte58: 23, Level: 23}
	oracle := &campOracle{}
	oracle.u8(2)
	memberRowOracle(oracle, row)
	if got, want := EncodeCampJoinRow3AC5(row), oracle.buf.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("join row = % X, want % X", got, want)
	}
}

// TestEncodeCampSeed3AC5 pins the status-10 sub-1 seed against the
// hand-rolled oracle (CNetProcess_OnNotification0x3AC5 case 9
// sub 1: CPSMission_Calls_PartyManager_Shutdown reset,
// CTrainingCampData_ReadCampRecord header,
// CTrainingCampData_ReadRoster counted roster): {u8 10}{u8 1}
// {u32 localMemberId}{16-byte blob}{u8 status}{str subject}{str contents}
// {u8 count}{rows}.
func TestEncodeCampSeed3AC5(t *testing.T) {
	t.Parallel()
	master := MemberWireRow{MemberID: 0x000186A1, Name: "Chun", Kind: MemberKindMaster, LevelByte58: 60, Level: 60}
	student := MemberWireRow{MemberID: 0x000186A5, Name: "Doran", Kind: MemberKindStudent, LevelByte58: 23, Level: 23}
	oracle := &campOracle{}
	oracle.u8(10)
	oracle.u8(1)
	oracle.u32(student.MemberID)
	oracle.zeros(16)
	oracle.u8(0)
	oracle.str("")
	oracle.str("")
	oracle.u8(2)
	memberRowOracle(oracle, master)
	memberRowOracle(oracle, student)
	got := EncodeCampSeed3AC5(student.MemberID, "", "", []MemberWireRow{master, student})
	if want := oracle.buf.Bytes(); !bytes.Equal(got, want) {
		t.Fatalf("seed = % X, want % X", got, want)
	}
}

// TestInvitePendingTable pins the pending-invitation semantics that run
// without any store collaborator: latest-wins replacement (the peer
// lanes' rule), the non-consuming ownership probe, the consume on take,
// and the session-boundary drop.
func TestInvitePendingTable(t *testing.T) {
	t.Parallel()
	r := NewInviteRuntime(&enterworld.Deps{}, offlinePresence{})
	const division = "global-official"

	if r.HasPendingInvite(division, "Berk") {
		t.Fatalf("fresh table reports a pending invite")
	}
	if r.DropPendingInvite(division, "Berk") {
		t.Fatalf("fresh table dropped a pending invite")
	}
	r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", CampID: 1})
	r.setPending(division, "Berk", PendingInvite{InviterName: "Cale", CampID: 2})
	if got := r.PendingInviteCount(); got != 1 {
		t.Fatalf("pending count after replacement = %d, want 1 (latest wins)", got)
	}
	// The key is case-insensitive - the hub bind key convention.
	if !r.HasPendingInvite(division, "bErK") {
		t.Fatalf("ownership probe missed the case-folded key")
	}
	invite, ok := r.takePending(division, "berk")
	if !ok || invite.InviterName != "Cale" || invite.CampID != 2 {
		t.Fatalf("take = %+v/%v, want the REPLACING invite from Cale", invite, ok)
	}
	if _, ok := r.takePending(division, "Berk"); ok {
		t.Fatalf("second take found a consumed invite")
	}
	r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", CampID: 1})
	if !r.DropPendingInvite(division, "Berk") || r.PendingInviteCount() != 0 {
		t.Fatalf("session-boundary drop did not clear the invitation")
	}
}

// TestApplyConsentWithoutCommit pins every consent arm that must run
// BEFORE any store collaborator is touched (the runtime here has none -
// reaching further would panic): the no-pending drop, the pinned {02 00}
// refuse consuming the invitation (the notice leg resolves the OFFLINE
// inviter to nobody and emits nothing), and the forged not-quite-accept
// shapes refusing rather than committing. NOTE the refuse code byte is
// 0x00 (CGInterface_DispatchSimpleMessageBoxResult case 7), NOT the
// guild arm's 0x16.
func TestApplyConsentWithoutCommit(t *testing.T) {
	t.Parallel()
	const division = "global-official"
	actor := &enterworld.Character{ID: 9, Name: "Berk"}

	r := NewInviteRuntime(&enterworld.Deps{}, offlinePresence{})
	// No outstanding invitation: dropped before anything is read.
	r.ApplyConsent(nil, division, actor, ConsentResultAccept, ConsentCodeAccept)

	// The pinned refuse {02 00} consumes the invitation; the inviter is
	// offline here, so the 0x3AC5 {10,2,0x11} rejection notice resolves
	// no session and nothing emits.
	r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", CampID: 1})
	r.ApplyConsent(nil, division, actor, ConsentResultRefuse, ConsentCodeRefuse)
	if r.PendingInviteCount() != 0 {
		t.Fatalf("refuse left the invitation outstanding")
	}

	// Anything but the EXACT {01 01} accept pair is a refusal - including
	// the guild lane's {02 16} shape and the half-accepts.
	for _, forged := range [][2]uint8{{1, 2}, {2, 1}, {0, 0}, {2, 0x16}, {1, 0}} {
		r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", CampID: 1})
		r.ApplyConsent(nil, division, actor, forged[0], forged[1])
		if r.PendingInviteCount() != 0 {
			t.Fatalf("forged consent {%d %#x} left the invitation outstanding", forged[0], forged[1])
		}
	}
}
