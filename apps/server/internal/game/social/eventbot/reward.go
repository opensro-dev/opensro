/*
===========================================================================

reward.go - paying a win into a live character's bag

The normal item path, the way the starter kit pays: rows land in the
character's mission inventory inside the caller's store door (the
wiring's Grant door, labelled domain's GrantEventBotReward), so the
item ledger writes the audit row and the free-grant day count moves
in the same commit. Existing stacks of the same item fill to their
itemdata cap first; the rest takes free bag slots, split at the cap.
Nothing here touches gold: a win can only ever add the file's item.

Port-only, not native.

===========================================================================
*/
package eventbot

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
AddReward

Adds count of the item to the character's bag; false when the bag
has no room for all of it (nothing is added then - a win pays in
full or not at all). The caller holds the character's mutation door.
================
*/
func AddReward(character *enterworld.Character, ref *enterworld.ItemRef, count int) bool {
	if character == nil || ref == nil || count < 1 {
		return false
	}
	typeFlags := wire.PackTypeFlags(uint8(ref.TypeIDs[0]), uint8(ref.TypeIDs[1]), uint8(ref.TypeIDs[2]), uint8(ref.TypeIDs[3]))
	capacity := int64(stackCap(ref, typeFlags))
	rows := append([]enterworld.InventoryRow(nil), character.MissionInventory...)
	remaining := int64(count)
	// Fill matching stacks first (same item, same flags, same
	// variance - a bound stack never swallows an unbound prize).
	for i := range rows {
		if remaining == 0 {
			break
		}
		row := &rows[i]
		if row.RefObjID != ref.RefObjID || row.TypeFlags != typeFlags || row.StackCount >= capacity {
			continue
		}
		add := capacity - row.StackCount
		if add > remaining {
			add = remaining
		}
		row.StackCount += add
		remaining -= add
	}
	// The rest takes free bag slots, one stack each.
	used := make(map[int64]bool, len(rows))
	for _, row := range rows {
		used[row.Slot] = true
	}
	for remaining > 0 {
		slot, ok := freeBagSlot(character, used)
		if !ok {
			return false
		}
		add := remaining
		if add > capacity {
			add = capacity
		}
		rows = append(rows, enterworld.InventoryRow{
			Slot:         slot,
			RefObjID:     ref.RefObjID,
			Codename:     ref.Codename,
			TypeFlags:    typeFlags,
			VarianceBits: "0",
			StackCount:   add,
		})
		used[slot] = true
		remaining -= add
	}
	character.MissionInventory = rows
	return true
}

/*
================
stackCap

The item's stack ceiling: 1 for anything outside the stackable ETC
family, else the itemdata MaxStack (an unknown cap stacks to the
count, the scroll case - such rows are born whole from the file).
================
*/
func stackCap(ref *enterworld.ItemRef, typeFlags uint16) int {
	if !inventory.IsEtcStackableTypeFlags(typeFlags) {
		return 1
	}
	if ref.NativeMaxStack > 1 {
		return int(ref.NativeMaxStack)
	}
	return 1 << 30
}

/*
================
freeBagSlot

The first free bag slot, the starter kit's scan (starterkit.go).
================
*/
func freeBagSlot(character *enterworld.Character, used map[int64]bool) (int64, bool) {
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.BagEnd(character)); slot++ {
		if !used[slot] {
			return slot, true
		}
	}
	return 0, false
}
