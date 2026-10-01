/*
===========================================================================

ammunition_reload_test.go - spending the last arrow reloads from the bag

CGObjPC_AutoReloadMagazine (4EC340) moves the lowest same-type bag stack into
socket 7; the client must see that move before the new absolute count.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

const arrowTypeFlags = 2668 // TID 3.3.4.1

/*
================
TestLastArrowReloadsLowestSameTypeBagStack
================
*/
func TestLastArrowReloadsLowestSameTypeBagStack(t *testing.T) {
	character := &enterworld.Character{MissionInventory: []enterworld.InventoryRow{
		{Slot: int64(inventory.SocketShield), RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: 1},
		{Slot: 20, RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: 250},
		{Slot: 15, RefObjID: 9999, TypeFlags: arrowTypeFlags, StackCount: 40},
		{Slot: 14, RefObjID: 10376, TypeFlags: 4716, StackCount: 99}, // bolts: another TID
	}}
	result := applyAmmunitionDebit(character, ammunitionDebit{index: 0, remaining: 0})
	if result.reload == nil || result.reload.from != 15 || result.count != 40 {
		t.Fatalf("reload = %+v count %d, want the slot 15 stack of 40", result.reload, result.count)
	}
	equipped := 0
	for _, row := range character.MissionInventory {
		if row.Slot == int64(inventory.SocketShield) {
			equipped++
			if row.StackCount != 40 || row.RefObjID != 9999 {
				t.Fatalf("socket 7 holds %+v, want the reloaded stack", row)
			}
		}
	}
	if equipped != 1 || len(character.MissionInventory) != 3 {
		t.Fatalf("inventory = %+v, want the spent row gone and one socket-7 stack", character.MissionInventory)
	}
	frames := ammunitionFrames(result)
	if len(frames) != 2 || frames[0].Opcode != wire.OpItemMoveResponse || frames[1].Opcode != wire.AvatarInventorySlot7StackCountFrame(0).Opcode {
		t.Fatalf("frames = %+v, want the move response before the 0x3752 count", frames)
	}
}

/*
================
TestLastArrowWithoutBagStockEmptiesSocket
================
*/
func TestLastArrowWithoutBagStockEmptiesSocket(t *testing.T) {
	character := &enterworld.Character{MissionInventory: []enterworld.InventoryRow{
		{Slot: int64(inventory.SocketShield), RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: 1},
	}}
	result := applyAmmunitionDebit(character, ammunitionDebit{index: 0, remaining: 0})
	if result.reload != nil || result.count != 0 || len(character.MissionInventory) != 0 {
		t.Fatalf("result %+v, inventory %+v: want an empty socket", result, character.MissionInventory)
	}
	if frames := ammunitionFrames(result); len(frames) != 1 {
		t.Fatalf("frames = %d, want only the zero count", len(frames))
	}
}
