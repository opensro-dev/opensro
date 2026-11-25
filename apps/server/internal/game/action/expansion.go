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
warehouse grows through the storage authority after the use's door
closes, on that authority's own lock; its capacity is read back at
every opening, so no live announce rides along.

Refusals (already used, no room left, no authority) spend nothing. The
refusal byte is the generic invalid-request: the later server's own
byte for an exhausted addition is not evidenced.

===========================================================================
*/

package action

import (
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

Runs inside the item use's character Update; the warehouse growth is
queued on after, which the caller runs once the door closes.
================
*/
func (rt *Runtime) useExpansion(use skillItemUse, character *enterworld.Character, tail []byte, result *OpResult, after *func()) bool {
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
		return rt.useInventoryAddition(use, character, int64(slots), result)
	case 1:
		return rt.useWarehouseAddition(use, character, int64(slots), result, after)
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
useWarehouseAddition

The account warehouse addition: the door reads the standing count to
refuse a third use before anything is spent; the storage authority's
own lock applies the growth after the door, re-checking the limit
there (a sibling character spending the last use first leaves this
item spent without growth - the two planes have no shared lock).
================
*/
func (rt *Runtime) useWarehouseAddition(use skillItemUse, character *enterworld.Character, slots int64, result *OpResult, after *func()) bool {
	if rt.storageAuthority == nil {
		return false
	}
	storage, err := rt.storageAuthority.AccountStorage(character)
	if err != nil {
		return false
	}
	if storage.ExpansionUses >= warehouseExpansionMaxUses || storage.Capacity+slots > domain.StorageMaxCapacity {
		*result = itemUseFailure(wire.ErrCodeInvalidRequest)
		return false
	}
	remaining := rt.consumeItemUseRow(character, use.row)
	*result = OpResult{Frames: []wire.Frame{
		{Opcode: wire.OpItemUseResponse, Payload: wire.EncodeItemUseSuccess(use.request.Slot, remaining, use.request.TypeWord)},
	}}
	result.Frames = append(result.Frames, rt.updateQuestInventory(character)...)
	*after = func() {
		_, _ = rt.storageAuthority.TransactStorage(character, func(_ *domain.Character, room *domain.AccountStorage) error {
			if room.ExpansionUses >= warehouseExpansionMaxUses || room.Capacity+slots > domain.StorageMaxCapacity {
				return nil
			}
			room.Capacity += slots
			room.ExpansionUses++
			return nil
		})
	}
	return true
}
