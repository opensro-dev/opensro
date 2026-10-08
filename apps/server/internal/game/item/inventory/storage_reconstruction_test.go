package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
)

// ----------------------------------------------------------------------------
// Test 1: Zero RecordID rejection in GetSplitEligibility (0x004B9D40)
// ----------------------------------------------------------------------------
func TestGetSplitEligibilityZeroRecordIDRejection(t *testing.T) {
	inv := New([]Item{
		{
			Slot:     13,
			RefObjID: 2005,
			Quantity: 10,
			RecordID: 0, // Unpersisted / zero DB serial
		},
	}, domain.DefaultInventorySize)

	incoming := &Item{
		RefObjID: 2005,
		Quantity: 5,
	}

	// Target item has RecordID == 0 -> native 004B9D40 MUST reject!
	eligible := inv.GetSplitEligibility(13, incoming, 50)
	if eligible != 0 {
		t.Fatalf("expected 0 eligible for zero RecordID, got %d", eligible)
	}

	// Now set a valid persistent RecordID -> should succeed
	inv.items[0].RecordID = 0x12345678
	eligible = inv.GetSplitEligibility(13, incoming, 50)
	if eligible != 5 {
		t.Fatalf("expected 5 eligible for valid RecordID, got %d", eligible)
	}
}

// ----------------------------------------------------------------------------
// Test 2: SplitItem with unpersisted identity and sequential allocation (0x004B9C60)
// ----------------------------------------------------------------------------
func TestSplitItemIdentityAndRollback(t *testing.T) {
	inv := New([]Item{
		{
			Slot:     13,
			RefObjID: 2005,
			Quantity: 20,
			RecordID: 10001,
		},
	}, domain.DefaultInventorySize)

	// 1. Split into empty slot 14 without allocator -> new item starts with RecordID == 0 (unpersisted)
	splitItem, fault := inv.SplitItem(13, 14, 5, nil)
	if fault != nil {
		t.Fatalf("SplitItem failed: %v", fault)
	}
	if splitItem.Quantity != 5 {
		t.Fatalf("expected split quantity 5, got %d", splitItem.Quantity)
	}
	if splitItem.RecordID != 0 {
		t.Fatalf("expected new unpersisted item to have RecordID == 0, got %d", splitItem.RecordID)
	}
	if inv.items[0].Quantity != 15 {
		t.Fatalf("expected source quantity 15, got %d", inv.items[0].Quantity)
	}

	// 2. Split with sequential ID allocator
	nextID := uint64(50001)
	allocator := func() uint64 {
		id := nextID
		nextID++
		return id
	}

	splitItem2, fault2 := inv.SplitItem(13, 15, 5, allocator)
	if fault2 != nil {
		t.Fatalf("SplitItem with allocator failed: %v", fault2)
	}
	if splitItem2.RecordID != 50001 {
		t.Fatalf("expected allocated RecordID 50001, got %d", splitItem2.RecordID)
	}

	// 3. Rollback test: try splitting into occupied slot 14 -> must fail without changing source
	_, faultOccupied := inv.SplitItem(13, 14, 5, nil)
	if faultOccupied == nil {
		t.Fatal("expected fault when splitting into occupied slot, got nil")
	}
	if inv.items[0].Quantity != 10 {
		t.Fatalf("expected source quantity unchanged (10), got %d", inv.items[0].Quantity)
	}
}

