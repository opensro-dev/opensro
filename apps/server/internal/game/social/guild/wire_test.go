package guild

import (
	"testing"

	"opensro.online/server/internal/game/item/wire"
)

// sizedString appends the {u16 byte length, ANSI bytes} layout.
func sizedString(writer *wire.Writer, value string) {
	writer.U16(uint16(len(value)))
	writer.Bytes([]byte(value))
}

func invitePayload(targetRef uint32) []byte {
	writer := wire.NewWriter(4)
	writer.U32(targetRef)
	return writer.Payload()
}

func kickPayload(name string) []byte {
	writer := wire.NewWriter(2 + len(name))
	sizedString(writer, name)
	return writer.Payload()
}

func nameGrantPayload(targetRef uint32, name string) []byte {
	writer := wire.NewWriter(6 + len(name))
	writer.U32(targetRef)
	sizedString(writer, name)
	return writer.Payload()
}

func positionGrantPayload(targetRef uint32, position uint8) []byte {
	writer := wire.NewWriter(5)
	writer.U32(targetRef)
	writer.U8(position)
	return writer.Payload()
}

func noticeEditPayload(subject, contents string) []byte {
	writer := wire.NewWriter(4 + len(subject) + len(contents))
	sizedString(writer, subject)
	sizedString(writer, contents)
	return writer.Payload()
}

func createPayload(selectedTargetGid uint32, name string) []byte {
	writer := wire.NewWriter(6 + len(name))
	writer.U32(selectedTargetGid)
	sizedString(writer, name)
	return writer.Payload()
}

// TestDecodeGpDonateRequest pins the structured 0x740F decoder
// {u32 amount} (the NetClient_Send740F composer shape - the last
// opcode to
// leave the old string-detail refuse table when its mutator landed).
func TestDecodeGpDonateRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodeGpDonateRequest(invitePayload(120))
	if err != nil || ok.Amount != 120 {
		t.Fatalf("gp-donate decode = %+v (%v), want amount 120", ok, err)
	}
	for name, malformed := range map[string][]byte{
		"nil":      nil,
		"short":    {0x0B},
		"trailing": append(invitePayload(11), 0x00),
	} {
		if _, err := DecodeGpDonateRequest(malformed); err == nil {
			t.Errorf("%s: malformed gp-donate body % X decoded without error", name, malformed)
		}
	}
}

// TestEncodeGpDonateFrames pins the three donate frames byte-for-byte
// against hand-rolled oracles: the 0xB40F ack {u8 1}{u32 amount} (the
// display-only echo the client CNetProcess_OnResponse0xB40F guide
// line formats), the subOp-5
// &0x08 guild-GP delta and the subOp-6 &0x08 donated-GP delta (the
// exclusive single-bit masks in the pinned layouts - client
// CPSMission_OnGuildUpdate0x3B29 slot 4 / slot 5 +
// CIFGuildMember_OnMemberUpdated &0x08).
func TestEncodeGpDonateFrames(t *testing.T) {
	t.Parallel()
	jidLE := []byte{0x0D, 0x0C, 0x0B, 0x0A}

	wantAck := []byte{0x01, 0x78, 0x00, 0x00, 0x00}
	if got := EncodeGpDonateAckB40F(120); !bytesEqual(got, wantAck) {
		t.Errorf("0xB40F = % X, want % X", got, wantAck)
	}

	wantGuildGp := []byte{0x05, 0x08, 0xD2, 0x04, 0x00, 0x00}
	if got := EncodeGuildGp3B29(1234); !bytesEqual(got, wantGuildGp) {
		t.Errorf("subOp-5 &0x08 = % X, want % X", got, wantGuildGp)
	}

	wantDonated := append(append([]byte{0x06}, jidLE...), 0x08, 0x2C, 0x01, 0x00, 0x00)
	if got := EncodeMemberDonatedGp3B29(0x0A0B0C0D, 300); !bytesEqual(got, wantDonated) {
		t.Errorf("subOp-6 &0x08 = % X, want % X", got, wantDonated)
	}
}

