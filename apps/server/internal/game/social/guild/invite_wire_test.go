package guild

// Table pins for the invite handshake's wire pieces (invite.go /
// wire.go / encode32c4.go): the 0x73AD strict decode, the 0x3393 type-5
// prompt bytes, the 0x3B29 subOp-2 join row bytes (hand-rolled oracle,
// never the production writers), and the pending-invitation table's
// consume/replace/drop semantics including the consent refuse arms that
// run without any store or presence collaborator.

import (
	"bytes"
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// TestDecodeInviteRequest pins the 0x73AD strict decoder
// {u32 targetRef} (the sub_700b50 composer shape): short, empty and
// trailing-byte payloads error.
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

// TestEncodeInvitePrompt3393 pins the S->C prompt bytes the sub_7644e0
// guild arm reads: {u8 5, u32 inviterRef} little-endian.
func TestEncodeInvitePrompt3393(t *testing.T) {
	t.Parallel()
	got := EncodeInvitePrompt3393(0x000186A1)
	want := []byte{0x05, 0xA1, 0x86, 0x01, 0x00}
	if !bytes.Equal(got, want) {
		t.Fatalf("prompt = % X, want % X", got, want)
	}
}

// joinOracle hand-rolls the 0x3B29 subOp-2 body with encoding/binary:
// {u8 2} then the member row in the pinned sub_762040 slot-2 read order
// (jid @0x0076224e .. offline @0x007622f8).
func joinOracle(member enterworld.GuildMemberRecord, offline uint8) []byte {
	buf := &bytes.Buffer{}
	str := func(v string) {
		binary.Write(buf, binary.LittleEndian, uint16(len(v)))
		buf.WriteString(v)
	}
	buf.WriteByte(2)
	binary.Write(buf, binary.LittleEndian, member.JID)
	str(member.Name)
	buf.WriteByte(member.Grade)
	buf.WriteByte(member.Level)
	binary.Write(buf, binary.LittleEndian, member.DonatedGP)
	binary.Write(buf, binary.LittleEndian, member.PermMask)
	binary.Write(buf, binary.LittleEndian, member.Dword30)
	binary.Write(buf, binary.LittleEndian, member.Dword34)
	binary.Write(buf, binary.LittleEndian, member.Dword38)
	str(member.GrantName)
	binary.Write(buf, binary.LittleEndian, member.RefObjID)
	buf.WriteByte(member.FortressRole)
	buf.WriteByte(offline)
	return buf.Bytes()
}

// TestEncodeMemberJoin3B29 pins the subOp-2 join push against the
// hand-rolled oracle, with the offline flag DERIVED both ways (the
// 0x32C4 rule: never persisted, read off live presence at encode time;
// a nil online func honestly reads offline).
func TestEncodeMemberJoin3B29(t *testing.T) {
	t.Parallel()
	member := enterworld.GuildMemberRecord{
		CharID:    7,
		JID:       0x00098967,
		Name:      "Fennel",
		Grade:     JoinerGrade,
		Level:     23,
		DonatedGP: 0,
		PermMask:  JoinerPermMask,
		GrantName: "",
		RefObjID:  1907,
	}
	online := func(name string) bool { return name == "Fennel" }
	if got, want := EncodeMemberJoin3B29(member, online), joinOracle(member, 0); !bytes.Equal(got, want) {
		t.Fatalf("online join push = % X, want % X", got, want)
	}
	if got, want := EncodeMemberJoin3B29(member, nil), joinOracle(member, 1); !bytes.Equal(got, want) {
		t.Fatalf("nil-presence join push = % X, want % X", got, want)
	}
	// The row bytes after the subOp byte must be IDENTICAL to the
	// 0x32C4 member-loop bytes for the same record - one layout, one
	// writer (writeGuildMemberRow).
	block := EncodeGuildInfo32C4(enterworld.GuildRecord{ID: 1, Name: "G"}, []enterworld.GuildMemberRecord{member}, online, 0)
	rowLen := len(joinOracle(member, 0)) - 1
	rowIn32C4 := block[len(block)-1-rowLen : len(block)-1]
	if !bytes.Equal(EncodeMemberJoin3B29(member, online)[1:], rowIn32C4) {
		t.Fatalf("subOp-2 row bytes diverge from the 0x32C4 member-loop bytes")
	}
}

// TestInvitePendingTable pins the pending-invitation semantics that run
// without any store or presence collaborator: latest-wins replacement
// (the sub_5c8210 rule), the non-consuming ownership probe, the consume
// on take, and the session-boundary drop.
func TestInvitePendingTable(t *testing.T) {
	t.Parallel()
	r := NewInviteRuntime(&enterworld.Deps{}, nil)
	const division = "global-official"

	if r.HasPendingInvite(division, "Berk") {
		t.Fatalf("fresh table reports a pending invite")
	}
	if r.DropPendingInvite(division, "Berk") {
		t.Fatalf("fresh table dropped a pending invite")
	}
	r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", GuildID: 1})
	r.setPending(division, "Berk", PendingInvite{InviterName: "Cale", GuildID: 2})
	if got := r.PendingInviteCount(); got != 1 {
		t.Fatalf("pending count after replacement = %d, want 1 (latest wins)", got)
	}
	// The key is case-insensitive - the hub bind key convention.
	if !r.HasPendingInvite(division, "bErK") {
		t.Fatalf("ownership probe missed the case-folded key")
	}
	invite, ok := r.takePending(division, "berk")
	if !ok || invite.InviterName != "Cale" || invite.GuildID != 2 {
		t.Fatalf("take = %+v/%v, want the REPLACING invite from Cale", invite, ok)
	}
	if _, ok := r.takePending(division, "Berk"); ok {
		t.Fatalf("second take found a consumed invite")
	}
	r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", GuildID: 1})
	if !r.DropPendingInvite(division, "Berk") || r.PendingInviteCount() != 0 {
		t.Fatalf("session-boundary drop did not clear the invitation")
	}
}

// TestApplyConsentWithoutCommit pins every consent arm that must run
// BEFORE any store or presence collaborator is touched (the runtime
// here has none - reaching further would panic): the no-pending drop,
// the pinned {02 16} refuse consuming the invitation, and the forged
// not-quite-accept shapes refusing rather than committing.
func TestApplyConsentWithoutCommit(t *testing.T) {
	t.Parallel()
	const division = "global-official"
	actor := &enterworld.Character{ID: 9, Name: "Berk"}

	r := NewInviteRuntime(&enterworld.Deps{}, nil)
	// No outstanding invitation: dropped before anything is read.
	r.ApplyConsent(nil, division, actor, ConsentResultAccept, ConsentCodeAccept)

	// The pinned refuse {02 16} consumes the invitation and emits
	// nothing.
	r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", GuildID: 1})
	r.ApplyConsent(nil, division, actor, ConsentResultRefuse, ConsentCodeRefuse)
	if r.PendingInviteCount() != 0 {
		t.Fatalf("refuse left the invitation outstanding")
	}

	// Anything but the EXACT {01 01} accept pair is a refusal.
	for _, forged := range [][2]uint8{{1, 2}, {2, 1}, {0, 0}, {1, 0x16}} {
		r.setPending(division, "Berk", PendingInvite{InviterName: "Alfa", GuildID: 1})
		r.ApplyConsent(nil, division, actor, forged[0], forged[1])
		if r.PendingInviteCount() != 0 {
			t.Fatalf("forged consent {%d %#x} left the invitation outstanding", forged[0], forged[1])
		}
	}
}
