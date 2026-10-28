package enterworld

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/testsupport/entryauth"
	"opensro.online/server/internal/transport"
)

// TestBuildLocalPlayerEntryPayloadChunkCWhisperBlock pins the entered
// chunk-C emission (client fold CPSMission_ReadBlockedWhispererList: u8 count, per name u16 len +
// ANSI bytes) by differential position: the payload with blocked names
// must diverge from the empty-list payload at EXACTLY the chunk-C count
// byte - after the carry-title chunk, immediately before the u32 0x10001
// fortress-war sentinel - carry exactly the encoded names there, and be
// byte-identical everywhere else. An empty list must stay byte-identical
// to the frozen emission (the entered fixture contract).
func TestBuildLocalPlayerEntryPayloadChunkCWhisperBlock(t *testing.T) {
	character := chinaSpearman()
	entry := ResolveLocalPlayerEntry(character, testRoster())
	empty := BuildLocalPlayerEntryPayload(character, &entry, 0, nil)

	character.BlockedWhisperers = []string{"Berk", "Cale"}
	withNames := BuildLocalPlayerEntryPayload(character, &entry, 0, nil)

	divergence := -1
	for i := 0; i < len(empty) && i < len(withNames); i++ {
		if empty[i] != withNames[i] {
			divergence = i
			break
		}
	}
	if divergence < 0 {
		t.Fatal("payload with blocked names did not diverge from the empty-list payload")
	}
	if empty[divergence] != 0x00 {
		t.Fatalf("empty payload byte at divergence = %#02x, want the 0x00 chunk-C count", empty[divergence])
	}

	// The chunk-C position pin: the EMPTY payload's next four bytes are
	// the 0x10001 fortress-war sentinel the client reads right after the
	// string list.
	sentinel := []byte{0x01, 0x00, 0x01, 0x00}
	if !bytes.Equal(empty[divergence+1:divergence+5], sentinel) {
		t.Fatalf("bytes after the empty chunk-C count = % x, want the fortress sentinel % x",
			empty[divergence+1:divergence+5], sentinel)
	}

	wantInsert := []byte{0x02}
	for _, name := range character.BlockedWhisperers {
		nameBytes := []byte(name)
		wantInsert = append(wantInsert, byte(len(nameBytes)), byte(len(nameBytes)>>8))
		wantInsert = append(wantInsert, nameBytes...)
	}
	if !bytes.Equal(withNames[divergence:divergence+len(wantInsert)], wantInsert) {
		t.Fatalf("chunk-C bytes = % x, want % x",
			withNames[divergence:divergence+len(wantInsert)], wantInsert)
	}
	if !bytes.Equal(withNames[divergence+len(wantInsert):], empty[divergence+1:]) {
		t.Fatal("payload tail after chunk C drifted - the insert desynced the entered stream")
	}
	if len(withNames) != len(empty)+len(wantInsert)-1 {
		t.Fatalf("payload grew by %d byte(s), want %d", len(withNames)-len(empty), len(wantInsert)-1)
	}
}

// TestHandleEnterWorldAppendsCommunitySeeds pins the seam contract: the
// CommunitySeedFramesFor packets ride AFTER the bootstrap frame sequence
// on the transport outcome, and Build's own Packets (the frozen fixture
// contract) stay untouched.
func TestHandleEnterWorldAppendsCommunitySeeds(t *testing.T) {
	character := chinaSpearman()
	character.ID = 7
	deps := testDeps(character)
	deps.ResolveDivisionID = DevResolveDivisionIDFromCatalog(deps.Characters)
	seedCalls := 0
	seedDivision := ""
	deps.CommunitySeedFramesFor = func(divisionID string, c *Character) []Packet {
		seedCalls++
		seedDivision = divisionID
		if c == nil || c.Name != "asd2" {
			t.Fatalf("seed seam received character %+v, want the asd2 snapshot", c)
		}
		return []Packet{
			NewPacket(0x3769, []byte{0x00}),
			NewPacket(0xB3CD, []byte{0x01, 0x00}),
		}
	}

	payload := transport.EncodeEnterWorld(entryauth.NewAuthenticatedEntryFixture(t, "0", "asd2"))
	outcome := HandleEnterWorld(deps, payload)
	if !outcome.OK {
		t.Fatalf("enter world failed: %+v", outcome.Result)
	}
	if seedCalls != 1 {
		t.Fatalf("seed seam ran %d time(s), want 1", seedCalls)
	}
	if seedDivision != outcome.DivisionID {
		t.Fatalf("seed seam received division %q, want the outcome's %q", seedDivision, outcome.DivisionID)
	}
	if len(outcome.Frames) != len(outcome.Result.Packets)+2 {
		t.Fatalf("outcome frames = %d, want the %d bootstrap packets + 2 seeds",
			len(outcome.Frames), len(outcome.Result.Packets))
	}
	for index, packet := range outcome.Result.Packets {
		if outcome.Frames[index].NativeOpcode != packet.NativeOpcode {
			t.Fatalf("frame[%d] opcode = %#x, want the bootstrap packet %#x",
				index, outcome.Frames[index].NativeOpcode, packet.NativeOpcode)
		}
	}
	seedStart := len(outcome.Result.Packets)
	if outcome.Frames[seedStart].NativeOpcode != 0x3769 || outcome.Frames[seedStart+1].NativeOpcode != 0xB3CD {
		t.Fatalf("seed frames = %#x/%#x, want 0x3769/0xB3CD",
			outcome.Frames[seedStart].NativeOpcode, outcome.Frames[seedStart+1].NativeOpcode)
	}
	for _, packet := range outcome.Result.Packets {
		if packet.NativeOpcode == 0x3769 || packet.NativeOpcode == 0xB3CD {
			t.Fatal("seed frames leaked into Build's Packets (the frozen fixture contract)")
		}
	}
}