// TestDecodeBreakRequest pins the structured 0x766E decoder - the
// leave body shape (the NetClient_SendSelectedTarget766E composer
// writes exactly 4 bytes
// from the +0x620 slot).
func TestDecodeBreakRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodeBreakRequest(invitePayload(0x00C40007))
	if err != nil || ok.SelectedTargetGid != 0x00C40007 {
		t.Fatalf("break decode = %+v (%v), want gid 0x%08X", ok, err, 0x00C40007)
	}
	for name, malformed := range map[string][]byte{
		"nil":      nil,
		"short":    {0x01, 0x00},
		"trailing": append(invitePayload(1), 0x00),
	} {
		if _, err := DecodeBreakRequest(malformed); err == nil {
			t.Errorf("%s: malformed break body % X decoded without error", name, malformed)
		}
	}
}

// TestDecodeNameGrantRequest pins the structured 0x72BC decoder
// {u32 targetJid}{u16-len ANSI name} (the NetClient_SendIDAndText72BC
// composer shape).
func TestDecodeNameGrantRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodeNameGrantRequest(nameGrantPayload(7, "Cale"))
	if err != nil || ok.TargetJID != 7 || ok.GrantName != "Cale" {
		t.Fatalf("name-grant decode = %+v (%v), want jid 7 name Cale", ok, err)
	}
	for name, malformed := range map[string][]byte{
		"nil":        nil,
		"short ref":  {0x07, 0x00},
		"short name": {0x07, 0x00, 0x00, 0x00, 0x04, 0x00, 'C'},
		"trailing":   append(nameGrantPayload(7, "Cale"), 0x00),
	} {
		if _, err := DecodeNameGrantRequest(malformed); err == nil {
			t.Errorf("%s: malformed name-grant body % X decoded without error", name, malformed)
		}
	}
}

// TestDecodePositionGrantRequest pins the structured 0x765F decoder
// {u32 targetJid}{u8 position} (the NetClient_Send765F composer shape).
func TestDecodePositionGrantRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodePositionGrantRequest(positionGrantPayload(9, 2))
	if err != nil || ok.TargetJID != 9 || ok.Position != 2 {
		t.Fatalf("position-grant decode = %+v (%v), want jid 9 position 2", ok, err)
	}
	for name, malformed := range map[string][]byte{
		"nil":      nil,
		"short":    invitePayload(9),
		"trailing": append(positionGrantPayload(9, 2), 0x00),
	} {
		if _, err := DecodePositionGrantRequest(malformed); err == nil {
			t.Errorf("%s: malformed position-grant body % X decoded without error", name, malformed)
		}
	}
}

// TestEncodeBreakFrames pins the break answer bytes: the 0xB66E success
// body is exactly {01} (the client CPSMission_OnGuildDisbandResponse0xB66E
// result-1 arm), and the subOp-1 dissolve announce is exactly {01}
// too - the client's break arm (client CPSMission_OnGuildUpdate0x3B29
// case 1) reads ZERO wire fields beyond the
// subOp byte, so ANY trailing byte would be an invented field.
func TestEncodeBreakFrames(t *testing.T) {
	t.Parallel()
	if got := EncodeBreakAckB66E(); len(got) != 1 || got[0] != 1 {
		t.Errorf("0xB66E ack = % X, want [01]", got)
	}
	if got := EncodeGuildBreak3B29(); len(got) != 1 || got[0] != 1 {
		t.Errorf("subOp-1 push = % X, want [01]", got)
	}
}

// TestEncodeGrantFrames pins the four grant frames byte-for-byte
// against hand-rolled oracles: the second-host acks carry the jid
// TWICE (the client reads two dwords into one local - the first is
// discarded; wire.go carries the DECISION), and the subOp-6 deltas
// carry the exclusive single-bit masks 0x20/0x40 in the pinned
// {u8 6}{u32 jid}{u8 mask}{field} layout (client
// CPSMission_OnGuildUpdate0x3B29 case 6 + the
// CIFGuildMember_OnMemberUpdated mask arms).
func TestEncodeGrantFrames(t *testing.T) {
	t.Parallel()
	jid := uint32(0x0A0B0C0D)
	jidLE := []byte{0x0D, 0x0C, 0x0B, 0x0A}

	wantB2BC := append(append(append([]byte{0x01}, jidLE...), jidLE...), 0x04, 0x00, 'W', 'a', 'r', 'd')
	if got := EncodeNameGrantAckB2BC(jid, "Ward"); !bytesEqual(got, wantB2BC) {
		t.Errorf("0xB2BC = % X, want % X", got, wantB2BC)
	}

	wantB65F := append(append(append([]byte{0x01}, jidLE...), jidLE...), 0x02)
	if got := EncodePositionGrantAckB65F(jid, 2); !bytesEqual(got, wantB65F) {
		t.Errorf("0xB65F = % X, want % X", got, wantB65F)
	}

	wantRole := append(append([]byte{0x06}, jidLE...), 0x40, 0x10)
	if got := EncodeMemberFortressRole3B29(jid, 0x10); !bytesEqual(got, wantRole) {
		t.Errorf("subOp-6 &0x40 = % X, want % X", got, wantRole)
	}
}

