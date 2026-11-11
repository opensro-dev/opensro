package action

import (
	"encoding/json"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"testing"
)

func TestPlayerCOSTransferRoundTrip(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 140}}
	original := c.Snapshot().MissionInventory[0]
	for _, q := range []wire.ItemMoveRequest{{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 139}, {MovementType: wire.MoveTypeCosToPlayer, CosGID: gid, SourceSlot: 139, DestSlot: 13}} {
		payload, e := q.Encode()
		if e != nil {
			t.Fatal(e)
		}
		got := rt.HandleItemMove(testDivision, c, payload)
		if len(got.Frames) < 1 || !reflect.DeepEqual(got.Frames[0].Payload, append([]byte{1}, payload...)) {
			t.Fatalf("transfer failed %+v", got)
		}
		data, e := json.Marshal(c)
		if e != nil {
			t.Fatal(e)
		}
		var restored enterworld.Character
		if e = json.Unmarshal(data, &restored); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(c.MissionInventory, restored.MissionInventory) || !reflect.DeepEqual(c.ActiveCOS.Container, restored.ActiveCOS.Container) {
			t.Fatal("persistence lost container metadata")
		}
	}
	original.Slot = 13
	if len(c.ActiveCOS.Container.Rows) != 0 || len(c.MissionInventory) != 1 || !reflect.DeepEqual(c.MissionInventory[0], original) {
		t.Fatalf("round trip changed item %+v", c.MissionInventory)
	}
}
func TestPlayerCOSTransferRejectsWithoutMutation(t *testing.T) {
	for _, mode := range []string{"foreign", "dead", "desummoned", "equipment", "range", "trailing", "truncated"} {
		t.Run(mode, func(t *testing.T) {
			c := testCharacter()
			rt, _ := newTestRuntime(c, testCosSource(testItems()))
			gid, _ := enterworld.CosObjectIDForCharacter(c)
			c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 4}}
			q := wire.ItemMoveRequest{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 0}
			switch mode {
			case "foreign":
				q.CosGID++
			case "dead":
				c.ActiveCOS.CurrentHP = 0
			case "desummoned":
				c.ActiveCOS.Summoned = false
			case "equipment":
				q.SourceSlot = 0
			case "range":
				q.DestSlot = 4
			}
			p, e := q.Encode()
			if e != nil {
				t.Fatal(e)
			}
			if mode == "trailing" {
				p = append(p, 0)
			}
			if mode == "truncated" {
				p = p[:6]
			}
			before := c.Snapshot()
			r := rt.HandleItemMove(testDivision, c, p)
			if len(r.Frames) != 1 || r.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatalf("rejection changed authority %+v", r)
			}
		})
	}
}

/*
================
TestCosToPlayerRefusesTradeGoods

#3144: a trade good dragged out of a companion's cargo into the
personal bag could never leave it again (the specialty shop buys
from the transport, and the row's authored CanDrop bit bars the
ground), so the transfer refuses it and neither side moves. The
event food under the same TID3 is an ordinary bag item and still
crosses.
================
*/
func TestCosToPlayerRefusesTradeGoods(t *testing.T) {
	c := testCharacter()
	items := testItems()
	items["ITEM_ETC_TRADE_WC_04"] = &enterworld.ItemRef{RefObjID: 5005, Codename: "ITEM_ETC_TRADE_WC_04", TypeIDs: [4]int64{3, 3, 8, 1}}
	items["ITEM_ETC_E050314_CANDY_01"] = &enterworld.ItemRef{RefObjID: 5006, Codename: "ITEM_ETC_E050314_CANDY_01", TypeIDs: [4]int64{3, 3, 8, 0}}
	rt, _ := newTestRuntime(c, testCosSource(items))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 2, Rows: []domain.InventoryRow{
		{Slot: 0, RefObjID: 5005, Codename: "ITEM_ETC_TRADE_WC_04", TypeFlags: wire.PackTypeFlags(3, 3, 8, 1), StackCount: 40},
		{Slot: 1, RefObjID: 5006, Codename: "ITEM_ETC_E050314_CANDY_01", TypeFlags: wire.PackTypeFlags(3, 3, 8, 0), StackCount: 1},
	}}}

	before := c.Snapshot()
	p, err := (wire.ItemMoveRequest{MovementType: wire.MoveTypeCosToPlayer, CosGID: gid, SourceSlot: 0, DestSlot: 13}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	r := rt.HandleItemMove(testDivision, c, p)
	if len(r.Frames) != 1 || r.Frames[0].Payload[0] != 2 || !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatalf("trade goods crossed into the bag or the refusal moved state: %+v", r)
	}

	p, err = (wire.ItemMoveRequest{MovementType: wire.MoveTypeCosToPlayer, CosGID: gid, SourceSlot: 1, DestSlot: 13}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	r = rt.HandleItemMove(testDivision, c, p)
	if len(r.Frames) < 1 || r.Frames[0].Payload[0] != 1 || bagCount(c, "ITEM_ETC_E050314_CANDY_01") != 1 {
		t.Fatalf("event food no longer crosses to the bag: %+v", r)
	}
}

func TestPlayerCOSOccupiedEquipmentSwapPreservesBothBodies(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.MissionInventory[0].Plus = 3
	row := c.MissionInventory[0]
	row.Slot = 0
	row.Plus = 7
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 1, Rows: []domain.InventoryRow{row}}}
	p, _ := (wire.ItemMoveRequest{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 0}).Encode()
	r := rt.HandleItemMove(testDivision, c, p)
	if r.Frames[0].Payload[0] != 1 || c.MissionInventory[0].Plus != 7 || c.ActiveCOS.Container.Rows[0].Plus != 3 {
		t.Fatal("occupied cross-container swap rejected or lost body", r)
	}
}
