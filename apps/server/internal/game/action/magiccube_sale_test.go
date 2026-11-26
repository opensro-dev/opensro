/*
===========================================================================

magiccube_sale_test.go - merchant refusal of a non-empty Magic Cube

Port-only (#3906): UIIT_STT_MAGIC_CUBE_STORE_SELL_ERROR (01:8E). An empty
cube sells like any other mall item the reference admits.

===========================================================================
*/
package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestMagicCubeSaleRefusesWhileNonEmpty
================
*/
func TestMagicCubeSaleRefusesWhileNonEmpty(t *testing.T) {
	cubeFlags := wire.PackTypeFlags(3, 2, 3, 1)
	for _, tc := range []struct {
		name   string
		data   uint32
		refuse bool
	}{
		{"empty", 0, false},
		{"holding", 3, true},
	} {
		rt, c := merchantFixture(t)
		rt.MagicCube = true
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{
			Slot: 13, RefObjID: 35568, Codename: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
			TypeFlags: cubeFlags, StackCount: 1, Data: tc.data,
			LinkedStorage: &domain.COSContainer{Capacity: 10},
		})
		// A cube has canSell=1 in the later row; the fixture's item source
		// must admit the sale so only StoredItemCount can refuse.
		before := goldOf(c)
		r := trade(t, rt, c, wire.ItemMoveRequest{
			MovementType: wire.MoveTypeShopSell, NpcGID: 17, SourceSlot: 13, Quantity: 1,
		})
		if tc.refuse {
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeMagicCubeStoreSell}) {
				t.Fatalf("%s: sale = %+v, want refusal 0x8E", tc.name, r.Frames)
			}
			if goldOf(c) != before {
				t.Fatalf("%s: refused sale moved gold", tc.name)
			}
			continue
		}
		// Empty cube: either sells or is refused for another reason (no
		// mall price in the fixture). Either way it must not be 0x8E.
		if len(r.Frames) == 1 && len(r.Frames[0].Payload) == 2 &&
			r.Frames[0].Payload[0] == 2 && r.Frames[0].Payload[1] == wire.ErrCodeMagicCubeStoreSell {
			t.Fatalf("%s: empty cube wrongly refused as non-empty", tc.name)
		}
	}
}