// ----------------------------------------------------------------------------
// Test 3: ReconcileCosParamEntries native semantics (0x004BA940)
// ----------------------------------------------------------------------------
func TestReconcileCosParamEntriesNativeSemantics(t *testing.T) {
	inv := New([]Item{
		// Slot 5: should be ignored by kind 0 (starts at 13)
		{Slot: 5, RefObjID: 555},
		// Slot 13: predicate = 0 -> producer NOT called
		{Slot: 13, RefObjID: 1300},
		// Slot 14: predicate = 2 -> producer NOT called
		{Slot: 14, RefObjID: 1400},
		// Slot 15: predicate = 1 -> producer CALLED
		{Slot: 15, RefObjID: 1500},
	}, domain.DefaultInventorySize)

	// 1. Invalid storageKind (must be 0, 3, or 4)
	table := &CosParamTable{}
	_, errKind := ReconcileCosParamEntries(inv, 1, 1001, nil, nil, table)
	if errKind == nil {
		t.Fatal("expected error for invalid storageKind 1, got nil")
	}

	// 2. Null owner (must be non-zero)
	_, errOwner := ReconcileCosParamEntries(inv, 0, 0, nil, nil, table)
	if errOwner == nil {
		t.Fatal("expected error for null owner 0, got nil")
	}

	// 3. Predicate checking (0, 1, 2) and producer dispatch
	predicate := func(item *Item) int32 {
		switch item.Slot {
		case 13:
			return 0
		case 14:
			return 2
		case 15:
			return 1
		default:
			return 0
		}
	}

	producer := func(item *Item, tbl *CosParamTable) {
		tbl.Entries = append(tbl.Entries, &CosParamEntry{
			ID:        item.RefObjID + 10000,
			IsEngaged: true,
			Valid:     true,
		})
	}

	// Add pre-existing engaged and unengaged entries
	engaged := &CosParamEntry{ID: 101, IsEngaged: true, Valid: true}
	unengaged := &CosParamEntry{ID: 102, IsEngaged: false, Valid: true}
	table.Entries = []*CosParamEntry{engaged, unengaged}

	count, err := ReconcileCosParamEntries(inv, 0, 1001, predicate, producer, table)
	if err != nil {
		t.Fatalf("ReconcileCosParamEntries failed: %v", err)
	}

	// Total entries: 2 initial + 1 producer from slot 15 = 3
	if count != 3 {
		t.Fatalf("expected 3 entries, got %d", count)
	}
	if table.Entries[2].ID != 11500 {
		t.Fatalf("expected producer entry ID 11500, got %d", table.Entries[2].ID)
	}

	// Engaged entry must NOT be reset
	if engaged.ResetCounter != 0 {
		t.Fatalf("engaged entry was reset: %d", engaged.ResetCounter)
	}
	// Unengaged entry MUST be reset
	if unengaged.ResetCounter != 1 {
		t.Fatalf("unengaged entry was not reset: %d", unengaged.ResetCounter)
	}
	// Table must be finalized
	if !table.Finalized {
		t.Fatal("table was not finalized")
	}
}

// ----------------------------------------------------------------------------
// Test 4: End-to-end Split and Merge via Inventory.Transfer
// ----------------------------------------------------------------------------
func TestTransferSplitAndMergeIntegration(t *testing.T) {
	inv := New([]Item{
		{
			Slot:     13,
			RefObjID: 3630, // HP Potion
			Quantity: 20,
			RecordID: 8888,
		},
	}, domain.DefaultInventorySize)

	// Split 5 units into empty slot 14
	out, fault := inv.Transfer(13, 14, 5, 50)
	if fault != nil {
		t.Fatalf("Transfer split failed: %v", fault)
	}
	if out.Leg != LegSplit {
		t.Fatalf("expected LegSplit, got %s", out.Leg)
	}
	if inv.items[0].Quantity != 15 || inv.items[1].Quantity != 5 {
		t.Fatalf("unexpected quantities: %d and %d", inv.items[0].Quantity, inv.items[1].Quantity)
	}

	// Merge slot 14 back into slot 13
	inv.items[1].RecordID = 8889 // Assign valid ID
	outMerge, faultMerge := inv.Transfer(14, 13, 5, 50)
	if faultMerge != nil {
		t.Fatalf("Transfer merge failed: %v", faultMerge)
	}
	if outMerge.Leg != LegMerge {
		t.Fatalf("expected LegMerge, got %s", outMerge.Leg)
	}
	if len(inv.items) != 1 || inv.items[0].Quantity != 20 {
		t.Fatalf("unexpected items after merge: %+v", inv.items)
	}
}
