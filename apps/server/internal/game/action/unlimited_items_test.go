/*
===========================================================================

unlimited_items_test.go - the beta kit's items take effect without being spent

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
)

/*
================
TestConsumeItemUseRowKeepsUnlimitedItems
================
*/
func TestConsumeItemUseRowKeepsUnlimitedItems(t *testing.T) {
	rt := &Runtime{UnlimitedItems: map[string]bool{"ITEM_ETC_SCROLL_RETURN_01": true}}
	c := &enterworld.Character{MissionInventory: []enterworld.InventoryRow{
		{Slot: 13, Codename: "ITEM_ETC_SCROLL_RETURN_01", StackCount: 1},
		{Slot: 14, Codename: "ITEM_ETC_HP_POTION_01", StackCount: 2},
	}}
	if left := rt.consumeItemUseRow(c, 0); left != 1 || len(c.MissionInventory) != 2 || c.MissionInventory[0].StackCount != 1 {
		t.Fatalf("unlimited scroll spent: left %d, rows %+v", left, c.MissionInventory)
	}
	if left := rt.consumeItemUseRow(c, 1); left != 1 || c.MissionInventory[1].StackCount != 1 {
		t.Fatalf("ordinary potion not spent: left %d, rows %+v", left, c.MissionInventory)
	}
}
