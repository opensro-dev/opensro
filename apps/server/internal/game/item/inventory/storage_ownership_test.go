package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
)

func TestInventoryRecordOwnershipBoundaries(t *testing.T) {
	input := Item{Slot: 13, RefObjID: 3630, Quantity: 20, RecordID: 91, MagicOptions: []uint64{123}}
	inv := New([]Item{input}, domain.DefaultInventorySize)
	input.MagicOptions[0] = 999
	assertOption := func() {
		t.Helper()
		row, _ := inv.At(13)
		if row.MagicOptions[0] != 123 {
			t.Fatalf("mutable record escaped ownership: %+v", row)
		}
	}
	assertOption()
	rows := inv.Items()
	rows[0].MagicOptions[0] = 222
	assertOption()
	row, _ := inv.At(13)
	row.MagicOptions[0] = 333
	assertOption()
	body := inv.items[0].Body()
	body.MagicOptions[0] = 444
	assertOption()
	dropped, fault := inv.DropQuantity(13, 5)
	if fault != nil || dropped.RecordID != 0 || dropped.Quantity != 5 {
		t.Fatalf("partial drop clone: %+v %v", dropped, fault)
	}
	dropped.MagicOptions[0] = 555
	assertOption()
	left, _ := inv.At(13)
	if left.RecordID != 91 || left.Quantity != 15 {
		t.Fatalf("source identity or count changed incorrectly: %+v", left)
	}
}

func TestPickupPartialCloneAndFullTransferIdentity(t *testing.T) {
	for _, quantity := range []uint16{5, 20} {
		ground := Item{RefObjID: 3630, Quantity: quantity, RecordID: 77, MagicOptions: []uint64{123}}
		inv := New(nil, domain.DefaultInventorySize)
		result, fault := inv.GrantStack(ground, 10)
		if fault != nil {
			t.Fatal(fault)
		}
		row, _ := inv.At(result.DestSlot)
		wantID := uint64(77)
		if quantity > 10 {
			wantID = 0
		}
		if row.RecordID != wantID {
			t.Fatalf("quantity %d: identity %d want %d", quantity, row.RecordID, wantID)
		}
		ground.MagicOptions[0] = 999
		row, _ = inv.At(result.DestSlot)
		if row.MagicOptions[0] != 123 {
			t.Fatal("pickup retained external options alias")
		}
	}
}
