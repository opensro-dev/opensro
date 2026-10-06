/*
===========================================================================

quickslot_test.go - native hotbar configuration wire and persistence tests

===========================================================================
*/
package enterworld

import (
	"bytes"
	"testing"
)

/*
================
quickSlotSavePayload
================
*/
func quickSlotSavePayload(slot, kind uint8, value uint32) []byte {
	return []byte{1, slot, kind, byte(value), byte(value >> 8), byte(value >> 16), byte(value >> 24)}
}

/*
================
TestQuickSlotSavePersistsReplacesAndClears
================
*/
func TestQuickSlotSavePersistsReplacesAndClears(t *testing.T) {
	character := &Character{Name: "quickbar-owner"}
	deps := &Deps{}

	changed, err := HandleQuickSlotSave(deps, character, quickSlotSavePayload(10, 0x49, 1234))
	if err != nil || !changed {
		t.Fatalf("first save = changed %v err %v", changed, err)
	}
	changed, err = HandleQuickSlotSave(deps, character, quickSlotSavePayload(10, 0x4a, 0x020003ed))
	if err != nil || !changed || len(character.QuickSlots) != 1 ||
		character.QuickSlots[0].Kind != 0x4a || character.QuickSlots[0].Payload != 0x020003ed {
		t.Fatalf("replace = changed %v err %v rows %+v", changed, err, character.QuickSlots)
	}
	changed, err = HandleQuickSlotSave(deps, character, quickSlotSavePayload(10, 0, 0xffffffff))
	if err != nil || !changed || len(character.QuickSlots) != 0 {
		t.Fatalf("clear = changed %v err %v rows %+v", changed, err, character.QuickSlots)
	}
}

/*
================
TestQuickSlotSaveRejectsMalformedStateAtomically
================
*/
func TestQuickSlotSaveRejectsMalformedStateAtomically(t *testing.T) {
	character := &Character{Name: "quickbar-owner", QuickSlots: []QuickSlotBinding{{Slot: 2, Kind: 0x46, Payload: 6}}}
	deps := &Deps{}
	bad := [][]byte{
		{1, 2},
		quickSlotSavePayload(51, 0x49, 1),
		quickSlotSavePayload(2, 0x48, 1),
		quickSlotSavePayload(2, 0x46, quickSlotBagPayloadLimit),
		quickSlotSavePayload(2, 0x47, 13),
		quickSlotSavePayload(2, 0x4e, 4),
	}
	for _, payload := range bad {
		if changed, err := HandleQuickSlotSave(deps, character, payload); err == nil || changed {
			t.Fatalf("payload %x accepted: changed %v err %v", payload, changed, err)
		}
	}
	if len(character.QuickSlots) != 1 || character.QuickSlots[0].Payload != 6 {
		t.Fatalf("refusal mutated rows: %+v", character.QuickSlots)
	}
}

/*
================
TestQuickSlotMissionRevealRequestIsAcceptedWithoutMutation
================
*/
func TestQuickSlotMissionRevealRequestIsAcceptedWithoutMutation(t *testing.T) {
	character := &Character{
		Name:       "quickbar-owner",
		QuickSlots: []QuickSlotBinding{{Slot: 2, Kind: 0x46, Payload: 6}},
	}
	changed, err := HandleQuickSlotMessage(&Deps{}, character, []byte{0, 7})
	if err != nil || changed {
		t.Fatalf("mission reveal request = changed %v err %v", changed, err)
	}
	if len(character.QuickSlots) != 1 || character.QuickSlots[0].Payload != 6 {
		t.Fatalf("mission reveal request mutated rows: %+v", character.QuickSlots)
	}
}

/*
================
TestQuickSlotHudStatePayloadIsModeSevenSortedAndPacked
================
*/
func TestQuickSlotHudStatePayloadIsModeSevenSortedAndPacked(t *testing.T) {
	character := &Character{QuickSlots: []QuickSlotBinding{
		{Slot: 9, Kind: 0x49, Payload: 0x11223344},
		{Slot: 2, Kind: 0x46, Payload: 6},
	}}
	want := []byte{
		7, 2,
		2, 0x46, 6, 0, 0, 0,
		9, 0x49, 0x44, 0x33, 0x22, 0x11,
	}
	if got := buildQuickSlotHudStatePayload(character); !bytes.Equal(got, want) {
		t.Fatalf("hud state = %x, want %x", got, want)
	}
}

/*
================
TestQuickSlotExpandedBagIndicesRoundTrip

The packet carries a bag-relative index, not a default inventory capacity.
================
*/
func TestQuickSlotExpandedBagIndicesRoundTrip(t *testing.T) {
	for _, index := range []uint32{44, 45, 63, 95, quickSlotBagPayloadLimit - 1} {
		character := &Character{Name: "expanded-bag"}
		payload := quickSlotSavePayload(50, 0x46, index)
		if changed, err := HandleQuickSlotSave(&Deps{}, character, payload); err != nil || !changed {
			t.Fatalf("index %d: changed %v, err %v", index, changed, err)
		}
		want := []byte{7, 1, 50, 0x46, byte(index), 0, 0, 0}
		if got := buildQuickSlotHudStatePayload(character); !bytes.Equal(got, want) {
			t.Fatalf("index %d: state %x, want %x", index, got, want)
		}
	}
}
