/*
===========================================================================

inventory_capacity_test.go - active capacity gates raw-row item actions

A persisted row beyond the presented capacity cannot authorize consumption
or a lottery roll. Pending expansion does not make that row usable yet.

===========================================================================
*/

package action

import (
	"reflect"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestItemUseRequiresPresentedInventoryCapacity
================
*/
func TestItemUseRequiresPresentedInventoryCapacity(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "presented"}[expanded], func(t *testing.T) {
			character := testCharacter()
			character.InventoryExpansion = 10
			if expanded {
				character.PresentInventoryExpansion()
			}
			hp := int64(40)
			character.CurrentHP = &hp
			character.MissionInventory = []enterworld.InventoryRow{{
				Slot: 45, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01",
				TypeFlags: wire.PackTypeFlags(3, 3, 1, 1), VarianceBits: "0", StackCount: 2,
			}}
			rt, _ := newTestRuntime(character, testItems())
			body := []byte{45, 0xec, 0x08}
			if !expanded {
				assertItemUseRefusedUnchanged(t, rt, character, body, wire.ErrCodeInvalidRequest)
				return
			}
			result := rt.HandleItemUse(testDivision, character, body)
			if len(result.Frames) == 0 || !reflect.DeepEqual(result.Frames[0].Payload, wire.EncodeItemUseSuccess(45, 1, 0x08ec)) {
				t.Fatalf("presented slot was refused: %+v", result)
			}
			if character.MissionInventory[0].StackCount != 1 || *character.CurrentHP != 65 {
				t.Fatal("presented slot did not consume and recover HP")
			}
		})
	}
}

/*
================
TestGachaRequiresPresentedInventoryCapacity
================
*/
func TestGachaRequiresPresentedInventoryCapacity(t *testing.T) {
	for _, expanded := range []bool{false, true} {
		t.Run(map[bool]string{false: "pending", true: "presented"}[expanded], func(t *testing.T) {
			rt, character, body := gachaFixture(t)
			character.InventoryExpansion = 10
			if expanded {
				character.PresentInventoryExpansion()
			}
			character.MissionInventory[0].Slot = 45
			body[8] = 45
			draws := 0
			rt.GachaRoll = func() (uint32, error) {
				draws++
				return 0, nil
			}
			before := character.Snapshot()
			frames, reason := rt.HandleGachaRoll(testDivision, character, body)
			if !expanded {
				if reason == "" || len(frames) != 0 || draws != 0 || !reflect.DeepEqual(character.Snapshot(), before) {
					t.Fatal("unpresented slot consumed a lottery roll or changed the character")
				}
				return
			}
			if reason != "" || draws != 1 || character.MissionInventory[0].RefObjID != rt.GachaCatalog.WinCard.RefObjID {
				t.Fatalf("presented slot did not produce the winning card: %s %+v", reason, frames)
			}
			assertOpcodes(t, frames, 0x3645, 0xb053)
		})
	}
}
