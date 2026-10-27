package community

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

// TestEncodeFriendRoster3769 pins the client's roster read layout:
// u8 count,
// per friend {u32 jid, u16 len + ANSI name, u32 modelRefId, u8 state}.
func TestEncodeFriendRoster3769(t *testing.T) {
	if got := EncodeFriendRoster3769(nil); !bytes.Equal(got, []byte{0x00}) {
		t.Fatalf("empty roster = % x, want the single 0x00 count byte", got)
	}
	got := EncodeFriendRoster3769([]FriendRosterEntry{
		{JID: 0x0a0b0c0d, Name: "Berk", ModelRefID: 1907, State: 1},
	})
	want := []byte{
		0x01,                   // u8 count
		0x0d, 0x0c, 0x0b, 0x0a, // u32 jid LE
		0x04, 0x00, 'B', 'e', 'r', 'k', // u16 len + ANSI name
		0x73, 0x07, 0x00, 0x00, // u32 modelRefId 1907 LE
		0x01, // u8 state (offline)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("roster row = % x, want % x", got, want)
	}
}

// TestEncodeLetterListB3CD pins the client
// CPSMission_OnLetterListResponse0xB3CD /
// CharacterDependentData_LoadLetterList read layout:
// u8 result 1, u8 count, per letter {u16 len + ANSI sender, u32
// senderModelRefId, u32 packedReceiveTime, u8 readFlag}.
func TestEncodeLetterListB3CD(t *testing.T) {
	if got := EncodeLetterListB3CD(nil); !bytes.Equal(got, []byte{0x01, 0x00}) {
		t.Fatalf("empty list = % x, want [01 00]", got)
	}
	got := EncodeLetterListB3CD([]LetterListEntry{
		{Sender: "Cale", SenderModelRefID: 14726, PackedReceiveTime: 0x01020304, ReadFlag: 1},
	})
	want := []byte{
		0x01,       // u8 result LIST
		0x01,       // u8 count
		0x04, 0x00, // u16 sender len
		'C', 'a', 'l', 'e',
		0x86, 0x39, 0x00, 0x00, // u32 modelRefId 14726 LE
		0x04, 0x03, 0x02, 0x01, // u32 packedReceiveTime LE
		0x01, // u8 readFlag
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("letter row = % x, want % x", got, want)
	}
}

// TestSeedFrames pins the enter-world seed pair: the friend roster push
// (empty for an edge-less character) then the empty letter list answer.
func TestSeedFrames(t *testing.T) {
	frames := SeedFrames("global-official", &enterworld.Character{Name: "asd2"})
	if len(frames) != 2 {
		t.Fatalf("seed frames = %d, want 2", len(frames))
	}
	if frames[0].NativeOpcode != OpFriendRosterPush {
		t.Errorf("frame[0] opcode = %#x, want 0x3769", frames[0].NativeOpcode)
	}
	if len(frames[0].Payload) != 1 || frames[0].Payload[0] != 0 {
		t.Errorf("frame[0] payload = %v, want [0]", frames[0].Payload)
	}
	if frames[1].NativeOpcode != OpLetterListAnswer {
		t.Errorf("frame[1] opcode = %#x, want 0xB3CD", frames[1].NativeOpcode)
	}
	if len(frames[1].Payload) != 2 || frames[1].Payload[0] != 1 || frames[1].Payload[1] != 0 {
		t.Errorf("frame[1] payload = %v, want [1 0]", frames[1].Payload)
	}
}

// TestDecodeLetterIndexRequest pins the live letter-index strict decode
// (the shared client NetClient_Send70CC / NetClient_Send73F2 body
// {u8 index} the 0x70CC and
// 0x73F2 handlers read): one byte in, no trailing bytes, empty refused.
func TestDecodeLetterIndexRequest(t *testing.T) {
	if index, err := DecodeLetterIndexRequest([]byte{0x07}); err != nil || index != 7 {
		t.Errorf("letter index decode = %d, %v", index, err)
	}
	if _, err := DecodeLetterIndexRequest(nil); err == nil {
		t.Error("letter index empty payload accepted")
	}
	if _, err := DecodeLetterIndexRequest([]byte{0x07, 0x00}); err == nil {
		t.Error("letter index trailing byte accepted")
	}
}
