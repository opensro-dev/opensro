/*
===========================================================================

expansion_test.go - the later inventory and warehouse addition items

===========================================================================
*/

package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
expansionCharacter

A character holding three of the addition item its codename names.
================
*/
func expansionCharacter(t *testing.T, codename string, refObjID uint32, params map[string]float64) (*enterworld.Character, *Runtime, *enterworld.ItemRef) {
	t.Helper()
	c := testCharacter()
	fields := map[string]float64{"canUse": 1, "maxStack": 10}
	for key, value := range params {
		fields[key] = value
	}
	item := &enterworld.ItemRef{RefObjID: refObjID, Codename: codename, Country: 3,
		TypeIDs: [4]int64{3, 3, 13, 17}, ReqQuadTypes: [4]int64{-1, -1, -1, -1},
		NativeFields: enterworld.NewNativeFields(fields)}
	items := testItems()
	items[item.Codename] = item
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 25, RefObjID: item.RefObjID,
		Codename: item.Codename, TypeFlags: item.TypeFlags(), StackCount: 3})
	rt, _ := newTestRuntime(c, items)
	return c, rt, item
}

/*
================
TestInventoryAdditionGrowsTheBagOnce

The addition grows the bag by its 32 slots, announced on 0x3092, and
the character's mark refuses a second one with nothing spent.
================
*/
func TestInventoryAdditionGrowsTheBagOnce(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_INVENTORY_ADDITION", 24405, map[string]float64{
		"itemParam1_29c": 32})
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	out := rt.HandleItemUse(testDivision, c, use)
	if out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("addition = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if c.InventoryCapacity() != 77 || !c.InventoryAdditionUsed {
		t.Fatalf("capacity %d, used %v", c.InventoryCapacity(), c.InventoryAdditionUsed)
	}
	announced := false
	for _, frame := range out.Frames {
		if frame.Opcode == wire.OpStorageCapacity {
			announced = len(frame.Payload) == 2 && frame.Payload[0] == 1 && frame.Payload[1] == 77
		}
	}
	if !announced {
		t.Fatalf("no 0x3092 bag announce in %+v", out.Frames)
	}
	assertItemUseRefusedUnchanged(t, rt, c, use, wire.ErrCodeInvalidRequest)
}

/*
================
TestInventoryAdditionTopsUpAFlooredBag

Under the inventory-pages floor the bag already holds 93: the
addition tops it up to the 109 ceiling and still spends its one use.
================
*/
func TestInventoryAdditionTopsUpAFlooredBag(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_INVENTORY_ADDITION", 24405, map[string]float64{
		"itemParam1_29c": 32})
	c.InventorySize = domain.InventoryPagesBase
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("addition = %+v / %q", out.Frames, out.DiagnosticRefusal)
	}
	if c.InventoryCapacity() != domain.MaxInventorySize || !c.InventoryAdditionUsed {
		t.Fatalf("capacity %d, used %v", c.InventoryCapacity(), c.InventoryAdditionUsed)
	}
}

/*
================
TestWarehouseAdditionGrowsTheRoomTwice

The warehouse addition grows the account room by 30 a use; the third
use refuses with nothing spent, and the room keeps its 210 slots.
================
*/
func TestWarehouseAdditionGrowsTheRoomTwice(t *testing.T) {
	c, rt, item := expansionCharacter(t, "ITEM_MALL_WAREHOUSE_ADDITION", 24406, map[string]float64{
		"itemParam1_29c": 30, "itemParam2_2a0": 1})
	authority := &memoryStorage{room: domain.NewAccountStorage()}
	rt.ConfigureStorage(authority)
	use := wire.NewWriter(3).U8(25).U16(item.TypeFlags()).Payload()
	for range 2 {
		if out := rt.HandleItemUse(testDivision, c, use); out.Frames[0].Payload[0] != wire.ResultSuccess {
			t.Fatalf("addition = %+v / %q", out.Frames, out.DiagnosticRefusal)
		}
	}
	if authority.room.Capacity != 210 || authority.room.ExpansionUses != 2 {
		t.Fatalf("room = %+v", authority.room)
	}
	assertItemUseRefusedUnchanged(t, rt, c, use, wire.ErrCodeInvalidRequest)
	if authority.room.Capacity != 210 {
		t.Fatalf("refused use grew the room: %+v", authority.room)
	}
}
