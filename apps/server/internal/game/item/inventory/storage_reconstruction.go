/*
===========================================================================

storage_reconstruction.go - copies and splits portable item records without mutable aliases

===========================================================================
*/
package inventory

import (
	"fmt"
	"opensro.online/server/internal/domain"

	"opensro.online/server/internal/game/item/wire"
)

// PARTIAL GetSplitEligibility: ordinary stack arithmetic projection of
// 0x004B9D40 / 0x00490230. Subtype/options and COS restrictions
// are not represented here; this is not a complete native eligibility gate.
// Checks whether the target slot can merge an incoming item.
// Native 0x004B9D40 specifically checks record+20/+24 != 0 (target.RecordID != 0).
// If RecordID == 0, returns 0 even if other fields match.
/*
================
GetSplitEligibility
================
*/
func (inv *Inventory) GetSplitEligibility(slot int32, incoming *Item, stackCap uint16) int32 {
	if inv == nil || incoming == nil || slot < 0 || slot >= int32(inv.slotEnd) {
		return 0
	}
	target, ok := inv.At(uint8(slot))
	if !ok {
		return 0
	}
	// Native 004B9D40 check: record+20/+24 != 0
	if target.RecordID == 0 || wire.IsCosSummoner(target.TypeFlags) || wire.IsCosSummoner(incoming.TypeFlags) || target.Summon != nil || incoming.Summon != nil {
		return 0
	}
	if !stackIdentityMatches(target, *incoming) {
		return 0
	}
	if stackCap == 0 {
		return 0 // Unknown reference-data capacity is not permission to assume 50.
	}
	if target.Quantity >= stackCap {
		return 0
	}
	space := stackCap - target.Quantity
	mergeQty := incoming.Quantity
	if mergeQty > space {
		mergeQty = space
	}
	return int32(mergeQty)
}

// splitInventoryRow projects native 4B9C60's zero record identity onto a new
// portable row. Mutable options must not alias the source record.
/*
================
cloneInventoryRow
================
*/
func cloneInventoryRow(source Item) Item {
	row := source
	row.MagicOptions = append([]uint64(nil), source.MagicOptions...)
	row.Summon = domain.CloneCOS(source.Summon)
	return row
}

/*
================
splitInventoryRow
================
*/
func splitInventoryRow(source Item, slot uint8, quantity uint16) Item {
	row := cloneInventoryRow(source)
	row.Slot, row.Quantity, row.RecordID = slot, quantity, 0
	return row
}

// PARTIAL portable SplitItem; not complete native 0x004B9C60 factory closure.
// Splits quantity from sourceSlot into empty destSlot with rollback protection.
// Newly split item in native Silkroad starts with RecordID == 0 (unpersisted in DB)
// until assigned by the persistence layer or an optional identity allocator.
/*
================
SplitItem
================
*/
func (inv *Inventory) SplitItem(sourceSlot, destSlot uint8, quantity uint16, idGen func() uint64) (*Item, *Fault) {
	if inv == nil || !inv.bagSlot(sourceSlot) || !inv.bagSlot(destSlot) {
		return nil, newFault(wire.ErrCodeInvalidRequest, "slotOutOfRange")
	}
	if quantity == 0 {
		return nil, newFault(wire.ErrCodePositiveNumberOnly, "splitQuantityNotPositive")
	}
	srcIdx := inv.indexOf(sourceSlot)
	if srcIdx < 0 {
		return nil, newFault(wire.ErrCodeInvalidRequest, "sourceSlotEmpty")
	}
	if inv.indexOf(destSlot) >= 0 {
		return nil, newFault(wire.ErrCodeInvalidRequest, "destSlotOccupied")
	}
	srcItem := inv.items[srcIdx]
	if wire.IsCosSummoner(srcItem.TypeFlags) || srcItem.Summon != nil {
		return nil, newFault(wire.ErrCodeInvalidRequest, "summonerCannotSplit")
	}
	if srcItem.Quantity <= quantity {
		return nil, newFault(wire.ErrCodeInputFewerThanRemain, "splitQuantityOverRemain")
	}

	// Prepare all allocating/callback work before mutating source stock. Never
	// retain a slice-element pointer across append, which may relocate storage.
	var newRecordID uint64
	if idGen != nil {
		newRecordID = idGen()
	}

	newItem := splitInventoryRow(srcItem, destSlot, quantity)
	newItem.RecordID = newRecordID
	inv.items = append(inv.items, newItem)
	inv.items[srcIdx].Quantity = srcItem.Quantity - quantity
	return &inv.items[len(inv.items)-1], nil
}

// CosParamEntry represents an entry evaluated during COS param reconciliation.
/*
================
CosParamEntry
================
*/
type CosParamEntry struct {
	ID           uint32
	IsEngaged    bool
	ResetCounter int32
	Valid        bool
}

/*
================
Validate
================
*/
func (e *CosParamEntry) Validate() bool {
	return e != nil && e.Valid
}

/*
================
Reset
================
*/
func (e *CosParamEntry) Reset() {
	if e != nil {
		e.ResetCounter++
	}
}

/*
================
CosParamTable
================
*/
type CosParamTable struct {
	Entries   []*CosParamEntry
	Finalized bool
}

/*
================
Finalize
================
*/
func (t *CosParamTable) Finalize() {
	if t != nil {
		t.Finalized = true
	}
}

type CosItemPredicate func(item *Item) int32
type CosSubtypeProducer func(item *Item, table *CosParamTable)

// PARTIAL portable ReconcileCosParamEntries (0x004BA940): subtype producers and
// actual COS lifecycle/DB finalization must be supplied by the owning subsystem.
// Reconciles COS parameter table across container slots.
// Native guards:
//   - storageKind MUST be 0 (inventory), 3 (guild), or 4 (chest).
//   - ownerID MUST be non-zero (non-null owner).
//
// Start slot:
//   - Kind 0 starts at slot 13.
//   - Kinds 3 and 4 start at slot 0.
//
// Predicate & Producer:
//   - For each slot, calls predicate(item). If predicate == 1, calls producer.
//   - If predicate is 0 or 2, producer is skipped.
//
// Entry scan:
//   - Reads entry count after producers execute.
//   - Only resets unengaged entries (IsEngaged == false).
//   - Finalizes table via sub_431550.
/*
================
ReconcileCosParamEntries
================
*/
func ReconcileCosParamEntries(
	inv *Inventory,
	storageKind int32,
	ownerID uint32,
	predicate CosItemPredicate,
	producer CosSubtypeProducer,
	table *CosParamTable,
) (int, error) {
	if inv == nil || table == nil {
		return -1, fmt.Errorf("nil inventory or table")
	}
	if (storageKind != 0 && storageKind != 3 && storageKind != 4) || ownerID == 0 {
		return -1, fmt.Errorf("native guard: invalid storageKind %d or null owner %d", storageKind, ownerID)
	}
	if predicate == nil || producer == nil {
		return -1, fmt.Errorf("COS reconciliation requires predicate and subtype producer")
	}

	startSlot := int32(0)
	if storageKind == 0 {
		startSlot = 13
	}

	for slot := startSlot; slot < int32(inv.slotEnd); slot++ {
		if item, ok := inv.At(uint8(slot)); ok {
			if predicate(&item) == 1 {
				producer(&item, table)
			}
		}
	}

	entryCount := len(table.Entries)
	for _, entry := range table.Entries {
		if entry == nil || !entry.Validate() {
			return -2, fmt.Errorf("native guard: invalid COS entry in table")
		}
		if !entry.IsEngaged {
			entry.Reset()
		}
	}

	table.Finalize()
	return entryCount, nil
}
