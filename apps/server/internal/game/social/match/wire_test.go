package match

// Byte-exact pins for the match wire codec. The fixture builders mirror
// the re-harness matchingWindowListingParity.test.ts builders (the same
// synthetic packets the REAL client folds consume), so the Go encoders
// and the client parsers agree on every byte, including the party row's
// flag0b-BEFORE-flag0a swap and the mentor row's read-and-dropped u32.

import (
	"bytes"
	"testing"
)

func u16le(v uint16) []byte {
	return []byte{byte(v), byte(v >> 8)}
}

func u32le(v uint32) []byte {
	return []byte{byte(v), byte(v >> 8), byte(v >> 16), byte(v >> 24)}
}

// narrowStr renders the CGInterface_Calls_CMsgStreamBuffer_WriteBytes sized-ANSI layout.
func narrowStr(s string) []byte {
	out := u16le(uint16(len(s)))
	return append(out, []byte(s)...)
}

// wideStr renders the CMsgStreamBuffer_ReadSizedUTF16String sized-wide layout (BMP-only fixtures).
func wideStr(s string) []byte {
	runes := []rune(s)
	out := u16le(uint16(len(runes)))
	for _, r := range runes {
		out = append(out, byte(r), byte(uint16(r)>>8))
	}
	return out
}

func concat(chunks ...[]byte) []byte {
	var out []byte
	for _, chunk := range chunks {
		out = append(out, chunk...)
	}
	return out
}

