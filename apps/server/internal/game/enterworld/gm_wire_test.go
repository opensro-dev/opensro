package enterworld

import (
	"bytes"
	"testing"
)

// TestBuildLocalPlayerEntryPayloadGMPrivilegeByte pins the GM privilege
// leg of the 0x32B3 wire contract the client agent builds against: the
// byte the local full-character deserialize (client 8675F0) deposits at
// CICPlayer+0x1890 (@0x867a90, right after the u32 +0x1894 zero dword),
// whose bit 0 client 862A70 tests. A non-GM emits 0x00 (the frozen
// emission), a GM emits exactly 0x01 (bit 0 set, bit 1 - the
// PC-room event client 862A80 gate - and all others zero), and NO other
// byte of the payload moves (differential identity, the chunk-C test's
// posture).
func TestBuildLocalPlayerEntryPayloadGMPrivilegeByte(t *testing.T) {
	character := chinaSpearman()
	entry := ResolveLocalPlayerEntry(character, testRoster())
	// A recognizable mask value makes the position pin content-addressed
	// instead of relying on zeros matching zeros.
	const eventGuideStateMask = uint32(0x0D0C0B0A)
	plain := BuildLocalPlayerEntryPayload(character, &entry, eventGuideStateMask, nil)

	character.GMPrivilege = true
	gm := BuildLocalPlayerEntryPayload(character, &entry, eventGuideStateMask, nil)

	if len(gm) != len(plain) {
		t.Fatalf("GM payload length %d != non-GM length %d - the privilege bit must not move any byte", len(gm), len(plain))
	}
	divergence := -1
	for i := range plain {
		if plain[i] != gm[i] {
			if divergence >= 0 {
				t.Fatalf("second divergence at %d (first at %d) - a byte other than the privilege byte changed", i, divergence)
			}
			divergence = i
		}
	}
	if divergence < 0 {
		t.Fatal("GM payload is byte-identical to the non-GM payload - the privilege bit never emitted")
	}
	if plain[divergence] != 0x00 {
		t.Fatalf("non-GM privilege byte = %#02x, want 0x00", plain[divergence])
	}
	if gm[divergence] != 0x01 {
		t.Fatalf("GM privilege byte = %#02x, want exactly 0x01 (bit 0 only)", gm[divergence])
	}

	// Offset pin: with the fixture's empty whisper-block and event-group
	// lists the privilege byte sits exactly 17 bytes from the payload
	// end (9 tail scalar bytes + chunk-C count + fortress u32 + event
	// count + mission byte follow it).
	if want := len(plain) - 17; divergence != want {
		t.Fatalf("privilege byte at offset %d, want %d (len-17)", divergence, want)
	}
	// Content pin, preceding bytes: u32 eventGuideStateMask LE then the
	// character JID the client reads into +0x1894.
	wantBefore := []byte{0x0A, 0x0B, 0x0C, 0x0D, 0xA0, 0x86, 0x01, 0x00}
	if !bytes.Equal(plain[divergence-8:divergence], wantBefore) {
		t.Fatalf("bytes before the privilege byte = % x, want mask LE + character JID % x",
			plain[divergence-8:divergence], wantBefore)
	}
	// Golden tail after the privilege byte: u8 7, u8 0, three u16 zeros,
	// u8 0, chunk-C count 0, fortress sentinel 0x10001, event count 0,
	// mission byte 0 - identical on both payloads (nothing after the
	// privilege byte may shift).
	wantTail := []byte{
		0x07, 0x00,
		0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
		0x00,
		0x01, 0x00, 0x01, 0x00,
		0x00,
		0x00,
	}
	if !bytes.Equal(gm[divergence+1:], wantTail) {
		t.Fatalf("payload tail after the privilege byte = % x, want % x", gm[divergence+1:], wantTail)
	}
}

func TestEntryEligibilityBitsAndSavedPotionTail(t *testing.T) {
	c := chinaSpearman()
	entry := ResolveLocalPlayerEntry(c, testRoster())
	if _, err := HandleQuickSlotMessage(&Deps{}, c, []byte{2, 0x11, 0xb2, 0x12, 0xb2, 0x13, 0x80, 0x8a}); err != nil {
		t.Fatal(err)
	}
	for bits := 0; bits < 4; bits++ {
		c.GMPrivilege = bits&1 != 0
		c.PCRoomEvent = bits&2 != 0
		p := BuildLocalPlayerEntryPayload(c, &entry, 0x12345678, nil)
		want := []byte{byte(bits), 7, 0, 0x11, 0xb2, 0x12, 0xb2, 0x13, 0x80, 0x8a, 0, 1, 0, 1, 0, 0, 0}
		if !bytes.Equal(p[len(p)-len(want):], want) {
			t.Fatalf("eligibility %d tail: % x", bits, p[len(p)-len(want):])
		}
	}
}
