/*
===========================================================================

stoneidentity_test.go - a stacked stone keeps its assimilation value

Port-only (#583): with SRO_STACK_SIZES raising stone caps, magic and
attribute stones merge only with an equal Plus. Every merge path shares
stackIdentityMatches; each is exercised here.

===========================================================================
*/
package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// stoneTestCap is a raised stone cap; natively stones stack 1.
const stoneTestCap = 50

/*
================
stone

A magic stone (TID 3.3.11.1) with its assimilation value in Plus.
================
*/
func stone(slot uint8, plus uint8, quantity uint16) Item {
	return Item{Slot: slot, RefObjID: 6800, TypeFlags: wire.PackTypeFlags(3, 3, 11, 1), Quantity: quantity, Plus: plus, RecordID: 1}
}

/*
================
TestStoneMergeRequiresEqualAssimilation
================
*/
func TestStoneMergeRequiresEqualAssimilation(t *testing.T) {
	for _, plus := range []uint8{90, 40} {
		inv := New([]Item{stone(13, 90, 10), stone(14, plus, 5)}, domain.DefaultInventorySize)
		if _, fault := inv.Transfer(14, 13, 5, stoneTestCap); fault != nil {
			t.Fatal(fault)
		}
		row, _ := inv.At(13)
		if plus == 90 {
			if row.Quantity != 15 || row.Plus != 90 || inv.Len() != 1 {
				t.Fatalf("equal stones did not merge: %+v", inv.Items())
			}
		} else if row.Quantity != 5 || row.Plus != 40 || inv.Len() != 2 {
			t.Fatalf("different assimilation values were combined: %+v", inv.Items())
		}
	}
}

/*
================
TestStonePickupSkipsOtherValuesAndSplitsKeepTheirs
================
*/
func TestStonePickupSkipsOtherValuesAndSplitsKeepTheirs(t *testing.T) {
	inv := New([]Item{stone(13, 40, 10), stone(15, 90, 5)}, domain.DefaultInventorySize)
	result, fault := inv.GrantStack(stone(0, 90, 8), stoneTestCap)
	if fault != nil || result.DestSlot != 15 || result.PostMergeCount != 13 {
		t.Fatalf("pickup merged into the wrong stones: %+v %v", result, fault)
	}
	result, fault = inv.GrantStack(stone(0, 70, 2), stoneTestCap)
	if fault != nil || result.Merged || result.DestSlot == 13 || result.DestSlot == 15 {
		t.Fatalf("a 70%% stone joined another value's stack: %+v %v", result, fault)
	}
	row, fault := inv.SplitItem(15, 20, 3, nil)
	if fault != nil || row.Plus != 90 || row.Quantity != 3 {
		t.Fatalf("split lost the assimilation value: %+v %v", row, fault)
	}
	if inv.GetSplitEligibility(13, row, stoneTestCap) != 0 || inv.GetSplitEligibility(15, row, stoneTestCap) != 3 {
		t.Fatal("storage merge eligibility ignored the assimilation value")
	}
	dropped, fault := inv.DropQuantity(15, 2)
	if fault != nil || dropped.Plus != 90 {
		t.Fatalf("partial drop lost the assimilation value: %+v %v", dropped, fault)
	}
}

/*
================
TestStoneContainerTransferKeepsBothValues
================
*/
func TestStoneContainerTransferKeepsBothValues(t *testing.T) {
	bag := New([]Item{stone(13, 90, 10)}, domain.DefaultInventorySize)
	pet, fault := NewContainer([]Item{stone(0, 40, 5)}, 10)
	if fault != nil {
		t.Fatal(fault)
	}
	if fault := bag.TransferWholeTo(pet, 13, 0, stoneTestCap); fault != nil {
		t.Fatal(fault)
	}
	a, _ := bag.At(13)
	b, _ := pet.At(0)
	if a.Plus != 40 || a.Quantity != 5 || b.Plus != 90 || b.Quantity != 10 {
		t.Fatalf("pet transfer merged different values: %+v %+v", a, b)
	}
	if fault := pet.TransferWholeTo(bag, 0, 13, stoneTestCap); fault != nil {
		t.Fatal(fault)
	}
	if bag.Len() != 1 || pet.Len() != 1 {
		t.Fatalf("swap back changed row counts: %+v %+v", bag.Items(), pet.Items())
	}
	equal, _ := NewContainer([]Item{stone(0, 90, 5)}, 10)
	if fault := equal.TransferWholeTo(bag, 0, 13, stoneTestCap); fault != nil {
		t.Fatal(fault)
	}
	if merged, _ := bag.At(13); equal.Len() != 0 || merged.Quantity != 15 || merged.Plus != 90 {
		t.Fatalf("equal values did not merge across containers: %+v", bag.Items())
	}
}

/*
================
TestValuelessStonesMergeByReference

3.3.11.7 stones carry no Plus byte, so they merge like any stackable row,
as do non-stone stackables whose Plus is always zero.
================
*/
func TestValuelessStonesMergeByReference(t *testing.T) {
	flags := wire.PackTypeFlags(3, 3, 11, 7)
	inv := New([]Item{
		{Slot: 13, RefObjID: 6900, TypeFlags: flags, Quantity: 4, Plus: 1, RecordID: 1},
		{Slot: 14, RefObjID: 6900, TypeFlags: flags, Quantity: 3, Plus: 2, RecordID: 2},
	}, domain.DefaultInventorySize)
	if _, fault := inv.Transfer(14, 13, 3, stoneTestCap); fault != nil {
		t.Fatal(fault)
	}
	if row, _ := inv.At(13); row.Quantity != 7 || inv.Len() != 1 {
		t.Fatalf("valueless stones did not merge: %+v", inv.Items())
	}
}
