package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"testing"
)

func TestEquipmentAmmunitionPairBranches(t *testing.T) {
	for _, kind := range []uint8{0, 2, 6, 12} {
		for _, ammo := range []uint8{1, 2} {
			c := testCharacter()
			rt, _ := newTestRuntime(c, testItems())
			rows := []inventory.Item{{Slot: 7, TypeFlags: wire.PackTypeFlags(3, 3, 4, ammo), Quantity: 50}}
			if kind != 0 {
				rows = append(rows, inventory.Item{Slot: 6, TypeFlags: wire.PackTypeFlags(3, 1, 6, kind), Quantity: 1})
			}
			inv := inventory.New(rows, domain.DefaultInventorySize)
			_, fault := rt.completeEquipmentPair(inv, 13, 7)
			allowed := kind == 6 && ammo == 1 || kind == 12 && ammo == 2
			if (fault == nil) != allowed {
				t.Fatalf("weapon=%d ammo=%d fault=%v", kind, ammo, fault)
			}
		}
	}
}
func TestWeaponChangeCompanionMoves(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testItems())
	sword := inventory.Item{Slot: 6, TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Quantity: 1}
	arrow := inventory.Item{Slot: 7, TypeFlags: wire.PackTypeFlags(3, 3, 4, 1), Quantity: 50}
	inv := inventory.New([]inventory.Item{sword, arrow}, domain.DefaultInventorySize)
	moves, fault := rt.completeEquipmentPair(inv, 13, 6)
	if fault != nil || len(moves) != 1 || moves[0].SourceSlot != 7 || moves[0].DestSlot != 13 {
		t.Fatalf("stash: %v %v", moves, fault)
	}
	// Bow admission automatically takes the first matching bag stack.
	sword.TypeFlags = wire.PackTypeFlags(3, 1, 6, 6)
	arrow.Slot = 18
	inv = inventory.New([]inventory.Item{sword, arrow}, domain.DefaultInventorySize)
	moves, fault = rt.completeEquipmentPair(inv, 13, 6)
	if fault != nil || len(moves) != 1 || moves[0].SourceSlot != 18 || moves[0].DestSlot != 7 {
		t.Fatalf("auto ammo: %v %v", moves, fault)
	}
	if _, fault := inv.Transfer(7, 18, 50, 1); fault != nil {
		t.Fatal(fault)
	}
	moves, fault = rt.completeEquipmentPair(inv, 7, 18)
	if fault != nil || len(moves) != 0 {
		t.Fatalf("manual offhand removal must not refill: %v %v", moves, fault)
	}
}

func TestEquipmentPairPacketRollback(t *testing.T) {
	for _, full := range []bool{false, true} {
		c := testCharacter()
		items := testItems()
		items["ARROW"] = &enterworld.ItemRef{RefObjID: 999, Codename: "ARROW", Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, TypeIDs: [4]int64{3, 3, 4, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 250})}
		arrow := enterworld.InventoryRow{Slot: 21, RefObjID: 999, Codename: "ARROW", TypeFlags: wire.PackTypeFlags(3, 3, 4, 1), StackCount: 50}
		if !full {
			c.MissionInventory[0].Slot = 6
			c.MissionInventory = append(c.MissionInventory, arrow)
		} else {
			arrow.Slot = 7
			c.MissionInventory = append(c.MissionInventory, arrow)
			current := c.MissionInventory[0]
			current.Slot = 6
			c.MissionInventory = append(c.MissionInventory, current)
			for slot := uint8(13); slot < 45; slot++ {
				if slot != 20 {
					row := arrow
					row.Slot = int64(slot)
					c.MissionInventory = append(c.MissionInventory, row)
				}
			}
		}
		before := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
		rt, _ := newTestRuntime(c, items)
		source, dest := uint8(21), uint8(7)
		code := byte(wire.ErrCodeCantEquip)
		if full {
			source = 20
			dest = 6
			code = wire.ErrCodeStorageFull
		}
		result := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: source, DestSlot: dest, Quantity: 1}))
		if len(result.Frames) != 1 || !reflect.DeepEqual(result.Frames[0].Payload, []byte{2, code}) {
			t.Fatalf("full=%v result=%+v", full, result)
		}
		if !reflect.DeepEqual(before, c.MissionInventory) {
			t.Fatal("refused equipment transaction mutated inventory")
		}
	}
}
func TestShieldUnseatsTwoHandedWeapon(t *testing.T) {
	c := testCharacter()
	items := testItems()
	items["ITEM_CH_SWORD_01_A_RARE"].NativeFields = enterworld.NewNativeFields(map[string]float64{"twoHanded": 1})
	rt, _ := newTestRuntime(c, items)
	inv := inventory.New([]inventory.Item{{Slot: 6, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), Quantity: 1}, {Slot: 7, TypeFlags: wire.PackTypeFlags(3, 1, 4, 1), Quantity: 1}}, domain.DefaultInventorySize)
	moves, fault := rt.completeEquipmentPair(inv, 13, 7)
	if fault != nil || len(moves) != 1 || moves[0].SourceSlot != 6 {
		t.Fatalf("%v %v", moves, fault)
	}
}

