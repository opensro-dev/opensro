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
