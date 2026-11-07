/*
===========================================================================

peercapture_test.go - the peer capture's allocation shape

The capture runs once per session per tick (#2030): its worn and
avatar copies are sized by the rows that pass their filters, never
by the whole inventory behind them.

===========================================================================
*/

package movement

import (
	"testing"

	"opensro.online/server/internal/domain"
)

/*
================
TestCapturePeerAppearanceSizesCopiesByPassingRows

A fat bag (ninety rows past the equipment band) must not widen the
worn copy: only the three equipment rows land, at exact capacity,
in inventory order, and the avatar copy is sized the same way.
================
*/
func TestCapturePeerAppearanceSizesCopiesByPassingRows(t *testing.T) {
	c := &domain.Character{Name: "Fat"}
	for slot := int64(13); slot < 103; slot++ {
		c.MissionInventory = append(c.MissionInventory, domain.InventoryRow{Slot: slot, RefObjID: 5000, StackCount: 1})
	}
	c.MissionInventory = append(c.MissionInventory,
		domain.InventoryRow{Slot: 12, RefObjID: 9012, TypeFlags: 3, Plus: 4},
		domain.InventoryRow{Slot: 0, RefObjID: 9000, TypeFlags: 1},
		domain.InventoryRow{Slot: 6, RefObjID: 9006, TypeFlags: 2, Plus: 9},
		domain.InventoryRow{Slot: 3, RefObjID: 0},
	)
	c.AvatarInventory = &domain.AvatarInventory{Capacity: 4, Rows: []domain.InventoryRow{
		{Slot: 1, RefObjID: 0},
		{Slot: 2, RefObjID: 23296, TypeFlags: avatarDressTid, StackCount: 1},
		{Slot: 0, RefObjID: 23295, TypeFlags: avatarDressTid, StackCount: 1},
	}}

	captured := capturePeerAppearance(c, 1907)
	if len(captured.worn) != 3 || cap(captured.worn) != 3 {
		t.Fatalf("worn = len %d cap %d, want 3/3: %+v", len(captured.worn), cap(captured.worn), captured.worn)
	}
	wantSlots := []int64{12, 0, 6}
	for i, row := range captured.worn {
		if row.slot != wantSlots[i] {
			t.Fatalf("worn[%d] slot %d, want inventory order %v", i, row.slot, wantSlots)
		}
	}
	if len(captured.avatars) != 2 || cap(captured.avatars) != 2 {
		t.Fatalf("avatars = len %d cap %d, want 2/2: %+v", len(captured.avatars), cap(captured.avatars), captured.avatars)
	}

	plain := capturePeerAppearance(&domain.Character{Name: "Plain"}, 1907)
	if plain.worn != nil || plain.avatars != nil {
		t.Fatalf("an empty inventory allocated copies: %+v", plain)
	}
}
