/*
===========================================================================

magiccube_ops_test.go - deposit, withdraw and full refusal for Magic Cube

Port-only (#3906). Layout: push [src][dst][qty]; pop [RefObjID][src][dst][qty].

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/magiccube"
)

func magicCubeFixture(t *testing.T) (*Runtime, *enterworld.Character) {
	t.Helper()
	cubeFlags := wire.PackTypeFlags(3, 2, 3, 1)
	elixirFlags := wire.PackTypeFlags(3, 3, 10, 1)
	items := testItems()
	items["ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10"] = &enterworld.ItemRef{
		RefObjID: 35568, Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
		TypeIDs: [4]int64{3, 2, 3, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{
			magiccube.ParamCapacityKey: 10,
			magiccube.ParamFilterKey:   float64(magiccube.FilterWeapon),
			"maxStack":                 1,
			"canSell":                  1,
		}),
	}
	items["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A"] = &enterworld.ItemRef{
		RefObjID: 2151, Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A",
		TypeIDs: [4]int64{3, 3, 10, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50}),
		CanDrop:      true,
	}
	items["ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B"] = &enterworld.ItemRef{
		RefObjID: 2152, Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
		TypeIDs: [4]int64{3, 3, 10, 1},
		NativeFields: enterworld.NewNativeFields(map[string]float64{"maxStack": 50}),
		CanDrop:      true,
	}
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{
		{
			Slot: 13, RefObjID: 35568, Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
			TypeFlags: cubeFlags, StackCount: 1,
			LinkedStorage: &domain.COSContainer{Capacity: 10},
		},
		{
			Slot: 14, RefObjID: 2151, Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_A",
			TypeFlags: elixirFlags, StackCount: 5,
		},
	}
	rt, _ := newTestRuntime(c, items)
	rt.MagicCube = true
	return rt, c
}

/*
================
TestMagicCubePushAndPop
================
*/
func TestMagicCubePushAndPop(t *testing.T) {
	rt, c := magicCubeFixture(t)
	push := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypePushIntoMagicCube,
		SourceSlot:   14, DestSlot: 13, Quantity: 2,
	}))
	if len(push.Frames) != 1 || push.Frames[0].Payload[0] != wire.ResultSuccess {
		t.Fatalf("push = %+v", push.Frames)
	}
	wantPush := wire.EncodePushIntoMagicCubeResult(14, 13, 2)
	if !bytes.Equal(push.Frames[0].Payload, wantPush) {
		t.Fatalf("push payload = % X, want % X", push.Frames[0].Payload, wantPush)
	}
	cube, ok := bagOf(c).At(13)
	if !ok || cube.Data != 2 || cube.LinkedStorage == nil || len(cube.LinkedStorage.Rows) != 1 {
		t.Fatalf("cube after push: data=%d storage=%+v", cube.Data, cube.LinkedStorage)
	}
	elixir, ok := bagOf(c).At(14)
	if !ok || elixir.Quantity != 3 {
		t.Fatalf("elixir remaining = %v", elixir)
	}

	pop := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypePopFromMagicCube,
		RefObjID:     2151, SourceSlot: 13, DestSlot: 15, Quantity: 1,
	}))
	if len(pop.Frames) != 1 || !bytes.Equal(pop.Frames[0].Payload, wire.EncodePopFromMagicCubeResult(2151, 13, 15, 1)) {
		t.Fatalf("pop = %+v", pop.Frames)
	}
	cube, _ = bagOf(c).At(13)
	if cube.Data != 1 {
		t.Fatalf("cube data after pop = %d", cube.Data)
	}
	got, ok := bagOf(c).At(15)
	if !ok || got.RefObjID != 2151 || got.Quantity != 1 {
		t.Fatalf("withdrawn = %+v", got)
	}
}

/*
================
TestMagicCubePushFullRefuses
================
*/
func TestMagicCubePushFullRefuses(t *testing.T) {
	rt, c := magicCubeFixture(t)
	elixirFlags := wire.PackTypeFlags(3, 3, 10, 1)
	// All 10 Param9 slots hold WEAPON_B at stack cap, so WEAPON_A cannot
	// merge and has no free slot (GrantStack answers inventory-full → 0x8A).
	rows := make([]domain.InventoryRow, 10)
	for i := range rows {
		rows[i] = domain.InventoryRow{
			Slot: int64(i), RefObjID: 2152, Codename: "ITEM_ETC_ARCHEMY_REINFORCE_RECIPE_WEAPON_B",
			TypeFlags: elixirFlags, StackCount: 50, VarianceBits: "0",
		}
	}
	updated := false
	for i, row := range c.MissionInventory {
		if row.Slot == 13 {
			c.MissionInventory[i].LinkedStorage = &domain.COSContainer{Capacity: 10, Rows: rows}
			c.MissionInventory[i].Data = 500
			updated = true
		}
	}
	if !updated {
		t.Fatal("cube row missing")
	}
	// Confirm the bag snapshot the handler will see is full of B.
	cube, ok := bagOf(c).At(13)
	if !ok || cube.LinkedStorage == nil || len(cube.LinkedStorage.Rows) != 10 {
		t.Fatalf("precondition cube storage = %+v", cube.LinkedStorage)
	}
	r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypePushIntoMagicCube,
		SourceSlot:   14, DestSlot: 13, Quantity: 1,
	}))
	if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeMagicCubeFull}) {
		t.Fatalf("full push = %+v, want 0x8A", r.Frames)
	}
}

/*
================
TestMagicCubeOffRefuses
================
*/
func TestMagicCubeOffRefuses(t *testing.T) {
	rt, c := magicCubeFixture(t)
	rt.MagicCube = false
	r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypePushIntoMagicCube,
		SourceSlot:   14, DestSlot: 13, Quantity: 1,
	}))
	if len(r.Frames) != 1 || r.Frames[0].Payload[0] != wire.ResultError {
		t.Fatalf("off = %+v", r.Frames)
	}
}
