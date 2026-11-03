/*
===========================================================================

costransfer_quest_test.go - the bag-to-COS quest-item refusal

Pins the transfer refusal in applyCosTransfer: a quest item (TypeID
3/3/9) never enters a COS bag, alchemy rows included - the transfer
judges the TypeID class alone, unlike the pickup share policy whose
alchemy exemption stays its own. Ordinary items, and the COS-to-player
direction, keep their existing behaviour.

===========================================================================
*/
package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

// questTransferCodename is a non-alchemy quest-band item (TypeIDs
// 3/3/9/1); alchemyTransferCodename carries the same band under the
// ITEM_QNO_RM_ARCHEMY prefix the classification excludes.
const questTransferCodename = "ITEM_QNO_CH_TEST_01"
const alchemyTransferCodename = "ITEM_QNO_RM_ARCHEMY_TEST_01"

/*
================
questTransferFixture

A test character whose single bag row (slot 20) is the named item under
the given type flags, with a summoned COS ready to trade container
slots.
================
*/
func questTransferFixture(t *testing.T, codename string, typeFlags uint16) (*enterworld.Character, *Runtime, uint32) {
	t.Helper()
	items := testItems()
	items[codename] = &enterworld.ItemRef{RefObjID: 4242, Codename: codename, TypeIDs: [4]int64{3, 3, 9, 1}}
	c := testCharacter()
	c.MissionInventory[0].RefObjID = 4242
	c.MissionInventory[0].Codename = codename
	c.MissionInventory[0].TypeFlags = typeFlags
	rt, _ := newTestRuntime(c, testCosSource(items))
	gid, ok := enterworld.CosObjectIDForCharacter(c)
	if !ok {
		t.Fatal("no COS gid for the test character")
	}
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 4}}
	return c, rt, gid
}

/*
================
TestPlayerToCosQuestItemRefused

A quest item dragged into the COS bag is refused with the
cannot-move-to-summon-item code (0xB1) and neither inventory changes.
================
*/
func TestPlayerToCosQuestItemRefused(t *testing.T) {
	c, rt, gid := questTransferFixture(t, questTransferCodename, wire.PackTypeFlags(3, 3, 9, 1))
	before := c.Snapshot()
	payload, err := (wire.ItemMoveRequest{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 0}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	got := rt.HandleItemMove(testDivision, c, payload)
	if len(got.Frames) != 1 || got.Frames[0].Opcode != wire.OpItemMoveResponse ||
		!reflect.DeepEqual(got.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeCannotEnterCosBag)) {
		t.Fatalf("quest item entered the COS bag or answered wrong: %+v", got)
	}
	if code := got.Frames[0].Payload[1]; code != 0xB1 {
		t.Fatalf("the refusal answered code %#x, want 0xB1", code)
	}
	if !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("the refusal mutated a container")
	}
}

/*
================
TestPlayerToCosAlchemyItemRefused

A quest-band row under the ITEM_QNO_RM_ARCHEMY prefix is refused too:
the transfer judges the TypeID class alone (3/3/9), with no codename
exemption. The alchemy exemption belongs to the pickup share policy
(isPersonalQuestPickupItem) and does not apply here.
================
*/
func TestPlayerToCosAlchemyItemRefused(t *testing.T) {
	c, rt, gid := questTransferFixture(t, alchemyTransferCodename, wire.PackTypeFlags(3, 3, 9, 1))
	before := c.Snapshot()
	payload, err := (wire.ItemMoveRequest{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 0}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	got := rt.HandleItemMove(testDivision, c, payload)
	if len(got.Frames) != 1 || got.Frames[0].Opcode != wire.OpItemMoveResponse ||
		!reflect.DeepEqual(got.Frames[0].Payload, wire.EncodeItemMoveError(wire.ErrCodeCannotEnterCosBag)) {
		t.Fatalf("alchemy item entered the COS bag or answered wrong: %+v", got)
	}
	if !reflect.DeepEqual(before, c.Snapshot()) {
		t.Fatal("the refusal mutated a container")
	}
}

/*
================
TestPlayerToCosOrdinaryItemStillTransfers

An ordinary bag item still transfers into the COS bag.
================
*/
func TestPlayerToCosOrdinaryItemStillTransfers(t *testing.T) {
	c := testCharacter()
	rt, _ := newTestRuntime(c, testCosSource(testItems()))
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 3914, Codename: "COS_T_DHORSE3", CurrentHP: 100, Summoned: true, Container: &domain.COSContainer{Capacity: 4}}
	payload, err := (wire.ItemMoveRequest{MovementType: wire.MoveTypePlayerToCos, CosGID: gid, SourceSlot: 20, DestSlot: 0}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	got := rt.HandleItemMove(testDivision, c, payload)
	if len(got.Frames) < 1 || !reflect.DeepEqual(got.Frames[0].Payload, append([]byte{1}, payload...)) {
		t.Fatalf("ordinary transfer failed: %+v", got)
	}
	if len(c.ActiveCOS.Container.Rows) != 1 || len(c.MissionInventory) != 0 {
		t.Fatalf("ordinary item landed wrong: cos=%d bag=%d", len(c.ActiveCOS.Container.Rows), len(c.MissionInventory))
	}
}

/*
================
TestCosToPlayerQuestItemUnchanged

The refusal is one-directional: a quest-band item already sitting in
the COS bag still moves back to the player's bag.
================
*/
func TestCosToPlayerQuestItemUnchanged(t *testing.T) {
	c, rt, gid := questTransferFixture(t, questTransferCodename, wire.PackTypeFlags(3, 3, 9, 1))
	row := c.MissionInventory[0]
	row.Slot = 0
	c.ActiveCOS.Container.Rows = []domain.InventoryRow{row}
	c.MissionInventory = nil
	payload, err := (wire.ItemMoveRequest{MovementType: wire.MoveTypeCosToPlayer, CosGID: gid, SourceSlot: 0, DestSlot: 13}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	got := rt.HandleItemMove(testDivision, c, payload)
	if len(got.Frames) < 1 || !reflect.DeepEqual(got.Frames[0].Payload, append([]byte{1}, payload...)) {
		t.Fatalf("COS-to-player quest move failed: %+v", got)
	}
	if len(c.ActiveCOS.Container.Rows) != 0 || len(c.MissionInventory) != 1 || c.MissionInventory[0].Codename != questTransferCodename {
		t.Fatalf("quest item landed wrong: cos=%d bag=%+v", len(c.ActiveCOS.Container.Rows), c.MissionInventory)
	}
}
