/*
===========================================================================

containertransfer.go - transfers complete item ownership between bounded containers

===========================================================================
*/
package inventory

import "opensro.online/server/internal/game/item/wire"

// TransferWholeTo applies a whole-source transfer between bag owners. The
// client's CPSMission_ApplyInventoryOperation supplies the source count to
// CPSMission_MergeOrSwapItems for both COS directions: compatible stacks
// merge (including the full-destination count exchange), otherwise swap.
/*
================
TransferWholeTo
================
*/
func (inv *Inventory) TransferWholeTo(destination *Inventory, sourceSlot, destinationSlot uint8, stackCap uint16) *Fault {
	if destination == nil || destination == inv || !inv.bagSlot(sourceSlot) || !destination.bagSlot(destinationSlot) {
		return newFault(wire.ErrCodeInvalidRequest, "invalidContainerTransfer")
	}
	i := inv.indexOf(sourceSlot)
	if i < 0 {
		return newFault(wire.ErrCodeInvalidRequest, "unavailableContainerSlot")
	}
	if fault := summonerTransferFault(inv.items[i]); fault != nil {
		return fault
	}
	j := destination.indexOf(destinationSlot)
	if j >= 0 {
		if fault := summonerTransferFault(destination.items[j]); fault != nil {
			return fault
		}
	}
	if j >= 0 {
		source, target := inv.items[i], destination.items[j]
		if IsEtcStackableTypeFlags(source.TypeFlags) && wholeStackIdentityMatches(source, target, stackCap) {
			if stackCap == 0 || source.Quantity == 0 || target.Quantity == 0 {
				return newFault(wire.ErrCodeInvalidRequest, "invalidContainerStack")
			}
			// Port-only, not native: lowering an operator cap does not corrupt
			// an existing consumable or stone stack. Keep the current cap for
			// pouring; the shared primitive preserves excess or swaps counts.
			if (source.Quantity > stackCap && !retainedOversizedStack(source, stackCap)) ||
				(target.Quantity > stackCap && !retainedOversizedStack(target, stackCap)) {
				return newFault(wire.ErrCodeInvalidRequest, "invalidContainerStack")
			}
			counts := TransferSlotStack(source.Quantity, target.Quantity, stackCap)
			destination.items[j].Quantity = counts.DestCount
			if counts.SourceRemainder == 0 {
				inv.items = append(inv.items[:i], inv.items[i+1:]...)
			} else {
				inv.items[i].Quantity = counts.SourceRemainder
			}
		} else {
			source.Slot = destinationSlot
			target.Slot = sourceSlot
			source = cloneInventoryRow(source)
			target = cloneInventoryRow(target)
			if source.Summon != nil {
				source.Summon.InventorySlot = destinationSlot
			}
			if target.Summon != nil {
				target.Summon.InventorySlot = sourceSlot
			}
			inv.items[i] = target
			destination.items[j] = source
		}
		return nil
	}
	row := inv.items[i]
	row.Slot = destinationSlot
	row = cloneInventoryRow(row)
	if row.Summon != nil {
		row.Summon.InventorySlot = destinationSlot
	}
	destination.items = append(destination.items, row)
	inv.items = append(inv.items[:i], inv.items[i+1:]...)
	return nil
}

/*
================
summonerItemFault

Validates the structural invariants of a summoner item row (type flags,
unit stack count, and COS record validity). Intra-bag moves and swaps
check only these invariants; an active companion's scroll may
move or swap freely inside the owner's bag.
================
*/
func summonerItemFault(item Item) *Fault {
	if !wire.IsCosSummoner(item.TypeFlags) && item.Summon == nil {
		return nil
	}
	if !wire.IsCosSummoner(item.TypeFlags) || item.Quantity != 1 || wire.ValidateCOSItem(item.TypeFlags, item.Summon) != nil {
		return newFault(wire.ErrCodeInvalidRequest, "summonerOwnershipConflict")
	}
	return nil
}

/*
================
summonerTransferFault

The native storage transfer and the ground drop/grant gates refuse a
summoner item whose companion is currently summoned (companion record
+0x38 & 2), so an active actor cannot cross an item container boundary.
================
*/
func summonerTransferFault(item Item) *Fault {
	if fault := summonerItemFault(item); fault != nil {
		return fault
	}
	if item.Summon != nil && item.Summon.Summoned {
		return newFault(wire.ErrCodeInvalidRequest, "summonerOwnershipConflict")
	}
	return nil
}