/*
================
TestBowEquipClearsDisplacedShieldVisual

Equipping a bow over a sword and shield: the pair-swap seats the bag's
arrows in socket 7 and the shield in the arrows' bag slot. The visuals
must draw the bow (0x3314) and clear socket 7 (0x377C) - ammunition has
no worn visual (natively, no equipment visual), so a 0x3314
carrying the arrows was ignored by the client and the shield stayed
drawn (the native move's unequip arm).
================
*/
func TestBowEquipClearsDisplacedShieldVisual(t *testing.T) {
	c := testCharacter()
	items := testItems()
	items["BOW"] = &enterworld.ItemRef{RefObjID: 998, Codename: "BOW", Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, TypeIDs: [4]int64{3, 1, 6, 6}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}
	items["ITEM_CH_SHIELD_01"] = &enterworld.ItemRef{RefObjID: 997, Codename: "ITEM_CH_SHIELD_01", Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, TypeIDs: [4]int64{3, 1, 4, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1})}
	items["ARROW"] = &enterworld.ItemRef{RefObjID: 999, Codename: "ARROW", Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, TypeIDs: [4]int64{3, 3, 4, 1}, NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 250})}
	c.MissionInventory = []enterworld.InventoryRow{
		{Slot: 6, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE", TypeFlags: wire.PackTypeFlags(3, 1, 6, 2), StackCount: 1},
		{Slot: 7, RefObjID: 997, Codename: "ITEM_CH_SHIELD_01", TypeFlags: wire.PackTypeFlags(3, 1, 4, 1), StackCount: 1},
		{Slot: 20, RefObjID: 998, Codename: "BOW", TypeFlags: wire.PackTypeFlags(3, 1, 6, 6), StackCount: 1},
		{Slot: 21, RefObjID: 999, Codename: "ARROW", TypeFlags: wire.PackTypeFlags(3, 3, 4, 1), StackCount: 50},
	}
	rt, _ := newTestRuntime(c, items)
	result := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 6, Quantity: 1}))
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemMoveResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("bow equip refused: %+v", result)
	}
	var equipRefs []uint32
	var unequipSlots []uint8
	for _, frame := range result.Frames {
		switch frame.Opcode {
		case wire.OpEquipVisual:
			equipRefs = append(equipRefs, uint32(frame.Payload[5])|uint32(frame.Payload[6])<<8|uint32(frame.Payload[7])<<16|uint32(frame.Payload[8])<<24)
		case wire.OpUnequipVisual:
			unequipSlots = append(unequipSlots, frame.Payload[4])
		}
	}
	if len(equipRefs) != 1 || equipRefs[0] != 998 {
		t.Fatalf("equip visuals %v, want only the bow (998)", equipRefs)
	}
	if len(unequipSlots) != 1 || unequipSlots[0] != 7 {
		t.Fatalf("unequip visuals %v, want one clear of socket 7", unequipSlots)
	}
	slots := map[int64]uint32{}
	for _, row := range c.MissionInventory {
		slots[row.Slot] = uint32(row.RefObjID)
	}
	if slots[6] != 998 || slots[7] != 999 || slots[20] != 11459 || slots[21] != 997 {
		t.Fatalf("post-swap rows %v, want bow 6, arrows 7, sword 20, shield 21", slots)
	}
}