func TestDecodePartyMatchRequestPinsTheSub703850Body(t *testing.T) {
	payload := concat(
		u32le(0), u32le(1001),
		[]byte{2, 3, 15, 80},
		wideStr("fresh party"),
	)
	request, err := DecodePartyMatchRequest(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := PartyMatchRequest{
		EntryID: 0, PartyNumber: 1001,
		TypeBits: 2, Purpose: 3, MinLevel: 15, MaxLevel: 80,
		Title: "fresh party",
	}
	if request != want {
		t.Fatalf("decoded %+v, want %+v", request, want)
	}
}

func TestDecodePartyMatchRequestRefusesTrailingBytes(t *testing.T) {
	payload := concat(u32le(0), u32le(1), []byte{0, 0, 0, 0}, wideStr("x"), []byte{0xFF})
	if _, err := DecodePartyMatchRequest(payload); err == nil {
		t.Fatal("trailing byte accepted")
	}
}

func TestDecodePartyMatchRequestRefusesShortWideString(t *testing.T) {
	payload := concat(u32le(0), u32le(1), []byte{0, 0, 0, 0}, u16le(4), []byte{0x61, 0x00})
	if _, err := DecodePartyMatchRequest(payload); err == nil {
		t.Fatal("cut-short wide string accepted")
	}
}

func TestDecodeMentorMatchRequestPinsTheSub7038f0Body(t *testing.T) {
	payload := concat(u32le(0), u32le(9), []byte{2}, wideStr("new entry"))
	request, err := DecodeMentorMatchRequest(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	want := MentorMatchRequest{EntryID: 0, Dword04: 9, Kind: 2, Detail: "new entry"}
	if request != want {
		t.Fatalf("decoded %+v, want %+v", request, want)
	}
}

func TestDecodeDeleteAndPageRequests(t *testing.T) {
	entryID, err := DecodeDeleteRequest(u32le(77))
	if err != nil || entryID != 77 {
		t.Fatalf("delete decode = (%d, %v), want (77, nil)", entryID, err)
	}
	if _, err := DecodeDeleteRequest([]byte{1, 2}); err == nil {
		t.Fatal("short delete body accepted")
	}
	page, err := DecodePageRequest([]byte{3})
	if err != nil || page != 3 {
		t.Fatalf("page decode = (%d, %v), want (3, nil)", page, err)
	}
	if _, err := DecodePageRequest([]byte{3, 0}); err == nil {
		t.Fatal("oversized page body accepted")
	}
}

func TestEncodePartyRegAckPinsTheSub75dc40Blob(t *testing.T) {
	// The same values the harness feeds CPSMission_OnPartyMatchingRecordResponse0xB6FF: flag 1, {u32 77,
	// u32 1234, 02 03 0F 50, wide "fresh party"}.
	got := EncodePartyRegAck(PartyEntry{
		EntryID: 77, PartyNumber: 1234,
		TypeBits: 2, Purpose: 3, MinLevel: 15, MaxLevel: 80,
		Title: "fresh party",
	})
	want := concat([]byte{1}, u32le(77), u32le(1234), []byte{2, 3, 15, 80}, wideStr("fresh party"))
	if !bytes.Equal(got, want) {
		t.Fatalf("0xB6FF blob = % X, want % X", got, want)
	}
}

func TestEncodeDeleteAckPinsTheFlagPlusId(t *testing.T) {
	got := EncodeDeleteAck(55)
	want := concat([]byte{1}, u32le(55))
	if !bytes.Equal(got, want) {
		t.Fatalf("delete ack = % X, want % X", got, want)
	}
}

func TestEncodePartyListingB588PinsTheSub75e5e0Rows(t *testing.T) {
	// Mirrors the harness 0xB588 packet: header [01 03 07 03] then three
	// rows with flag0b (typeBits) BEFORE flag0a (purpose) on the wire.
	rows := []PartyEntry{
		{EntryID: 11, PartyNumber: 1001, MasterName: "Hero", RaceByte: 1, MemberCount: 2, TypeBits: 1, Purpose: 0, MinLevel: 20, MaxLevel: 60, Title: "my own party"},
		{EntryID: 22, PartyNumber: 1002, MasterName: "Alice", RaceByte: 0, MemberCount: 3, TypeBits: 0, Purpose: 2, MinLevel: 1, MaxLevel: 40, Title: "trade run"},
		{EntryID: 33, PartyNumber: 1003, MasterName: "Bob", RaceByte: 1, MemberCount: 8, TypeBits: 1, Purpose: 1, MinLevel: 30, MaxLevel: 90, Title: "quest grind"},
	}
	got := EncodePartyListingB588(3, 7, rows)
	want := concat(
		[]byte{1, 3, 7, 3},
		u32le(11), u32le(1001), narrowStr("Hero"), []byte{1, 2, 1, 0, 20, 60}, wideStr("my own party"),
		u32le(22), u32le(1002), narrowStr("Alice"), []byte{0, 3, 0, 2, 1, 40}, wideStr("trade run"),
		u32le(33), u32le(1003), narrowStr("Bob"), []byte{1, 8, 1, 1, 30, 90}, wideStr("quest grind"),
	)
	if !bytes.Equal(got, want) {
		t.Fatalf("0xB588 page = % X, want % X", got, want)
	}
}

func TestEncodePartyListingB588EmptyPage(t *testing.T) {
	got := EncodePartyListingB588(1, 1, nil)
	want := []byte{1, 1, 1, 0}
	if !bytes.Equal(got, want) {
		t.Fatalf("empty 0xB588 = % X, want % X (clears rows AND the own snapshot client-side)", got, want)
	}
}

func TestEncodeMentorRegisterAckB55DPinsTheTrailingDword14(t *testing.T) {
	// Mirrors the harness 0xB55D packet: {u32 777, u32 0, 02, wide
	// "new entry", u32 42} - the trailing u32 lands in head dword14.
	got := EncodeMentorRegisterAckB55D(MentorEntry{
		EntryID: 777, Dword04: 0, Kind: 2, Detail: "new entry", Dword14: 42,
	})
	want := concat([]byte{1}, u32le(777), u32le(0), []byte{2}, wideStr("new entry"), u32le(42))
	if !bytes.Equal(got, want) {
		t.Fatalf("0xB55D blob = % X, want % X", got, want)
	}
}

func TestEncodeMentorModifyAckB13EDropsTheTrailingDword(t *testing.T) {
	got := EncodeMentorModifyAckB13E(MentorEntry{
		EntryID: 777, Dword04: 9, Kind: 1, Detail: "edited", Dword14: 42,
	})
	want := concat([]byte{1}, u32le(777), u32le(9), []byte{1}, wideStr("edited"))
	if !bytes.Equal(got, want) {
		t.Fatalf("0xB13E blob = % X, want % X", got, want)
	}
}

func TestEncodeMentorListingB701PinsTheSub769720Rows(t *testing.T) {
	// Mirrors the harness 0xB701 row shape: id, DROPPED u32 (always 0
	// from this server), kind, wide detail, dword04, u8 dword08, byte09,
	// refObjId, narrow requester, dword14, u8 dword20, dword18, dword1c.
	rows := []MentorEntry{
		{EntryID: 501, Kind: 1, Detail: "my camp", Dword04: 9, LevelAlt: 4, Level: 60, RefObjID: 1907, Requester: "Hero", Dword14: 3, Grade: 2, Dword18: 1, Dword1C: 1},
		{EntryID: 502, Kind: 2, Detail: "join us", Dword04: 8, LevelAlt: 60, Level: 60, RefObjID: 1907, Requester: "Alice", Dword14: 5, Grade: 4, Dword18: 2, Dword1C: 0},
	}
	got := EncodeMentorListingB701(2, 5, rows)
	want := concat(
		[]byte{1, 2, 5, 2},
		u32le(501), u32le(0), []byte{1}, wideStr("my camp"), u32le(9), []byte{4, 60}, u32le(1907), narrowStr("Hero"), u32le(3), []byte{2}, u32le(1), u32le(1),
		u32le(502), u32le(0), []byte{2}, wideStr("join us"), u32le(8), []byte{60, 60}, u32le(1907), narrowStr("Alice"), u32le(5), []byte{4}, u32le(2), u32le(0),
	)
	if !bytes.Equal(got, want) {
		t.Fatalf("0xB701 page = % X, want % X", got, want)
	}
}

func TestWideStringRoundTripsNonAscii(t *testing.T) {
	title := "\uAC00\uB098 party \u00E9"
	payload := concat(u32le(0), u32le(0), []byte{0, 0, 0, 0}, wideStr(title))
	request, err := DecodePartyMatchRequest(payload)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if request.Title != title {
		t.Fatalf("title round trip = %q, want %q", request.Title, title)
	}
	encoded := EncodePartyRegAck(PartyEntry{Title: title})
	if !bytes.Contains(encoded, wideStr(title)) {
		t.Fatalf("encoded ack does not carry the UTF-16LE title: % X", encoded)
	}
}
