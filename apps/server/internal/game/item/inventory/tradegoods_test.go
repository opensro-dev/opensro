/*
===========================================================================

tradegoods_test.go - original cargo ownership survives stack operations

===========================================================================
*/
package inventory

import "testing"

/*
================
tradeGoods
================
*/
func tradeGoods(slot uint8, owner string, quantity uint16) Item {
	return Item{Slot: slot, RefObjID: 2151, TypeFlags: tradeGoodsType, Quantity: quantity, TradeOwner: owner, RecordID: 1}
}

/*
================
TestCargoMergeRequiresOriginalOwner
================
*/
func TestCargoMergeRequiresOriginalOwner(t *testing.T) {
	for _, owner := range []string{"Trader", "Thief", "trader", ""} {
		inv := New([]Item{tradeGoods(13, "Trader", 10), tradeGoods(14, owner, 5)})
		result, fault := inv.Transfer(14, 13, 5, 20)
		if fault != nil {
			t.Fatal(fault)
		}
		row, _ := inv.At(13)
		if owner == "Trader" {
			if row.Quantity != 15 || row.TradeOwner != owner || !result.SourceRemoved {
				t.Fatalf("same owner did not merge: %+v", inv.Items())
			}
		} else if row.Quantity != 5 || row.TradeOwner != owner || inv.Len() != 2 {
			t.Fatalf("different owners were combined: %+v", inv.Items())
		}
	}
}

/*
================
TestCargoPickupSkipsForeignStacksAndSplitsPreserveOwner
================
*/
func TestCargoPickupSkipsForeignStacksAndSplitsPreserveOwner(t *testing.T) {
	inv := New([]Item{tradeGoods(13, "Foreign", 10), tradeGoods(15, "Trader", 5)})
	result, fault := inv.GrantStack(tradeGoods(0, "Trader", 8), 20)
	if fault != nil || result.DestSlot != 15 || result.PostMergeCount != 13 {
		t.Fatalf("pickup merged into the wrong cargo: %+v %v", result, fault)
	}
	row, fault := inv.SplitItem(15, 14, 3, nil)
	if fault != nil || row.TradeOwner != "Trader" || row.Quantity != 3 {
		t.Fatalf("split lost cargo identity: %+v %v", row, fault)
	}
	if inv.GetSplitEligibility(13, row, 20) != 0 || inv.GetSplitEligibility(15, row, 20) != 3 {
		t.Fatal("storage merge eligibility ignored cargo ownership")
	}
	dropped, fault := inv.DropQuantity(15, 2)
	if fault != nil || dropped.TradeOwner != "Trader" {
		t.Fatalf("partial drop lost cargo identity: %+v %v", dropped, fault)
	}
}

/*
================
TestCargoContainerTransferKeepsBothOwners
================
*/
func TestCargoContainerTransferKeepsBothOwners(t *testing.T) {
	bag := New([]Item{tradeGoods(13, "Trader", 10)})
	vehicle, fault := NewContainer([]Item{tradeGoods(0, "Foreign", 5)}, 10)
	if fault != nil {
		t.Fatal(fault)
	}
	if fault := bag.TransferWholeTo(vehicle, 13, 0, 20); fault != nil {
		t.Fatal(fault)
	}
	a, _ := bag.At(13)
	b, _ := vehicle.At(0)
	if a.TradeOwner != "Foreign" || a.Quantity != 5 || b.TradeOwner != "Trader" || b.Quantity != 10 {
		t.Fatalf("COS cargo transfer lost ownership: %+v %+v", a, b)
	}
}
