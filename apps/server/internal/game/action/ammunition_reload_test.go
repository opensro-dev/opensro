/*
===========================================================================

ammunition_reload_test.go - spending the last arrow reloads from the bag

The reference server's projectile action sends the socket's new count (0 when
the stack ran out) before it consumes the ammunition, whose auto-reload then
moves the lowest same-type bag stack into the emptied socket 7.

===========================================================================
*/
package action

import (
	"bytes"
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
	result := applyAmmunitionDebit(character, ammunitionDebit{index: 0, remaining: 0}, false)
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
	zero := wire.AvatarInventorySlot7StackCountFrame(0)
	if len(frames) != 2 || frames[0].Opcode != zero.Opcode || !bytes.Equal(frames[0].Payload, zero.Payload) ||
		frames[1].Opcode != wire.OpItemMoveResponse {
		t.Fatalf("frames = %+v, want the 0x3752 zero count before the move response", frames)
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
	result := applyAmmunitionDebit(character, ammunitionDebit{index: 0, remaining: 0}, false)
	if result.reload != nil || result.count != 0 || len(character.MissionInventory) != 0 {
		t.Fatalf("result %+v, inventory %+v: want an empty socket", result, character.MissionInventory)
	}
	if frames := ammunitionFrames(result); len(frames) != 1 {
		t.Fatalf("frames = %d, want only the zero count", len(frames))
	}
}

/*
================
TestSmallestStackReloadPicksSmallestStack

The Experimental smallest-stack choice (#4527, port-only) takes the
smallest matching stack instead of the native lowest slot.
================
*/
func TestSmallestStackReloadPicksSmallestStack(t *testing.T) {
	character := &enterworld.Character{MissionInventory: []enterworld.InventoryRow{
		{Slot: int64(inventory.SocketShield), RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: 1},
		{Slot: 15, RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: 250},
		{Slot: 20, RefObjID: 9999, TypeFlags: arrowTypeFlags, StackCount: 40},
	}}
	result := applyAmmunitionDebit(character, ammunitionDebit{index: 0, remaining: 0}, true)
	if result.reload == nil || result.reload.from != 20 || result.count != 40 {
		t.Fatalf("reload = %+v count %d, want the slot 20 stack of 40", result.reload, result.count)
	}
	for _, row := range character.MissionInventory {
		if row.Slot == 15 && row.StackCount != 250 {
			t.Fatalf("slot 15 = %+v, want the 250 stack left in the bag", row)
		}
	}
}

/*
================
TestSmallestStackReloadTieGoesToTheLowerSlot

Equal stacks under the smallest-stack choice still go to the lower
slot, so the choice never scrambles a bag of equal stacks.
================
*/
func TestSmallestStackReloadTieGoesToTheLowerSlot(t *testing.T) {
	character := &enterworld.Character{MissionInventory: []enterworld.InventoryRow{
		{Slot: int64(inventory.SocketShield), RefObjID: 62, TypeFlags: arrowTypeFlags, StackCount: 1},
		{Slot: 20, RefObjID: 9999, TypeFlags: arrowTypeFlags, StackCount: 40},
		{Slot: 15, RefObjID: 9999, TypeFlags: arrowTypeFlags, StackCount: 40},
	}}
	result := applyAmmunitionDebit(character, ammunitionDebit{index: 0, remaining: 0}, true)
	if result.reload == nil || result.reload.from != 15 || result.count != 40 {
		t.Fatalf("reload = %+v count %d, want the slot 15 stack of 40", result.reload, result.count)
	}
}
