package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

func TestVisualFlagsBeginnerMarkRoundTripPreservesOtherOwnerBits(t *testing.T) {
	character := testCharacter()
	flags := int64(enterworld.VisualFlagBeginner | enterworld.VisualFlagHelper)
	character.VisualFlags = &flags
	rt, _ := newTestRuntime(character, testItems())

	result := rt.HandleVisualFlags(testDivision, character, []byte{enterworld.VisualFlagHelper})
	if len(result.Frames) != 1 || len(result.Broadcast) != 1 {
		t.Fatalf("visual-flags result frames/broadcast = %d/%d, want 1/1", len(result.Frames), len(result.Broadcast))
	}
	want := wire.EncodeVisualFlagsUpdate(
		enterworld.ObjectIDForCharacter(character), enterworld.VisualFlagHelper,
	)
	if result.Frames[0].Opcode != wire.OpVisualFlagsUpdate ||
		!bytes.Equal(result.Frames[0].Payload, want) ||
		!bytes.Equal(result.Broadcast[0].Payload, want) {
		t.Fatalf("visual-flags update = %#v/%#v, want B683 % X", result.Frames, result.Broadcast, want)
	}
	if character.VisualFlags == nil || *character.VisualFlags != int64(enterworld.VisualFlagHelper) {
		t.Fatalf("persisted visual flags = %v, want helper bit preserved and beginner bit cleared", character.VisualFlags)
	}
}

func TestVisualFlagsHelperToggleRejectsUnknownBitsAndExpiresBeginnerAboveLevel19(t *testing.T) {
	character := testCharacter()
	rt, _ := newTestRuntime(character, testItems())

	if result := rt.HandleVisualFlags(testDivision, character, []byte{0x04}); len(result.Frames) != 0 || character.VisualFlags != nil {
		t.Fatalf("an unknown bit was accepted: %#v flags=%v", result, character.VisualFlags)
	}
	// Action 1011 (695420) turns the helper mark on and off again.
	for _, want := range []uint8{enterworld.VisualFlagBeginner | enterworld.VisualFlagHelper, enterworld.VisualFlagBeginner} {
		result := rt.HandleVisualFlags(testDivision, character, []byte{want})
		if len(result.Broadcast) != 1 || result.Broadcast[0].Payload[4] != want || *character.VisualFlags != int64(want) {
			t.Fatalf("helper toggle to %#x = %#v flags=%v", want, result, character.VisualFlags)
		}
	}

	level := int64(20)
	character.Level = &level
	result := rt.HandleVisualFlags(testDivision, character, []byte{enterworld.VisualFlagBeginner})
	if len(result.Frames) != 1 || len(result.Frames[0].Payload) != 5 || result.Frames[0].Payload[4] != 0 {
		t.Fatalf("level-20 beginner request = %#v, want authoritative B683 flags=0", result.Frames)
	}
	if character.VisualFlags == nil || *character.VisualFlags != 0 {
		t.Fatalf("level-20 persisted visual flags = %v, want 0", character.VisualFlags)
	}
}
