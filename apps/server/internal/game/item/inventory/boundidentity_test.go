/*
===========================================================================

boundidentity_test.go - a bound stack merges only with a bound stack

Port-only (issue #1980): a merge keeps the destination row and rewrites
only its count, so bound quantity poured into an unbound row would come
out tradeable, and unbound quantity poured into a bound row would come
out locked. Every lane that pours counts - pickup grant, bag transfer,
whole-container transfer, storage eligibility - shares the inventory
stack identity; each is exercised here in both directions.

===========================================================================
*/
package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

// boundTestCap is the fixture potion cap (itemdata MaxStack 50).
const boundTestCap = 50

/*
================
boundPotion

The stock HP potion as an inventory row, bound or not.
================
*/
func boundPotion(slot uint8, bound bool, quantity uint16) Item {
	return Item{
		Slot: slot, RefObjID: 3630, TypeFlags: wire.PackTypeFlags(3, 3, 1, 1),
		Quantity: quantity, ForgeBound: bound, RecordID: 1,
	}
}

/*
================
TestBoundPickupMergesOnlyWithBound

The laundering probe of the #2775 review, pinned at the grant: an
unbound x10 bag row plus a bound x25 ground pickup must stay two rows.
================
*/
func TestBoundPickupMergesOnlyWithBound(t *testing.T) {
	for _, tc := range []struct {
		bagBound, grantBound, merged bool
	}{
		{false, false, true},
		{true, true, true},
		{false, true, false},
		{true, false, false},
	} {
		inv := New([]Item{boundPotion(13, tc.bagBound, 10)}, domain.DefaultInventorySize)
		grant, fault := inv.GrantStack(boundPotion(0, tc.grantBound, 25), boundTestCap)
		if fault != nil {
			t.Fatal(fault)
		}
		if grant.Merged != tc.merged {
			t.Fatalf("bag bound=%v, pickup bound=%v: merged=%v, want %v",
				tc.bagBound, tc.grantBound, grant.Merged, tc.merged)
		}
		row, _ := inv.At(13)
		if tc.merged {
			if row.Quantity != 35 || row.ForgeBound != tc.bagBound || inv.Len() != 1 {
				t.Fatalf("bag bound=%v, pickup bound=%v: rows %+v",
					tc.bagBound, tc.grantBound, inv.Items())
			}
			continue
		}
		landed, _ := inv.At(grant.DestSlot)
		if row.Quantity != 10 || row.ForgeBound != tc.bagBound ||
			landed.Quantity != 25 || landed.ForgeBound != tc.grantBound || inv.Len() != 2 {
			t.Fatalf("bag bound=%v, pickup bound=%v poured across the mark: %+v",
				tc.bagBound, tc.grantBound, inv.Items())
		}
	}
}

/*
================
TestBoundBagTransferMergesOnlyWithBound

The manual bag merge (CPSMission_MergeOrSwapItems): a mixed pair takes
the swap leg, so each row keeps its own mark and count.
================
*/
func TestBoundBagTransferMergesOnlyWithBound(t *testing.T) {
	for _, tc := range []struct {
		destBound, sourceBound, merged bool
	}{
		{false, false, true},
		{true, true, true},
		{false, true, false},
		{true, false, false},
	} {
		inv := New([]Item{
			boundPotion(13, tc.destBound, 10),
			boundPotion(14, tc.sourceBound, 25),
		}, domain.DefaultInventorySize)
		if _, fault := inv.Transfer(14, 13, 25, boundTestCap); fault != nil {
			t.Fatal(fault)
		}
		dest, _ := inv.At(13)
		if tc.merged {
			if dest.Quantity != 35 || dest.ForgeBound != tc.destBound || inv.Len() != 1 {
				t.Fatalf("dest bound=%v, source bound=%v: rows %+v",
					tc.destBound, tc.sourceBound, inv.Items())
			}
			continue
		}
		source, _ := inv.At(14)
		if dest.Quantity != 25 || dest.ForgeBound != tc.sourceBound ||
			source.Quantity != 10 || source.ForgeBound != tc.destBound || inv.Len() != 2 {
			t.Fatalf("dest bound=%v, source bound=%v swapped wrong: %+v",
				tc.destBound, tc.sourceBound, inv.Items())
		}
	}
}

/*
================
TestBoundContainerTransferSwapsMixedPairs

The whole-container transfer, including the cap-one singleton shortcut:
a mixed pair swaps rows wholesale instead of exchanging counts under
the destination row's mark.
================
*/
func TestBoundContainerTransferSwapsMixedPairs(t *testing.T) {
	bag := New([]Item{boundPotion(13, true, 10)}, domain.DefaultInventorySize)
	pet, fault := NewContainer([]Item{boundPotion(0, false, 10)}, 10)
	if fault != nil {
		t.Fatal(fault)
	}
	if fault := bag.TransferWholeTo(pet, 13, 0, boundTestCap); fault != nil {
		t.Fatal(fault)
	}
	inBag, _ := bag.At(13)
	inPet, _ := pet.At(0)
	if inBag.Quantity != 10 || inBag.ForgeBound || inPet.Quantity != 10 || !inPet.ForgeBound {
		t.Fatalf("stacked transfer crossed the mark: bag %+v pet %+v", inBag, inPet)
	}

	single := New([]Item{boundPotion(13, true, 1)}, domain.DefaultInventorySize)
	pouch, fault := NewContainer([]Item{boundPotion(0, false, 1)}, 10)
	if fault != nil {
		t.Fatal(fault)
	}
	if fault := single.TransferWholeTo(pouch, 13, 0, 1); fault != nil {
		t.Fatal(fault)
	}
	inBag, _ = single.At(13)
	inPet, _ = pouch.At(0)
	if inBag.Quantity != 1 || inBag.ForgeBound || inPet.Quantity != 1 || !inPet.ForgeBound {
		t.Fatalf("singleton transfer crossed the mark: bag %+v pet %+v", inBag, inPet)
	}

	merged := New([]Item{boundPotion(13, true, 10)}, domain.DefaultInventorySize)
	kin, fault := NewContainer([]Item{boundPotion(0, true, 10)}, 10)
	if fault != nil {
		t.Fatal(fault)
	}
	if fault := merged.TransferWholeTo(kin, 13, 0, boundTestCap); fault != nil {
		t.Fatal(fault)
	}
	row, _ := kin.At(0)
	if row.Quantity != 20 || !row.ForgeBound || merged.Len() != 0 {
		t.Fatalf("bound stacks did not merge across containers: %+v", kin.Items())
	}
}

/*
================
TestBoundStorageEligibilitySplitsByBound
================
*/
func TestBoundStorageEligibilitySplitsByBound(t *testing.T) {
	for _, tc := range []struct {
		targetBound, incomingBound bool
		want                       int32
	}{
		{false, false, 5},
		{true, true, 5},
		{false, true, 0},
		{true, false, 0},
	} {
		inv := New([]Item{boundPotion(13, tc.targetBound, 10)}, domain.DefaultInventorySize)
		incoming := boundPotion(0, tc.incomingBound, 5)
		if got := inv.GetSplitEligibility(13, &incoming, boundTestCap); got != tc.want {
			t.Fatalf("target bound=%v, incoming bound=%v: eligibility %d, want %d",
				tc.targetBound, tc.incomingBound, got, tc.want)
		}
	}
}
