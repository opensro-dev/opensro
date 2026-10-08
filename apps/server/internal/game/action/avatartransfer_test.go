package action

import (
	"encoding/binary"
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"reflect"
	"testing"
)

func TestAvatarTransferRoundtripPreservesBodyAndVisualOrder(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, RefObjID: 90001, Codename: "AVATAR_HAT", TypeFlags: wire.PackTypeFlags(3, 1, 13, 1), Plus: 2, VarianceBits: "123", Durability: 30, StackCount: 1}}
	original := c.MissionInventory[0]
	original.Slot = 13
	items := staticItemSource{"AVATAR_HAT": {RefObjID: 90001, Codename: "AVATAR_HAT", TypeIDs: [4]int64{3, 1, 13, 1}, Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}}}
	rt, _ := newTestRuntime(c, items)
	equip := rt.HandleItemMove(testDivision, c, []byte{0x24, 20, 0})
	if len(equip.Frames) != 2 || equip.Frames[0].Opcode != wire.OpItemMoveResponse || equip.Frames[1].Opcode != 0x3314 {
		t.Fatalf("equip: %+v", equip)
	}
	// Viewers get the avatar's reference first (#340), then the owner's visual.
	if len(equip.Broadcast) != 2 || equip.Broadcast[0].Opcode != opCommerceItemReferences || !reflect.DeepEqual(equip.Broadcast[1:], equip.Frames[1:]) {
		t.Fatalf("viewers: %+v", equip.Broadcast)
	}
	if len(c.MissionInventory) != 0 || c.AvatarInventory == nil || len(c.AvatarInventory.Rows) != 1 {
		t.Fatal("not committed")
	}
	unequip := rt.HandleItemMove(testDivision, c, []byte{0x23, 0, 20})
	if len(unequip.Frames) != 2 || unequip.Frames[1].Opcode != 0x377c {
		t.Fatalf("unequip: %+v", unequip)
	}
	if !reflect.DeepEqual(c.MissionInventory, []enterworld.InventoryRow{original}) || len(c.AvatarInventory.Rows) != 0 {
		t.Fatalf("body changed: %+v", c.MissionInventory)
	}
}

func TestAvatarTransferRejectsOccupiedAndEquipmentDestinationsAtomically(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, RefObjID: 90001, Codename: "AVATAR_HAT", TypeFlags: wire.PackTypeFlags(3, 1, 13, 1), VarianceBits: "0", StackCount: 1}, {Slot: 21, RefObjID: 90001, Codename: "AVATAR_HAT", TypeFlags: wire.PackTypeFlags(3, 1, 13, 1), VarianceBits: "0", StackCount: 1}}
	rt, _ := newTestRuntime(c, staticItemSource{"AVATAR_HAT": {RefObjID: 90001, TypeIDs: [4]int64{3, 1, 13, 1}, Country: 3, RequiredSex: 2, ReqQuadTypes: [4]int64{-1, -1, -1, -1}}})
	rt.HandleItemMove(testDivision, c, []byte{0x24, 20, 0})
	for _, payload := range [][]byte{{0x24, 21, 0}, {0x24, 21, 1}, {0x23, 0, 6}, {0x23, 0, 45}, {0x24, 21, 4}, {0x23, 0}, {0x23, 0, 20, 1}} {
		result := rt.HandleItemMove(testDivision, c, payload)
		if len(result.Frames) != 1 || result.Frames[0].Payload[0] != 2 {
			t.Fatalf("accepted %x: %+v", payload, result)
		}
		if len(c.MissionInventory) != 1 || c.MissionInventory[0].Slot != 21 || len(c.AvatarInventory.Rows) != 1 {
			t.Fatalf("mutated for %x", payload)
		}
	}
}

func TestAvatarDressRemovalMovesAttachmentAtomicallyAndUsesFirstFreeSlots(t *testing.T) {
	for _, freeCount := range []int{0, 1, 2} {
		t.Run(string(rune('0'+freeCount)), func(t *testing.T) {
			c := testCharacter()
			c.MissionInventory = nil
			for slot := 13; slot < 45-freeCount; slot++ {
				c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: int64(slot), RefObjID: 1, TypeFlags: 0x6c, StackCount: 1})
			}
			dress := enterworld.InventoryRow{Slot: 3, RefObjID: 90002, Codename: "DRESS", TypeFlags: 0x16ac, StackCount: 1, VarianceBits: "123", Plus: 2, Durability: 17}
			attachment := enterworld.InventoryRow{Slot: 0, RefObjID: 90003, Codename: "ATTACHMENT", TypeFlags: 0x1eac, StackCount: 1, VarianceBits: "456", Plus: 3, Durability: 23}
			c.AvatarInventory = &domain.AvatarInventory{Capacity: 4, Rows: []enterworld.InventoryRow{dress, attachment}}
			before := append([]enterworld.InventoryRow(nil), c.MissionInventory...)
			rt, _ := newTestRuntime(c, nil)
			result := rt.HandleItemMove(testDivision, c, []byte{0x23, 3, 20})
			if freeCount < 2 {
				if len(result.Frames) != 1 || !reflect.DeepEqual(result.Frames[0].Payload, []byte{2, 7}) || !reflect.DeepEqual(before, c.MissionInventory) || len(c.AvatarInventory.Rows) != 2 {
					t.Fatalf("partial removal: %+v", result)
				}
				return
			}
			if len(result.Frames) != 3 || !reflect.DeepEqual(result.Frames[0].Payload, []byte{1, 0x23, 3, 43, 1, 0, 1, 0x23, 0, 44, 1, 0}) {
				t.Fatalf("wire: %+v", result)
			}
			if binary.LittleEndian.Uint32(result.Frames[1].Payload[5:]) != 90003 || binary.LittleEndian.Uint32(result.Frames[2].Payload[5:]) != 90002 {
				t.Fatal("attachment must disappear before dress")
			}
			if !reflect.DeepEqual(result.Broadcast, result.Frames[1:]) {
				t.Fatalf("viewers: %+v", result.Broadcast)
			}
			dress.Slot = 43
			attachment.Slot = 44
			if len(c.AvatarInventory.Rows) != 0 || !reflect.DeepEqual(c.MissionInventory[len(before):], []enterworld.InventoryRow{dress, attachment}) {
				t.Fatalf("body changed: %+v", c.MissionInventory)
			}
		})
	}
}

func TestStackSplitAndMergeRoundtripThroughNativeMoveFrames(t *testing.T) {
	c := testCharacter()
	c.MissionInventory = []enterworld.InventoryRow{{Slot: 20, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), StackCount: 10, VarianceBits: "0"}}
	rt, _ := newTestRuntime(c, testItems())
	split := rt.HandleItemMove(testDivision, c, []byte{0, 20, 13, 3, 0})
	if len(split.Frames) != 1 || !reflect.DeepEqual(split.Frames[0].Payload, []byte{1, 0, 20, 13, 3, 0, 0}) || len(c.MissionInventory) != 2 || c.MissionInventory[0].StackCount != 7 || c.MissionInventory[1].StackCount != 3 {
		t.Fatalf("split: %+v / %+v", split, c.MissionInventory)
	}
	merge := rt.HandleItemMove(testDivision, c, []byte{0, 13, 20, 3, 0})
	if len(merge.Frames) != 1 || merge.Frames[0].Payload[0] != 1 || len(c.MissionInventory) != 1 || c.MissionInventory[0].Slot != 20 || c.MissionInventory[0].StackCount != 10 {
		t.Fatalf("merge: %+v / %+v", merge, c.MissionInventory)
	}
}