// bytesEqual avoids importing bytes into the package-internal test.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestDecodeCreateRequest pins the 0x7663 strict decoder:
// {u32 selectedTargetGid}{u16-len ANSI name}. An EMPTY name is
// well-formed at this layer (the length prefix legally says 0 - the
// refusal is the handler's job); short and trailing payloads error.
func TestDecodeCreateRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodeCreateRequest(createPayload(0x00C40001, "NightWatch"))
	if err != nil {
		t.Fatalf("well-formed create decode error: %v", err)
	}
	if ok.SelectedTargetGid != 0x00C40001 || ok.Name != "NightWatch" {
		t.Errorf("decoded = %+v, want gid 0x%08X name NightWatch", ok, 0x00C40001)
	}

	empty, err := DecodeCreateRequest(createPayload(7, ""))
	if err != nil {
		t.Fatalf("empty-name create decode error (must be well-formed at the wire layer): %v", err)
	}
	if empty.Name != "" || empty.SelectedTargetGid != 7 {
		t.Errorf("empty-name decoded = %+v", empty)
	}

	for name, malformed := range map[string][]byte{
		"nil":            nil,
		"short gid":      {0x01, 0x00},
		"missing name":   {0x01, 0x00, 0x00, 0x00},
		"short name len": {0x01, 0x00, 0x00, 0x00, 0x04},
		"short name":     {0x01, 0x00, 0x00, 0x00, 0x04, 0x00, 'N'},
		"trailing":       append(createPayload(1, "N"), 0x00),
	} {
		if _, err := DecodeCreateRequest(malformed); err == nil {
			t.Errorf("%s: malformed create body % X decoded without error", name, malformed)
		}
	}
}

// TestDecodeKickRequest pins the structured 0x74B1 decoder.
func TestDecodeKickRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodeKickRequest(kickPayload("Berk"))
	if err != nil || ok.MemberName != "Berk" {
		t.Fatalf("kick decode = %+v (%v), want MemberName Berk", ok, err)
	}
	for name, malformed := range map[string][]byte{
		"nil":          nil,
		"short header": {0x04},
		"short body":   {0x04, 0x00, 'B'},
		"trailing":     append(kickPayload("Berk"), 0x00),
	} {
		if _, err := DecodeKickRequest(malformed); err == nil {
			t.Errorf("%s: malformed kick body % X decoded without error", name, malformed)
		}
	}
}

// TestDecodeNoticeEditRequest pins the structured 0x777A decoder.
func TestDecodeNoticeEditRequest(t *testing.T) {
	t.Parallel()
	ok, err := DecodeNoticeEditRequest(noticeEditPayload("Subject", "Contents"))
	if err != nil || ok.Subject != "Subject" || ok.Contents != "Contents" {
		t.Fatalf("notice decode = %+v (%v), want Subject/Contents", ok, err)
	}
	if wide, err := DecodeNoticeEditRequest(noticeEditPayload("Ünïcode", "é")); err != nil || wide.Subject != "Ünïcode" {
		t.Fatalf("valid UTF-8 notice = %+v (%v)", wide, err)
	}
	for name, malformed := range map[string][]byte{
		"nil":              nil,
		"missing contents": kickPayload("Subject"),
		"short contents":   append(kickPayload("Subject"), 0x08, 0x00, 'C'),
		"trailing":         append(noticeEditPayload("S", "C"), 0x00),
		// Notices replay to every member at entry: only valid UTF-8 is stored.
		"invalid subject":  {0x02, 0x00, 'S', 0xff, 0x01, 0x00, 'C'},
		"invalid contents": {0x01, 0x00, 'S', 0x02, 0x00, 0xc3, 0x28},
	} {
		if _, err := DecodeNoticeEditRequest(malformed); err == nil {
			t.Errorf("%s: malformed notice body % X decoded without error", name, malformed)
		}
	}
}
