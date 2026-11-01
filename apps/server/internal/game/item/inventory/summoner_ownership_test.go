/*
===========================================================================

summoner_ownership_test.go - companion records survive container transfers

===========================================================================
*/
package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestSummonerTransferPreservesDetachedRecordAndRejectsDuplication
================
*/
func TestSummonerTransferPreservesDetachedRecordAndRejectsDuplication(t *testing.T) {
	pet := &domain.CharacterCOS{RefObjID: 77, Name: "Saved", CurrentHP: 42, Experience: 900, StateFlags: 1, Container: &domain.COSContainer{Capacity: 28, Rows: []domain.InventoryRow{{Slot: 0, StackCount: 3}}}}
	item := Item{Slot: 13, RefObjID: 9, TypeFlags: 0x8cc, Quantity: 1, Summon: pet}
	bag := New([]Item{item}, domain.DefaultInventorySize)
	room, fault := NewStorageRoom(nil, 150)
	if fault != nil {
		t.Fatal(fault)
	}
	if fault = bag.TransferWholeTo(room, 13, 0, 500); fault != nil {
		t.Fatal(fault)
	}
	pet.Container.Rows[0].StackCount = 99
	stored, ok := room.At(0)
	if !ok || stored.Summon.Container.Rows[0].StackCount != 3 || bag.Len() != 0 {
		t.Fatal("transfer aliased or duplicated source")
	}
	if fault = room.TransferWholeTo(bag, 0, 14, 500); fault != nil {
		t.Fatal(fault)
	}
	retained, _ := bag.At(14)
	if retained.Summon.Experience != 900 || retained.Summon.CurrentHP != 42 || room.Len() != 0 {
		t.Fatal("withdraw lost pet")
	}
	corrupt := item
	corrupt.Quantity = 2
	for _, summoned := range []bool{false, true} {
		corrupt.Summon = domain.CloneCOS(pet)
		corrupt.Summon.Summoned = summoned
		if summoned {
			corrupt.Quantity = 1
		}
		inv := New([]Item{corrupt}, domain.DefaultInventorySize)
		if _, fault = inv.Drop(13); fault == nil {
			t.Fatal("dropped invalid/live companion")
		}
		if !summoned {
			if _, fault = inv.Transfer(13, 14, 1, 500); fault == nil {
				t.Fatal("moved invalid quantity-2 companion")
			}
		}
		if fault = inv.TransferWholeTo(room, 13, 0, 500); fault == nil {
			t.Fatal("stored invalid/live companion")
		}
		if _, fault = inv.SplitItem(13, 14, 1, nil); fault == nil {
			t.Fatal("split companion")
		}
		if _, fault = room.GrantStack(corrupt, 500); fault == nil {
			t.Fatal("granted duplicated companion")
		}
	}

	// The server's inventory slot-move dispatch permits moving or swapping
	// an active companion scroll within the owner's bag and updates its
	// retained InventorySlot, while still refusing equipment sockets.
	activeA := domain.CloneCOS(pet)
	activeA.Summoned = true
	activeA.InventorySlot = 13
	activeB := domain.CloneCOS(pet)
	activeB.Summoned = true
	activeB.InventorySlot = 15
	bagWithSummoned := New([]Item{
		{Slot: 13, RefObjID: 9, TypeFlags: 0x8cc, Quantity: 1, Summon: activeA},
		{Slot: 15, RefObjID: 10, TypeFlags: 0x10cc, Quantity: 1, Summon: activeB},
	}, domain.DefaultInventorySize)
	if _, fault := bagWithSummoned.Transfer(13, 0, 1, 1); fault == nil {
		t.Fatal("equipped summoned companion scroll")
	}
	moved, fault := bagWithSummoned.Transfer(13, 14, 1, 500)
	if fault != nil || moved.Leg != LegMove {
		t.Fatalf("bag move of summoned companion failed: %+v, fault=%v", moved, fault)
	}
	if row14, ok := bagWithSummoned.At(14); !ok || row14.Summon == nil || row14.Summon.InventorySlot != 14 {
		t.Fatalf("moved companion InventorySlot not updated: %+v", row14)
	}
	swapped, fault := bagWithSummoned.Transfer(14, 15, 1, 500)
	if fault != nil || swapped.Leg != LegSwap {
		t.Fatalf("bag swap of summoned companions failed: %+v, fault=%v", swapped, fault)
	}
	row14, _ := bagWithSummoned.At(14)
	row15, _ := bagWithSummoned.At(15)
	if row14.RefObjID != 10 || row14.Summon.InventorySlot != 14 || row15.RefObjID != 9 || row15.Summon.InventorySlot != 15 {
		t.Fatalf("swapped companion slots mismatch: 14=%+v 15=%+v", row14, row15)
	}
}
