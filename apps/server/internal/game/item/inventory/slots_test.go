package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
)

func TestSlotBands(t *testing.T) {
	cases := []struct {
		wireSlot  uint8
		bagEnd    uint8
		equipment bool
		bag       bool
	}{
		{0, 45, true, false},
		{12, 45, true, false},
		{13, 45, false, true},
		{44, 45, false, true},
		{45, 45, false, false},
		{45, 67, false, true},
		{66, 67, false, true},
		{67, 67, false, false},
		{255, 77, false, false},
	}

	for _, testCase := range cases {
		if got := IsEquipmentSlot(testCase.wireSlot); got != testCase.equipment {
			t.Fatalf("IsEquipmentSlot(%d) = %v, want %v", testCase.wireSlot, got, testCase.equipment)
		}
		if got := IsBagSlot(testCase.wireSlot, testCase.bagEnd); got != testCase.bag {
			t.Fatalf("IsBagSlot(%d, %d) = %v, want %v", testCase.wireSlot, testCase.bagEnd, got, testCase.bag)
		}
	}
}

// The composer biases bag slots by 13, so bag index 0 travels as wire slot 13.
func TestBagSlotBiasRoundTrips(t *testing.T) {
	if got := WireSlotFromBagIndex(0); got != 13 {
		t.Fatalf("bag index 0 = wire slot %d, want 13", got)
	}
	if got := WireSlotFromBagIndex(31); got != 44 {
		t.Fatalf("bag index 31 = wire slot %d, want 44", got)
	}

	for bagIndex := uint8(0); bagIndex < domain.MaxInventorySize-EquipmentSlotEnd; bagIndex++ {
		wireSlot := WireSlotFromBagIndex(bagIndex)
		got, ok := BagIndexFromWireSlot(wireSlot, domain.MaxInventorySize)
		if !ok {
			t.Fatalf("wire slot %d did not map back to a bag index", wireSlot)
		}
		if got != bagIndex {
			t.Fatalf("wire slot %d = bag index %d, want %d", wireSlot, got, bagIndex)
		}
	}
}

func TestBagIndexRejectsEquipmentSlots(t *testing.T) {
	if _, ok := BagIndexFromWireSlot(12, domain.DefaultInventorySize); ok {
		t.Fatal("equipment wire slot 12 was accepted as a bag slot")
	}
	if _, ok := BagIndexFromWireSlot(45, domain.DefaultInventorySize); ok {
		t.Fatal("out-of-range wire slot 45 was accepted as a bag slot")
	}
}

// A character's bag ends at its capacity byte: 45 at creation, more once an
// expansion quest's slots were presented at a world entry.
func TestBagEndFollowsTheCharacter(t *testing.T) {
	fresh := &domain.Character{}
	if got := BagEnd(fresh); got != 45 {
		t.Fatalf("fresh bag end = %d, want 45", got)
	}
	expanded := &domain.Character{InventorySize: 55, InventoryExpansion: 4}
	if got := BagEnd(expanded); got != 55 {
		t.Fatalf("expanded bag end = %d, want 55 (waiting slots are not usable yet)", got)
	}
	if !InBag(expanded, 54) || InBag(expanded, 55) || InBag(expanded, 12) {
		t.Fatal("InBag must cover wire slots 13..54 of a 55-slot inventory")
	}
}
