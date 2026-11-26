/*
===========================================================================

magiccube_transfer_test.go - drop, trade and stall keep linked elixirs

Port-only (#3906). A non-empty cube may leave the bag through drop,
exchange or stall: its LinkedStorage and Data travel with the item, so
pickup / the other bag never silently empties it. Merchant sale and
consignment still refuse (magiccube_sale_test.go, market admit).

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
testCubeWithElixir

A 10-slot weapon cube holding one reinforce recipe.
================
*/
func testCubeWithElixir() inventory.Item {
	return inventory.Item{
		Slot: 13, RefObjID: 35568, Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
		TypeFlags: wire.PackTypeFlags(3, 2, 3, 1), Quantity: 1, Data: 1,
		LinkedStorage: &domain.COSContainer{
			Capacity: 10,
			Rows: []domain.InventoryRow{{
				Slot: 0, RefObjID: 2151, Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A",
				TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), StackCount: 1,
			}},
		},
	}
}

/*
================
TestPlanItemDropKeepsMagicCubeLinkedStorage
================
*/
func TestPlanItemDropKeepsMagicCubeLinkedStorage(t *testing.T) {
	cube := testCubeWithElixir()
	at := simulation.Spawn{RegionID: 0x62A8, X: 10, Y: 2, Z: 20, Angle: 1}
	dropped := PlanItemDrop(cube, 1, at, "holder", time.Unix(100, 0))
	if dropped.Data != 1 || dropped.LinkedStorage == nil || len(dropped.LinkedStorage.Rows) != 1 {
		t.Fatalf("drop lost linked storage: data=%d storage=%+v", dropped.Data, dropped.LinkedStorage)
	}
	if dropped.LinkedStorage.Rows[0].Codename != "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A" {
		t.Fatalf("drop elixir = %+v", dropped.LinkedStorage.Rows[0])
	}
	restored := groundInventoryItem(dropped, 1)
	if restored.Data != 1 || restored.LinkedStorage == nil || len(restored.LinkedStorage.Rows) != 1 {
		t.Fatalf("pickup lost linked storage: %+v", restored)
	}
	if restored.LinkedStorage == cube.LinkedStorage {
		t.Fatal("pickup aliased the bag's linked storage")
	}
}

/*
================
TestMagicCubeExchangeOfferableKeepsLinkedStorage
================
*/
func TestMagicCubeExchangeOfferableKeepsLinkedStorage(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.MagicCube = true
	cube := testCubeWithElixir()
	admitTradeableCube(rt, cube)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: int64(cube.Slot), RefObjID: cube.RefObjID, Codename: cube.Codename,
		TypeFlags: cube.TypeFlags, StackCount: int64(cube.Quantity), Data: cube.Data,
		LinkedStorage: domain.CloneCOSContainer(cube.LinkedStorage),
	})
	item, code := rt.exchangeOfferable(c, 13)
	if code != 0 {
		t.Fatalf("exchangeOfferable code = %d, want 0", code)
	}
	if item.Data != 1 || item.LinkedStorage == nil || len(item.LinkedStorage.Rows) != 1 {
		t.Fatalf("offer lost linked storage: %+v", item)
	}
}

/*
================
TestMagicCubeStallOfferableKeepsLinkedStorage
================
*/
func TestMagicCubeStallOfferableKeepsLinkedStorage(t *testing.T) {
	rt, c := merchantFixture(t)
	rt.MagicCube = true
	cube := testCubeWithElixir()
	admitTradeableCube(rt, cube)
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
		Slot: int64(cube.Slot), RefObjID: cube.RefObjID, Codename: cube.Codename,
		TypeFlags: cube.TypeFlags, StackCount: int64(cube.Quantity), Data: cube.Data,
		LinkedStorage: domain.CloneCOSContainer(cube.LinkedStorage),
	})
	item, code := rt.stallOfferable(testDivision, c, 13)
	if code != 0 {
		t.Fatalf("stallOfferable code = %d, want 0", code)
	}
	if item.Data != 1 || item.LinkedStorage == nil || len(item.LinkedStorage.Rows) != 1 {
		t.Fatalf("stall offer lost linked storage: %+v", item)
	}
}

/*
================
admitTradeableCube

The fixture's item table must name the cube with canTrade so exchange
and stall admit it the way a later-client row does.
================
*/
func admitTradeableCube(rt *Runtime, cube inventory.Item) {
	rt.deps.ItemReferences().(staticItemSource)[cube.Codename] = &enterworld.ItemRef{
		RefObjID: cube.RefObjID, Codename: cube.Codename,
		TypeIDs:      [4]int64{3, 2, 3, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"canTrade": 1}),
	}
}
