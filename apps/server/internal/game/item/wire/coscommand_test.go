/*
===========================================================================

coscommand_test.go - codec falsifiers for the 0x769E COS command family

These pin the decoded VALUES; the handler tests cover the same bytes
through the refusal plane, and a handler test that only checks "refused"
cannot see a byte-swapped gid.

===========================================================================
*/
package wire

import (
	"bytes"
	"testing"
)

/*
================
TestDecodeCosMountRequestPinsTheClientBytes
================
*/
func TestDecodeCosMountRequestPinsTheClientBytes(t *testing.T) {
	// The exact frame the client-side harness proves the REAL sub_695420
	// mount arm builds (actionPaneEmitChainParity: COS gid 0x00C05001 ->
	// [01 50 C0 00 0B]).
	request, err := DecodeCosMountRequest([]byte{0x01, 0x50, 0xC0, 0x00, 0x0B})
	if err != nil {
		t.Fatalf("the client fixture frame refused: %v", err)
	}
	if request.CosGid != 0x00C05001 {
		t.Errorf("cosGid = 0x%08X, want 0x00C05001 (u32 little-endian before the tag)", request.CosGid)
	}
}

/*
================
TestDecodeCosCommandPinsMountedAttackBytes
================
*/
func TestDecodeCosCommandPinsMountedAttackBytes(t *testing.T) {
	request, err := DecodeCosCommand([]byte{
		0x01, 0x50, 0xC0, 0x00,
		0x02,
		0x78, 0x56, 0x34, 0x12,
	})
	if err != nil {
		t.Fatalf("mounted attack refused: %v", err)
	}
	if request.Tag != CosCommandAttackTag || request.CosGid != 0x00C05001 || request.TargetGid != 0x12345678 {
		t.Fatalf("mounted attack = %+v", request)
	}
	for _, payload := range [][]byte{
		{1, 0x50, 0xC0, 0, 2},
		{1, 0x50, 0xC0, 0, 2, 0, 0, 0, 0, 0},
	} {
		if _, err := DecodeCosCommand(payload); err == nil {
			t.Fatalf("malformed attack % X accepted", payload)
		}
	}
}

/*
================
TestDecodeCosCommandPinsSteerAndStopBytes

The vehicle's stop (tag 0x03, SendAngleUpdatePacket 0x8777B0) and steer
(tag 0x04, SendSteeringUpdate 0x877540) carry one little-endian heading
word after the tag, and nothing else.
================
*/
func TestDecodeCosCommandPinsSteerAndStopBytes(t *testing.T) {
	for _, tag := range []uint8{CosCommandStopTag, CosCommandSteerTag} {
		request, err := DecodeCosCommand([]byte{0x01, 0x50, 0xC0, 0x00, tag, 0x34, 0x12})
		if err != nil {
			t.Fatalf("tag 0x%02X refused: %v", tag, err)
		}
		if request.Tag != tag || request.CosGid != 0x00C05001 || request.Heading != 0x1234 {
			t.Fatalf("tag 0x%02X = %+v", tag, request)
		}
		for _, payload := range [][]byte{
			{0x01, 0x50, 0xC0, 0x00, tag},
			{0x01, 0x50, 0xC0, 0x00, tag, 0x34},
			{0x01, 0x50, 0xC0, 0x00, tag, 0x34, 0x12, 0x00},
		} {
			if _, err := DecodeCosCommand(payload); err == nil {
				t.Errorf("malformed % X accepted", payload)
			}
		}
	}
}

/*
================
TestCosServerPayloadsPinWidthsAndOrdering
================
*/
func TestCosServerPayloadsPinWidthsAndOrdering(t *testing.T) {
	wantRide := []byte{1, 0xA3, 0x86, 0x01, 0x00, 1, 3, 0, 0xC0, 0}
	gotRide := EncodeCosRideState(100003, true, 0x00C00003)
	if string(gotRide) != string(wantRide) {
		t.Fatalf("B4B5 = % X, want % X", gotRide, wantRide)
	}
}

/*
================
TestDecodeCosMountRequestSplitsMalformedFromUnsupported
================
*/
func TestDecodeCosMountRequestSplitsMalformedFromUnsupported(t *testing.T) {
	// Wrong LENGTH is malformed...
	for _, payload := range [][]byte{{}, {0x0B}, {0x01, 0x50, 0xC0, 0x00}, {0x01, 0x50, 0xC0, 0x00, 0x0B, 0x00}} {
		if _, err := DecodeCosMountRequest(payload); err == nil {
			t.Errorf("payload % X accepted, want length error", payload)
		}
	}
	// ...while a 5-byte body with a NON-MOUNT family tag is a distinct
	// not-a-mount refusal (01 move, 02 attack, 03 stop and 04 steer have
	// their own lengths; 08 pickup is not decoded).
	for _, tag := range []uint8{0x01, 0x02, 0x03, 0x04, 0x08, 0x00, 0xFF} {
		if _, err := DecodeCosMountRequest([]byte{0x01, 0x50, 0xC0, 0x00, tag}); err == nil {
			t.Errorf("tag 0x%02X accepted, want unsupported-form error", tag)
		}
	}
}

/*
================
TestCosSummonTimer3691PinsSubtypeAndWidths

The 0x3691 subtype-3 body sub_775f20 @0x7760FE reads as three u32s after
the subtype byte, little-endian, with no tail.
================
*/
func TestCosSummonTimer3691PinsSubtypeAndWidths(t *testing.T) {
	got := EncodeCosSummonTimer3691(0x00003039, 40320, 0x000A0005)
	want := []byte{
		3,
		0x39, 0x30, 0x00, 0x00,
		0x80, 0x9D, 0x00, 0x00,
		0x05, 0x00, 0x0A, 0x00,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("0x3691 subtype 3 = % X, want % X", got, want)
	}
	if OpCosStateRefresh != 0x3691 || CosStateRefreshSummonTimer != 3 {
		t.Fatalf("opcode/subtype drifted: 0x%X/%d", OpCosStateRefresh, CosStateRefreshSummonTimer)
	}
	// The zero pair is sub_6E6150's REMOVE selector, not an empty window.
	if retire := EncodeCosSummonTimerRetire3691(0x00003039); !bytes.Equal(retire, []byte{3, 0x39, 0x30, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("retire = % X", retire)
	}
}

/*
================
TestCosSpawnRidingHorseOmitsOwnerBlock

sub_8554e0 reads the owner name, hold, PvP state and owner gid for COS
bands 2-6 only: a riding horse's spawn ends with its name and the state
byte, while a transport carries the full owner tail.
================
*/
func TestCosSpawnRidingHorseOmitsOwnerBlock(t *testing.T) {
	row := CosSpawnBand2{RefObjID: 3915, Gid: 7, Name: "Horse", OwnerName: "Rider", OwnerGid: 9, State: 3}
	row.Band = 1
	horse := EncodeCosSpawnBand2(row)
	if !bytes.HasSuffix(horse, append([]byte{5, 0}, append([]byte("Horse"), 3)...)) {
		t.Fatalf("band-1 tail = % x", horse[len(horse)-8:])
	}
	row.Band = 2
	transport := EncodeCosSpawnBand2(row)
	// Owner name (2+5), hold byte, PvP byte and owner gid (4).
	if len(transport)-len(horse) != 2+5+1+1+4 {
		t.Fatalf("band-2 spawn is %d bytes, band-1 %d", len(transport), len(horse))
	}
}
