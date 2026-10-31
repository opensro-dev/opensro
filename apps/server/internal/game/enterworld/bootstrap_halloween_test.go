/*
===========================================================================

bootstrap_halloween_test.go - the entry door's expired-box cleanup call

The filtering itself is the action lane's (action/halloween.go); here the
door only has to call the hook under its conditional write and re-read
the snapshot, so the projection never shows a retired box.

===========================================================================
*/
package enterworld

import "testing"

/*
================
TestEntryCleanupRetiresBoxesBeforeTheProjection
================
*/
func TestEntryCleanupRetiresBoxesBeforeTheProjection(t *testing.T) {
	character := chinaSpearman()
	character.MissionInventory = []InventoryRow{
		{Slot: 20, RefObjID: 3862, Codename: "ITEM_ETC_E050618_TREASUREBOX", StackCount: 2},
		{Slot: 21, RefObjID: 3630, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 3},
	}
	deps := testDeps(character)
	calls := 0
	deps.RemoveExpiredEventBoxes = func(c *Character) bool {
		calls++
		kept := c.MissionInventory[:0]
		for _, row := range c.MissionInventory {
			if row.Codename != "ITEM_ETC_E050618_TREASUREBOX" {
				kept = append(kept, row)
			}
		}
		if len(kept) == len(c.MissionInventory) {
			return false
		}
		c.MissionInventory = kept
		return true
	}
	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	if result.NativeResult != nativeResultSuccess {
		t.Fatalf("entry failed: %+v", result)
	}
	if calls == 0 {
		t.Fatal("the entry door never called the cleanup hook")
	}
	for _, row := range character.MissionInventory {
		if row.Codename == "ITEM_ETC_E050618_TREASUREBOX" {
			t.Fatal("a box survived entry", character.MissionInventory)
		}
	}
	if len(character.MissionInventory) != 1 {
		t.Fatal("cleanup removed the wrong rows", character.MissionInventory)
	}
}

/*
================
TestEntryWithoutCleanupHookIsUnchanged
================
*/
func TestEntryWithoutCleanupHookIsUnchanged(t *testing.T) {
	character := chinaSpearman()
	character.MissionInventory = []InventoryRow{
		{Slot: 20, RefObjID: 3862, Codename: "ITEM_ETC_E050618_TREASUREBOX", StackCount: 2},
	}
	deps := testDeps(character)
	result := Build(deps, BootstrapRequest{CharacterName: "asd2"})
	if result.NativeResult != nativeResultSuccess {
		t.Fatalf("entry failed: %+v", result)
	}
	if len(character.MissionInventory) != 1 {
		t.Fatal("a nil cleanup hook changed the bag", character.MissionInventory)
	}
}
