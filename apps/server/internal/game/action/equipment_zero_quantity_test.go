package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

// TestNativeBagToEquipmentZeroQuantity pins the exact client/server boundary
// emitted by CIFEquipment vFunc_27. It deliberately enters with raw bytes:
// an Encode helper must not normalize the native zero quantity to one.
func TestNativeBagToEquipmentZeroQuantity(t *testing.T) {
	equipWord := wire.PackTypeFlags(3, 1, 6, 2)
	character := testCharacter()
	character.MissionInventory = []enterworld.InventoryRow{
		{
			Slot: 20, RefObjID: 11459, Codename: "ITEM_CH_SWORD_01_A_RARE",
			TypeFlags: equipWord, Plus: 5, VarianceBits: "0", Durability: 96,
			StackCount: 1,
		},
	}
	rt, _ := newTestRuntime(character, testItems())

	// sub_594980 -> sub_699250 -> sub_697e80 emits this exact body. The
	// movement-record ctor leaves quantity at zero on the bag->equipment row;
	// the nearby `push 1` arms CGInterface's pending-state timer.
	result := rt.HandleItemMove(testDivision, character, []byte{
		wire.MoveTypeInventory, 20, 6, 0, 0,
	})
	assertOpcodes(
		t,
		result.Frames,
		wire.OpItemMoveResponse,
		wire.OpEquipVisual,
		wire.OpBaseStats,
	)
	if _, ok := inventory.New(invItemsFromBag(character), domain.DefaultInventorySize).At(6); !ok {
		t.Fatal("native zero-quantity equipment request did not seat the weapon in slot 6")
	}
	move, err := wire.DecodeItemMoveResult(result.Frames[0].Payload, 0)
	if err != nil || move.Result != wire.ResultSuccess || move.Quantity != 0 {
		t.Fatalf(
			"native zero-quantity response = %+v (%v), want successful echoed quantity 0",
			move,
			err,
		)
	}
}
