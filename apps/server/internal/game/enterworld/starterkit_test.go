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
		{RefObjID: 24198, Codename: "ITEM_ETC_SPEED_UP_BASIC", StackCount: 1},
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
	if last.RefObjID != 24198 || last.Slot != 15 || last.StackCount != 1 {
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

/*
================
TestGrantStarterKitReplacesRetiredScrollInPlace
================
*/
func TestGrantStarterKitReplacesRetiredScrollInPlace(t *testing.T) {
	kit := []WireItem{{RefObjID: 24198, Codename: "ITEM_ETC_SPEED_UP_BASIC", TypeFlags: 7, StackCount: 1}}
	character := &Character{Name: "Retired", MissionInventory: []InventoryRow{
		{Slot: 13, RefObjID: 62, Codename: "ITEM_ETC_ARROW", StackCount: 250},
		{Slot: 20, RefObjID: 9264, Codename: "ITEM_MALL_MOVE_SPEED_UP_100", StackCount: 1},
	}}
	if granted := GrantStarterKit(character, kit); granted != 1 {
		t.Fatalf("granted %d items, want the one replacement", granted)
	}
	if len(character.MissionInventory) != 2 {
		t.Fatalf("inventory has %d rows, want the retired row replaced, not a new one", len(character.MissionInventory))
	}
	row := character.MissionInventory[1]
	if row.Slot != 20 || row.RefObjID != 24198 || row.Codename != "ITEM_ETC_SPEED_UP_BASIC" || row.TypeFlags != 7 || row.StackCount != 1 {
		t.Fatalf("replaced row = %+v, want the Beginner's movement scroll in slot 20", row)
	}
	if StarterKitMissing(character, kit) {
		t.Fatal("kit still reported missing after the swap")
	}
}
