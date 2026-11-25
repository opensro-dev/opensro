/*
===========================================================================

expansion.go - the later inventory and warehouse addition items

The two v1.696 mall items (3/3/13/17) each author Param1 as the slots
they add. ITEM_MALL_INVENTORY_ADDITION adds Param1 (32) bag slots and
its text limits it to one time per character; ITEM_MALL_WAREHOUSE_
ADDITION carries Param2 = 1, adds Param1 (30) warehouse slots and is
limited to two times per account. Neither type stands in v1.150; the
build stages the rows from the v1.696 mall data (#4240 part 3,
scripts/later_mall_items.py).

The bag grows through the same live rule the quest expansions use
(GrantInventoryExpansion, announced with 0x3092); the port's
inventory-pages floor (93) leaves less than the full 32 under the 109
ceiling, so a floored character is topped up to the ceiling instead of
refused a bought item - the one-time mark is spent either way. The
warehouse grows through the storage authority on that authority's own
lock; its capacity is read back at every opening, so no live announce
rides along. Like the name change, the warehouse use checks inside
the door but spends after it: the growth commits first and the item
is spent only when it did, so a refused or failed growth leaves the
item in the bag.

Refusals (already used, no room left, no authority) spend nothing. The
refusal byte is the generic invalid-request: the later server's own
byte for an exhausted addition is not evidenced.

===========================================================================
*/

package action

import (
	log "github.com/sirupsen/logrus"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// warehouseExpansionMaxUses is the account limit the item's v1.696 text
// states ("Limited to 2 times only per account").
const warehouseExpansionMaxUses int64 = 2

/*
================
useExpansion

Runs inside the item use's character Update. The warehouse kind only
checks there and names the pending use on warehouse; the caller
finishes it after the door (finishWarehouseAddition).
================
*/
func (rt *Runtime) useExpansion(use skillItemUse, character *enterworld.Character, tail []byte, result *OpResult, warehouse **pendingWarehouseUse) bool {
	if len(tail) != 0 {
		return false
	}
	slotsValue, okSlots := use.ref.NativeFields.Lookup("itemParam1_29c")
	kind, _ := use.ref.NativeFields.Lookup("itemParam2_2a0")
	if !okSlots || slotsValue <= 0 || slotsValue > float64(domain.MaxInventorySize) {
		return false
	}
	slots := int64(slotsValue)
	switch kind {
	case 0:
		return rt.useInventoryAddition(use, character, slots, result)
	case 1:
		return rt.admitWarehouseAddition(use, slots, warehouse)
	}
	return false
}

/*
================
useInventoryAddition

The one-time bag addition: the character's own record carries the use,
so a second item finds the mark and refuses.
================
*/
func (rt *Runtime) useInventoryAddition(use skillItemUse, character *enterworld.Character, slots int64, result *OpResult) bool {
	if character.InventoryAdditionUsed {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return false
	}
	room := int64(domain.MaxInventorySize) - int64(character.InventoryCapacity()) - int64(character.InventoryExpansion)
	if slots > room {
		slots = room
	}
	if slots <= 0 || !character.GrantInventoryExpansion(uint8(slots)) {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return false
	}
	character.InventoryAdditionUsed = true
	remaining := rt.consumeItemUseRow(character, use.row)
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
		{Opcode: wire.OpStorageCapacity,
			Payload: wire.EncodeStorageCapacity(wire.StorageCapacityInventory, character.InventoryCapacity())},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
	return true
}

/*
================
pendingWarehouseUse

A warehouse addition the door admitted and the finish (after the
door) has yet to settle: the slots it would add, and the ref and
request the spent row is found and answered by.
================
*/
type pendingWarehouseUse struct {
	slots   int64
	ref     *enterworld.ItemRef
	request wire.ItemUseRequest
}

/*
================
admitWarehouseAddition

The door's half of the warehouse addition: queue the pending use and
spend nothing here. It reads no storage. This runs inside the item
use's character door, which holds the store lock, and the storage
authority's read takes that same lock: a read here deadlocked the
live shard (tick stall 2026-11-25 22:04Z). Never open a door inside
a door. The account limit is checked once, by finishWarehouseAddition
under the storage lock after the door, which answers a third use with
the same failure; a growth it refuses (a sibling character spent the
last use first) leaves the item in the bag.
================
*/
func (rt *Runtime) admitWarehouseAddition(use skillItemUse, slots int64, warehouse **pendingWarehouseUse) bool {
	if rt.storageAuthority == nil {
		return false
	}
	*warehouse = &pendingWarehouseUse{slots: slots, ref: use.ref, request: use.request}
	return false
}

/*
================
finishWarehouseAddition

The warehouse addition after the door, as the name change finishes
(namechange.go): the growth commits under the storage authority's
own lock first, re-checking the account limit there; the item is
spent under a fresh character door only when the growth committed.
A refusal at the commit, a store error, or a spent row that somehow
vanished answers an item-use failure with the item still in the bag.
================
*/
func (rt *Runtime) finishWarehouseAddition(divisionID string, character *enterworld.Character, pending *pendingWarehouseUse, result *OpResult) {
	grew := false
	_, err := rt.storageAuthority.TransactStorage(character, func(_ *domain.Character, room *domain.AccountStorage) error {
		if room.ExpansionUses >= warehouseExpansionMaxUses || room.Capacity+pending.slots > domain.StorageMaxCapacity {
			return nil
		}
		room.Capacity += pending.slots
		room.ExpansionUses++
		grew = true
		return nil
	})
	if err != nil {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		// The error goes to the log: the refusal reason is a counter
		// key (countDiagnosticRefusal) and stays a fixed literal, as
		// the name change's does.
		log.Warnf("action: warehouse addition growth for %q in %s failed: %v", character.Name, divisionID, err)
		result.DiagnosticRefusal = "item-use: warehouse growth door failed"
		return
	}
	if !grew {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return
	}
	consumed := rt.deps.Update(character, "item-use-expansion", func() bool {
		rowIndex := -1
		for index := range character.MissionInventory {
			if character.MissionInventory[index].Slot == int64(pending.request.Slot) &&
				character.MissionInventory[index].Codename == pending.ref.Codename {
				rowIndex = index
				break
			}
		}
		if rowIndex < 0 {
			return false
		}
		remaining := rt.consumeItemUseRow(character, rowIndex)
		*result = OpResult{Frames: []wire.Frame{{Opcode: wire.OpItemUseResponse,
			Payload: wire.EncodeItemUseSuccess(pending.request.Slot, remaining, pending.request.TypeWord)}}}
		result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
		return true
	})
	if !consumed {
		log.Warnf("action: warehouse addition for %q in %s grew but its item row is gone; left unspent", character.Name, divisionID)
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		result.DiagnosticRefusal = "item-use: warehouse grew but the item row vanished before it could be spent"
		return
	}
	rt.publishItemUseVisual(character, pending.ref, 0, result)
}
