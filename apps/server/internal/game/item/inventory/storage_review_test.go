package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
)

func TestTransferSplitOwnsOptionsAndClearsIdentity(t *testing.T) {
	inv := New([]Item{{Slot: 13, RefObjID: 3630, Quantity: 20, RecordID: 0x123456789,
		MagicOptions: []uint64{11, 22}}}, domain.DefaultInventorySize)
	result, fault := inv.Transfer(13, 14, 5, 50)
	if fault != nil || result.Leg != LegSplit {
		t.Fatalf("split: %+v %v", result, fault)
	}
	if inv.items[0].RecordID != 0x123456789 || inv.items[1].RecordID != 0 {
		t.Fatalf("split duplicated or changed identity: %+v", inv.items)
	}
	inv.items[1].MagicOptions[0] = 99
	if inv.items[0].MagicOptions[0] != 11 {
		t.Fatal("split options alias source")
	}
}

func TestSplitAllocatorPanicDoesNotConsumeStock(t *testing.T) {
	inv := New([]Item{{Slot: 13, RefObjID: 3630, Quantity: 20}}, domain.DefaultInventorySize)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("expected allocator panic")
			}
		}()
		inv.SplitItem(13, 14, 5, func() uint64 { panic("allocation failed") })
	}()
	if len(inv.items) != 1 || inv.items[0].Quantity != 20 {
		t.Fatalf("failed allocation mutated inventory: %+v", inv.items)
	}
}

func TestUnknownStackCapacityIsNotFifty(t *testing.T) {
	inv := New([]Item{{Slot: 13, RefObjID: 3630, RecordID: 1, Quantity: 10}}, domain.DefaultInventorySize)
	if got := inv.GetSplitEligibility(13, &Item{RefObjID: 3630, Quantity: 5}, 0); got != 0 {
		t.Fatalf("unknown capacity accepted %d items", got)
	}
}

func TestCosMissingProducerCannotFinalize(t *testing.T) {
	inv := New([]Item{{Slot: 13, RefObjID: 3630, Quantity: 1}}, domain.DefaultInventorySize)
	table := &CosParamTable{}
	_, err := ReconcileCosParamEntries(inv, 0, 1, func(*Item) int32 { return 1 }, nil, table)
	if err == nil || table.Finalized {
		t.Fatal("missing subtype producer silently succeeded")
	}
}
