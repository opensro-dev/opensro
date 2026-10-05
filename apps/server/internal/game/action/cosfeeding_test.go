/*
===========================================================================

cosfeeding_test.go - food admission, atomic debit, and native satiety boundaries

===========================================================================
*/
package action

import (
	"encoding/binary"
	"reflect"
	"testing"

	"opensro.online/server/internal/game/companion"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestShippedPetFoodUsesAuthoredPercentAndNativeBoundary
================
*/
func TestShippedPetFoodUsesAuthoredPercentAndNativeBoundary(t *testing.T) {
	items := shippedItems(t)
	food, found := items.ItemRefByCodename("ITEM_COS_P_HGP_POTION_01")
	if !found || food.TypeIDs != [4]int64{3, 3, 1, 9} {
		t.Fatal("missing shipped pet food")
	}
	percent := food.NativeFields.Get("itemParam1_29c")
	if percent <= 0 || percent >= 100 {
		t.Fatal("invalid shipped pet food percent", percent)
	}
	for _, initial := range []uint16{0, 100, 2999, 3000, 9899, 9900, 10000} {
		c := testCharacter()
		rt, _ := newTestRuntime(c, items)
		equipShippedPet(t, rt, c, items, "COS_P_WOLF_001")
		c.ActiveCOS.Satiety = initial
		c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: food.RefObjID, Codename: food.Codename, TypeFlags: food.TypeFlags(), StackCount: 2}}
		request := petUse(c, food, c.ActiveCOS.GID, -1)
		if initial >= cosFeedingRefusalThreshold {
			assertItemUseRefusedUnchanged(t, rt, c, request, cosFeedingFullError)
			continue
		}
		ownerHP, petHP := enterworld.CurrentHP(c), c.ActiveCOS.CurrentHP
		result := rt.HandleItemUse(testDivision, c, request)
		assertOpcodes(t, result.Frames, wire.OpItemUseResponse, wire.OpItemUseVisual, cosPetUpdateOpcode)
		want := uint16(min(companion.MaximumSatiety, int(initial)+int(percent)*100))
		if result.Frames[0].Payload[0] != 1 || c.ActiveCOS.Satiety != want || c.MissionInventory[0].StackCount != 1 {
			t.Fatalf("initial %d: pet %+v, frames %+v", initial, c.ActiveCOS, result.Frames)
		}
		payload := result.Frames[2].Payload // after the success and its visual
		if len(payload) != 7 || binary.LittleEndian.Uint32(payload) != c.ActiveCOS.GID || payload[4] != 4 || binary.LittleEndian.Uint16(payload[5:]) != want {
			t.Fatalf("satiety wire = %x", payload)
		}
		if enterworld.CurrentHP(c) != ownerHP || c.ActiveCOS.CurrentHP != petHP || c.PetPotionCooldowns != [3]int64{} ||
			// Only the item's own visual is public (510980 sends 0x305C for every use).
			len(result.Broadcast) != 2 || result.Broadcast[1].Opcode != wire.OpItemUseVisual {
			t.Fatal("feeding changed vitals, reuse timers or public state")
		}
	}
}

/*
================
TestPetFoodRefusesForeignDeadDormantAndWrongFamily
================
*/
func TestPetFoodRefusesForeignDeadDormantAndWrongFamily(t *testing.T) {
	items := shippedItems(t)
	food, _ := items.ItemRefByCodename("ITEM_COS_P_HGP_POTION_01")
	for _, variant := range []string{"foreign", "dead", "dormant", "pickup", "malformed"} {
		t.Run(variant, func(t *testing.T) {
			c := testCharacter()
			rt, _ := newTestRuntime(c, items)
			code := "COS_P_WOLF_001"
			if variant == "pickup" {
				code = "COS_P_RABBIT"
			}
			equipShippedPet(t, rt, c, items, code)
			c.MissionInventory = []enterworld.InventoryRow{{Slot: 21, RefObjID: food.RefObjID, Codename: food.Codename, TypeFlags: food.TypeFlags(), StackCount: 1}}
			gid := c.ActiveCOS.GID
			if variant == "foreign" {
				gid++
			}
			if variant == "dead" {
				c.ActiveCOS.CurrentHP = 0
			}
			if variant == "dormant" {
				c.ActiveCOS.Summoned = false
			}
			request := petUse(c, food, gid, -1)
			if variant == "malformed" {
				request = append(request, 0)
			}
			before := c.Snapshot()
			assertItemUseRefusedUnchanged(t, rt, c, request, wire.ErrCodeCosRefused)
			if !reflect.DeepEqual(before, c.Snapshot()) {
				t.Fatal("refusal mutated state")
			}
		})
	}
}
