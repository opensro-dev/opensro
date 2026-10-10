/*
===========================================================================

stacksizes_test.go - SRO_STACK_SIZES reaches pickup and bag merges

Both settings: unset keeps the itemdata caps (potion 50, elixir 1); a
raise lets the same pickup and bag move fill one stack past them.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

const testElixir = "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"

/*
================
stackTestItems

testItems plus an elixir, with sizes applied through the production rule.
================
*/
func stackTestItems(sizes enterworld.StackSizes) staticItemSource {
	items := testItems()
	items[testElixir] = &enterworld.ItemRef{
		RefObjID: 3700, Codename: testElixir, TypeIDs: [4]int64{3, 3, 10, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 1}),
	}
	for _, ref := range items {
		sizes.Raise(ref)
	}
	return items
}

/*
================
TestStackSizesReachPickup

45 potions in the bag, 10 on the ground. Native: the stack fills to 50 and
5 stay on the ground. Raised: all 55 land in the one stack.
================
*/
func TestStackSizesReachPickup(t *testing.T) {
	for _, c := range []struct {
		name      string
		sizes     enterworld.StackSizes
		stack     int64
		remainder uint16
	}{
		{"native", nil, 50, 5},
		{"raised", enterworld.StackSizes{"potion": 2000}, 55, 0},
	} {
		t.Run(c.name, func(t *testing.T) {
			ch := testCharacter()
			flags := wire.PackTypeFlags(3, 3, 1, 1)
			row := enterworld.InventoryRow{Slot: 20, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: flags, StackCount: 10, VarianceBits: "0"}
			kept := row
			kept.Slot, kept.StackCount = 13, 45
			ch.MissionInventory = []enterworld.InventoryRow{row, kept}
			rt, _ := newTestRuntime(ch, stackTestItems(c.sizes))
			if r := rt.HandleItemMove(testDivision, ch, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20})); len(r.Frames) == 0 {
				t.Fatal("drop failed")
			}
			ground := rt.Ground.All(testDivision)
			if len(ground) != 1 {
				t.Fatalf("ground = %+v", ground)
			}
			rt.HandleTargetInteract(testDivision, ch, wire.TargetInteract{Gid: ground[0].Gid}.Encode())
			if len(ch.MissionInventory) != 1 || ch.MissionInventory[0].Slot != 13 || ch.MissionInventory[0].StackCount != c.stack {
				t.Fatalf("bag = %+v, want one stack of %d in slot 13", ch.MissionInventory, c.stack)
			}
			left, onGround := rt.Ground.Get(testDivision, ground[0].Gid)
			if onGround != (c.remainder != 0) || onGround && left.StackCount != c.remainder {
				t.Fatalf("ground remainder = %+v (present %v), want %d", left, onGround, c.remainder)
			}
		})
	}
}

/*
================
TestStackSizesReachBagMerge

Moving one elixir onto another. Native (cap 1): the two swap and stay
apart. Raised: they merge into one stack of two.
================
*/
func TestStackSizesReachBagMerge(t *testing.T) {
	for _, c := range []struct {
		name  string
		sizes enterworld.StackSizes
		rows  int
	}{
		{"native", nil, 2},
		{"raised", enterworld.StackSizes{"elixir": 50}, 1},
	} {
		t.Run(c.name, func(t *testing.T) {
			ch := testCharacter()
			flags := wire.PackTypeFlags(3, 3, 10, 1)
			row := enterworld.InventoryRow{Slot: 20, RefObjID: 3700, Codename: testElixir, TypeFlags: flags, StackCount: 1, VarianceBits: "0"}
			other := row
			other.Slot = 21
			ch.MissionInventory = []enterworld.InventoryRow{row, other}
			rt, _ := newTestRuntime(ch, stackTestItems(c.sizes))
			rt.HandleItemMove(testDivision, ch, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeInventory, SourceSlot: 20, DestSlot: 21, Quantity: 1}))
			total := int64(0)
			for _, r := range ch.MissionInventory {
				total += r.StackCount
			}
			if len(ch.MissionInventory) != c.rows || total != 2 {
				t.Fatalf("bag = %+v, want %d row(s) holding 2 elixirs", ch.MissionInventory, c.rows)
			}
			if c.rows == 1 && (ch.MissionInventory[0].Slot != 21 || ch.MissionInventory[0].StackCount != 2) {
				t.Fatalf("merged row = %+v, want 2 in slot 21", ch.MissionInventory[0])
			}
		})
	}
}
