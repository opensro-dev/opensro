package party

import (
	"bytes"
	"testing"
)

func u32le(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

func u16le(v uint16) []byte { return []byte{byte(v), byte(v >> 8)} }

func narrowStr(s string) []byte {
	return append(u16le(uint16(len(s))), []byte(s)...)
}

func concat(chunks ...[]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}

func TestDecodePartyInviteRequest(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    PartyInviteRequest
		wantErr bool
	}{
		{
			name:    "valid",
			payload: concat(u32le(100003), []byte{0x07}),
			want:    PartyInviteRequest{TargetRef: 100003, OptionBits: 0x07},
		},
		{
			name:    "short",
			payload: u32le(100003),
			wantErr: true,
		},
		{
			name:    "trailing bytes",
			payload: concat(u32le(100003), []byte{0x07, 0x00}),
			wantErr: true,
		},
		{
			name:    "empty",
			payload: nil,
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodePartyInviteRequest(tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("decoded %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != tc.want {
				t.Fatalf("decoded %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDecodePartyJoinInviteRequest(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    uint32
		wantErr bool
	}{
		{name: "valid", payload: u32le(100007), want: 100007},
		{name: "short", payload: []byte{0x01, 0x02}, wantErr: true},
		{name: "trailing bytes", payload: concat(u32le(1), []byte{0}), wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodePartyJoinInviteRequest(tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("decoded %d, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != tc.want {
				t.Fatalf("decoded %d, want %d", got, tc.want)
			}
		})
	}
}

func TestDecodePartyLeaveRequest(t *testing.T) {
	if err := DecodePartyLeaveRequest(nil); err != nil {
		t.Fatalf("empty body: %v", err)
	}
	if err := DecodePartyLeaveRequest([]byte{}); err != nil {
		t.Fatalf("zero-length body: %v", err)
	}
	if err := DecodePartyLeaveRequest([]byte{0x01}); err == nil {
		t.Fatal("trailing byte decoded, want error")
	}
}

func TestDecodePartyBanishRequest(t *testing.T) {
	got, err := DecodePartyBanishRequest(u32le(100005))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got != 100005 {
		t.Fatalf("decoded %d, want 100005", got)
	}
	if _, err := DecodePartyBanishRequest([]byte{1, 2}); err == nil {
		t.Fatal("short body decoded, want error")
	}
	if _, err := DecodePartyBanishRequest(concat(u32le(1), []byte{0})); err == nil {
		t.Fatal("trailing byte decoded, want error")
	}
}

func TestEncodeInvitationPrompt3393(t *testing.T) {
	got := EncodeInvitationPrompt3393(InvitationTypeParty, 100001)
	want := concat([]byte{0x02}, u32le(100001), []byte{0})
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestDecodeInvitationConsent3393(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		want    InvitationConsent
		wantErr bool
	}{
		{
			name:    "party accept (CIFMessageBox_OnAcceptButton button 1)",
			payload: []byte{0x01, 0x01},
			want:    InvitationConsent{InviteType: 1, Button: ConsentButtonAccept},
		},
		{
			name:    "party refuse (CIFMessageBox_OnRefuseButton button 2)",
			payload: []byte{0x01, 0x02},
			want:    InvitationConsent{InviteType: 1, Button: ConsentButtonRefuse},
		},
		{
			name:    "option-off auto-decline (CPSMission_OnConfirmBoxPrompt0x3393 spilled 0)",
			payload: []byte{0x01, 0x00},
			want:    InvitationConsent{InviteType: 1, Button: 0},
		},
		{
			name:    "guild type decodes (dropped by the handler switch, not the decoder)",
			payload: []byte{0x05, 0x01},
			want:    InvitationConsent{InviteType: 5, Button: 1},
		},
		{name: "short", payload: []byte{0x01}, wantErr: true},
		{name: "empty", payload: nil, wantErr: true},
		{name: "trailing bytes", payload: []byte{0x01, 0x01, 0x00}, wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DecodeInvitationConsent3393(tc.payload)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("decoded %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if got != tc.want {
				t.Fatalf("decoded %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestEncodeCreatePartyAckB0D5(t *testing.T) {
	got := EncodeCreatePartyAckB0D5(100003)
	want := concat([]byte{0x01}, u32le(100003))
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

// memberRowBytes renders the expected MemberMaskFull row in
// CPartyMemberInfo_ReadMasked's
// read order: mask, id, sized name + model dword, level, status
// nibbles, region + int16 x/y/z + war.
func memberRowBytes(row MemberRow) []byte {
	return concat(
		[]byte{MemberMaskFull},
		u32le(row.MemberID),
		narrowStr(row.Name),
		u32le(row.ModelRefID),
		[]byte{row.Level, row.StatusNibbles},
		u16le(row.Region),
		u16le(uint16(row.PosX)),
		u16le(uint16(row.PosY)),
		u16le(uint16(row.PosZ)),
		u32le(row.War),
	)
}

func TestEncodePartyInfo35D6(t *testing.T) {
	rowA := MemberRow{
		MemberID: 100001, Name: "alfa", ModelRefID: 1907, Level: 5,
		StatusNibbles: 0xAA, Region: 0x62A8, PosX: 960, PosY: 20, PosZ: 458,
	}
	rowB := MemberRow{
		MemberID: 100002, Name: "bravo", ModelRefID: 1920, Level: 3,
		StatusNibbles: 0x5A, Region: 0x62A8, PosX: -12, PosY: 0, PosZ: -9,
	}
	got := EncodePartyInfo35D6(100001, PartyOptionExpShare|PartyOptionJoinAnyone, []MemberRow{rowA, rowB})
	want := concat(
		[]byte{0x03},
		u32le(100001),
		[]byte{0x05, 0x02},
		memberRowBytes(rowA),
		memberRowBytes(rowB),
	)
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestEncodePartyJoin3E58(t *testing.T) {
	row := MemberRow{
		MemberID: 100003, Name: "cent", ModelRefID: 1907, Level: 9,
		StatusNibbles: 0xA5, Region: 0x6B4F, PosX: 1205, PosY: 80, PosZ: 396,
	}
	got := EncodePartyJoin3E58(row)
	want := concat([]byte{0x02}, memberRowBytes(row))
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestEncodePartyLeave3E58(t *testing.T) {
	got := EncodePartyLeave3E58(100002, PartyLeaveReasonBooted)
	want := concat([]byte{0x03}, u32le(100002), []byte{0x04})
	if !bytes.Equal(got, want) {
		t.Fatalf("payload = % X, want % X", got, want)
	}
}

func TestEncodePartyBroken3E58(t *testing.T) {
	if got := EncodePartyBroken3E58(); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("payload = % X, want 01 00", got)
	}
}

func TestVitalStatusNibbles(t *testing.T) {
	cases := []struct {
		name         string
		curHP, maxHP int64
		curMP, maxMP int64
		want         uint8
	}{
		{name: "full vitals", curHP: 200, maxHP: 200, curMP: 200, maxMP: 200, want: 0xAA},
		{name: "half hp full mp", curHP: 100, maxHP: 200, curMP: 200, maxMP: 200, want: 0xA5},
		{name: "dead", curHP: 0, maxHP: 200, curMP: 0, maxMP: 200, want: 0x00},
		{name: "living floor never zero", curHP: 1, maxHP: 200, curMP: 1, maxMP: 200, want: 0x11},
		{name: "over max clamps", curHP: 999, maxHP: 200, curMP: 999, maxMP: 200, want: 0xAA},
		{name: "zero denominators", curHP: 10, maxHP: 0, curMP: 10, maxMP: 0, want: 0x00},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := VitalStatusNibbles(tc.curHP, tc.maxHP, tc.curMP, tc.maxMP)
			if got != tc.want {
				t.Fatalf("nibbles = %#02x, want %#02x", got, tc.want)
			}
		})
	}
}
