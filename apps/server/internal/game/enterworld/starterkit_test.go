/*
===========================================================================

starterkit_test.go - the beta starter kit backfills free bag slots

===========================================================================
*/
package enterworld

import (
	"testing"

	"opensro.online/server/internal/game/item/inventory"
)

/*
================
TestGrantStarterKitBackfillsMissingItemsOnly
================
*/
func TestGrantStarterKitBackfillsMissingItemsOnly(t *testing.T) {
	kit := []WireItem{
		{RefObjID: 61, Codename: "ITEM_ETC_SCROLL_RETURN_01", StackCount: 1},
		{RefObjID: 9264, Codename: "ITEM_MALL_MOVE_SPEED_UP_100", StackCount: 1},
	}
	character := &Character{Name: "Kit", MissionInventory: []InventoryRow{
		{Slot: 6, RefObjID: 3655, StackCount: 1},
		{Slot: 13, RefObjID: 62, StackCount: 250},
		{Slot: 14, RefObjID: 61, StackCount: 3},
	}}
	if !StarterKitMissing(character, kit) {
		t.Fatal("kit reported complete while the speed scroll is missing")
	}
	if granted := GrantStarterKit(character, kit); granted != 1 {
		t.Fatalf("granted %d items, want only the missing speed scroll", granted)
	}
	last := character.MissionInventory[len(character.MissionInventory)-1]
	if last.RefObjID != 9264 || last.Slot != 15 || last.StackCount != 1 {
		t.Fatalf("speed scroll row = %+v, want slot 15 (first free bag slot)", last)
	}
	if StarterKitMissing(character, kit) || GrantStarterKit(character, kit) != 0 {
		t.Fatal("a second entry granted the kit again")
	}
}

/*
================
TestGrantStarterKitFullBagGrantsNothing
================
*/
func TestGrantStarterKitFullBagGrantsNothing(t *testing.T) {
	character := &Character{Name: "Full"}
	for slot := int64(inventory.EquipmentSlotEnd); slot < int64(inventory.BagSlotEnd); slot++ {
		character.MissionInventory = append(character.MissionInventory, InventoryRow{Slot: slot, RefObjID: 1000, StackCount: 1})
	}
	kit := []WireItem{{RefObjID: 61, Codename: "ITEM_ETC_SCROLL_RETURN_01", StackCount: 1}}
	before := len(character.MissionInventory)
	if GrantStarterKit(character, kit) != 0 || len(character.MissionInventory) != before {
		t.Fatal("a full bag received a kit item")
	}
}
