/*
===========================================================================

grounddrop_test.go - the native ground-drop bars at the 0x706D seam

A barred bag row answers the indisposable refusal (01:38) with the
bag untouched; every unbarred row drops exactly as before. The bars
are the COS-summoner family (monster capsules exempt) and a clear
authored CanDrop bit; a worn row keeps the equipment fault (01:6D),
which precedes the bars natively.

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

/*
================
groundDropTestItems
================
*/
func groundDropTestItems() staticItemSource {
	return staticItemSource{
		"ITEM_ETC_HP_POTION_01": {
			RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeIDs: [4]int64{3, 3, 1, 1},
			CanDrop: true,
		},
		"ITEM_QNO_RM_VILLAGECHIEF_1_01": {
			RefObjID: 5001, Codename: "ITEM_QNO_RM_VILLAGECHIEF_1_01", TypeIDs: [4]int64{3, 3, 9, 0},
		},
		"ITEM_COS_P_WOLF_SCROLL": {
			RefObjID: 5002, Codename: "ITEM_COS_P_WOLF_SCROLL", TypeIDs: [4]int64{3, 2, 1, 0},
			CanDrop: true,
		},
		"ITEM_ETC_TRANS_MONSTER": {
			RefObjID: 5003, Codename: "ITEM_ETC_TRANS_MONSTER", TypeIDs: [4]int64{3, 2, 2, 0},
			CanDrop: true,
		},
		"ITEM_ETC_CAPSULE_KEPT": {
			RefObjID: 5004, Codename: "ITEM_ETC_CAPSULE_KEPT", TypeIDs: [4]int64{3, 2, 2, 0},
		},
	}
}

/*
================
groundDropCharacter
================
*/
func groundDropCharacter(codename string, typeIDs [4]int64, slot int64) *enterworld.Character {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{
		Slot: slot, RefObjID: 5000, Codename: codename,
		TypeFlags: wire.PackTypeFlags(uint8(typeIDs[0]), uint8(typeIDs[1]), uint8(typeIDs[2]), uint8(typeIDs[3])), StackCount: 1,
	}}
	return c
}

/*
================
TestGroundDropNativeBars

Each barred row answers {2, 01:38} with the bag untouched; each
unbarred row drops. The summoner scroll carries CanDrop on purpose:
the family bar alone must refuse it. The kept capsule carries the
family exemption on purpose: the CanDrop bit alone must refuse it.
================
*/
func TestGroundDropNativeBars(t *testing.T) {
	items := groundDropTestItems()
	for _, row := range []struct {
		codename string
		barred   bool
	}{
		{"ITEM_ETC_HP_POTION_01", false},
		{"ITEM_QNO_RM_VILLAGECHIEF_1_01", true},
		{"ITEM_COS_P_WOLF_SCROLL", true},
		{"ITEM_ETC_TRANS_MONSTER", false},
		{"ITEM_ETC_CAPSULE_KEPT", true},
	} {
		ref := items[row.codename]
		c := groundDropCharacter(row.codename, ref.TypeIDs, 20)
		rt, _ := newTestRuntime(c, items)

		before := c.Snapshot()
		r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 20}))
		if row.barred {
			if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotBeDropped}) ||
				!reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
				t.Fatalf("%s drop: %+v", row.codename, r)
			}
			continue
		}
		echo := false
		for _, frame := range r.Frames {
			if len(frame.Payload) > 0 && frame.Payload[0] == 1 {
				echo = true
			}
		}
		if !echo || bagCount(c, row.codename) != 0 {
			t.Fatalf("%s drop: %+v", row.codename, r)
		}
	}
}

/*
================
TestGroundDropWornBarredItemKeepsEquipmentFault

A worn row never reaches the drop bars: even with CanDrop clear it
answers the equipment fault (01:6D), the native order.
================
*/
func TestGroundDropWornBarredItemKeepsEquipmentFault(t *testing.T) {
	items := groundDropTestItems()
	c := groundDropCharacter("ITEM_QNO_RM_VILLAGECHIEF_1_01", [4]int64{3, 3, 9, 0}, 6)
	rt, _ := newTestRuntime(c, items)

	before := c.Snapshot()
	r := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{MovementType: wire.MoveTypeGroundDrop, SourceSlot: 6}))
	if len(r.Frames) != 1 || !bytes.Equal(r.Frames[0].Payload, []byte{2, wire.ErrCodeCannotDropEquipped}) ||
		!reflect.DeepEqual(before.MissionInventory, c.MissionInventory) {
		t.Fatalf("worn drop: %+v", r)
	}
}
