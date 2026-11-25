/*
===========================================================================

stackmerge.go - which bag stack an incoming item adds to

The lowest-slot stack of the same identity with room under its cap; a
pickup that finds one needs no free slot.

===========================================================================
*/
package inventory

// MergeTargetSlot returns the bag row a pickup of refObjID would merge onto:
// the LOWEST bag wire slot holding the same ref below stackCap. Lowest slot
// wins, not row order - the persisted row order is an artifact of past moves,
// and the destination must be reproducible. The second result is false when
// no mergeable row exists.
/*
================
MergeTargetSlot
================
*/
func (inv *Inventory) MergeTargetSlot(refObjID uint32, stackCap uint16) (uint8, bool) {
	return inv.mergeTargetSlot(Item{RefObjID: refObjID}, stackCap)
}

// CanMergeStack reports whether a pickup can add to a stack without a free slot.
func (inv *Inventory) CanMergeStack(item Item, stackCap uint16) bool {
	_, ok := inv.mergeTargetSlot(item, stackCap)
	return stackCap > 1 && ok
}

/*
================
mergeTargetSlot

Cargo grants carry the owner name; reference-only callers cannot merge into
an owned cargo stack without that identity.
================
*/
func (inv *Inventory) mergeTargetSlot(item Item, stackCap uint16) (uint8, bool) {
	best := -1
	for index := range inv.items {
		row := &inv.items[index]
		if !inv.bagSlot(row.Slot) || !stackIdentityMatches(*row, item) {
			continue
		}
		count := row.Quantity
		if count == 0 {
			count = 1
		}
		if count >= stackCap {
			continue
		}
		if best < 0 || row.Slot < inv.items[best].Slot {
			best = index
		}
	}
	if best < 0 {
		return 0, false
	}
	return inv.items[best].Slot, true
}
