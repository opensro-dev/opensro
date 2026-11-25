/*
===========================================================================

spiritpaths_test.go - the Devil's Spirit in the sell, drop and store paths

Port-only, not native (#4459): a Spirit (3/1/14) is bound by its imported
row - nasrun_import clears CanTrade, CanSell, CanBorrow and CanDrop, the
v1.150 honor-bound shape and the owner's pick on #3456 - so each path
refuses it for that bound rule, never because the kind is unknown to it.
Each test carries the same Spirit with the cell set as a control: the
unbound row passes the same path. The NPC sale leg stands in
nasrun_shop_test.go (TestNoMerchantBuysASpirit).

===========================================================================
*/
package action

import (
	"bytes"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// spiritBagRow is the row a Mall-bought Spirit (ITEM_MALL_AVATAR_W_NASRUN_BLUE,
// RefObjID 51681) occupies in the bag.
func spiritBagRow(slot int64) enterworld.InventoryRow {
	return enterworld.InventoryRow{
		Slot: slot, RefObjID: 51681, Codename: "ITEM_MALL_AVATAR_W_NASRUN_BLUE",
		TypeFlags: wire.PackTypeFlags(3, 1, 14, 1), StackCount: 1, VarianceBits: "0",
	}
}

/*
================
TestSpiritDropAnswersTheBoundRefusal

A bound Spirit answers the indisposable refusal (01:38) with the bag
untouched; the same Spirit on a droppable row drops, so the drop path
refuses the bound cell, not the 3/1/14 kind.
================
*/
func TestSpiritDropAnswersTheBoundRefusal(t *testing.T) {
	for _, canDrop := range []bool{false, true} {
		c := testCharacter()
		c.MissionInventory = []enterworld.InventoryRow{spiritBagRow(20)}
		items := staticItemSource{
			"ITEM_MALL_AVATAR_W_NASRUN_BLUE": {
				RefObjID: 51681, Codename: "ITEM_MALL_AVATAR_W_NASRUN_BLUE",
				TypeIDs: [4]int64{3, 1, 14, 1}, CanDrop: canDrop,
			},
		}
		rt, _ := newTestRuntime(c, items)

		before := c.Snapshot()
		r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20}))
		if !canDrop {
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotBeDropped}) ||
				!reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
				t.Fatalf("bound Spirit drop: %+v", r)
			}
			continue
		}
		if bagCount(c, "ITEM_MALL_AVATAR_W_NASRUN_BLUE") != 0 {
			t.Fatalf("droppable Spirit stayed in the bag: %+v", r)
		}
	}
}

/*
================
TestSpiritStorageDepositAnswersTheBoundRefusal

A bound Spirit (CanBorrow clear, as the import authors it) answers the
invalid-storage-target refusal (0x43) and nothing enters the room; the
same Spirit with the storable bit set deposits, so the warehouse
refuses the bound cell, not the 3/1/14 kind.
================
*/
func TestSpiritStorageDepositAnswersTheBoundRefusal(t *testing.T) {
	for _, canBorrow := range []float64{0, 255} {
		rt, c, authority := storageFixture(t)
		spirit := &enterworld.ItemRef{
			RefObjID: 51681, Codename: "ITEM_MALL_AVATAR_W_NASRUN_BLUE", TypeIDs: [4]int64{3, 1, 14, 1},
		}
		spirit.NativeFields = spirit.NativeFields.With("canBorrow", canBorrow).With("keepingFee", 0)
		rt.deps.ItemReferences().(staticItemSource)["ITEM_MALL_AVATAR_W_NASRUN_BLUE"] = spirit
		c.MissionInventory = append(c.MissionInventory, spiritBagRow(15))

		r := trade(t, rt, c, wire.ItemMoveRequest{MovementType: wire.MoveTypeStorageDeposit, SourceSlot: 15, DestSlot: 0, NpcGID: 17})
		if canBorrow == 0 {
			if r.Frames[0].Payload[0] != 2 || r.Frames[0].Payload[1] != errCodeInvalidStorageTarget || len(authority.room.Rows) != 0 {
				t.Fatalf("bound Spirit deposit = %+v, room %+v", r, authority.room)
			}
			continue
		}
		if r.Frames[0].Payload[0] != 1 || len(authority.room.Rows) != 1 {
			t.Fatalf("storable Spirit deposit = %+v, room %+v", r, authority.room)
		}
	}
}
