/*
===========================================================================

duplicate_rows_test.go - arming a bag that holds two rows on one slot

The B-12 report shape (#3005): the persisted bag can hold two rows on
one wire slot (a shadowed row). The engine reads a slot through its
first row, so the shadow occupies the slot for every grant while the
client, which keeps one row per slot, shows it empty - pickups then
refuse as "inventory full" against visibly free slots. New heals the
shape: the first row keeps the slot, a shadowed bag row moves to the
lowest free bag slot, and a shadow with nowhere to go is kept as it
lies until a slot frees. No row is ever deleted.

===========================================================================
*/
package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
TestNewHealsDuplicateBagRowsIntoTheFirstHole

Capacity 45, bag slots 13..44 filled except the holes {24, 29}, plus a
second row shadowed onto occupied slot 20. The shadow must surface in
the lowest hole with its count intact, no row may be lost, and grants
then fill the remaining hole before refusing honestly.
================
*/
func TestNewHealsDuplicateBagRowsIntoTheFirstHole(t *testing.T) {
	rows := make([]Item, 0, 38)
	for slot := uint8(0); slot < 7; slot++ {
		rows = append(rows, sword(slot))
	}
	for slot := uint8(13); slot < 45; slot++ {
		if slot == 24 || slot == 29 {
			continue
		}
		rows = append(rows, sword(slot))
	}
	shadow := potion(20)
	shadow.Quantity = 7
	rows = append(rows, shadow)

	inv := New(rows, domain.DefaultInventorySize)
	if inv.Len() != len(rows) {
		t.Fatalf("row count = %d, want %d: healing lost a row", inv.Len(), len(rows))
	}
	seen := map[uint8]bool{}
	for _, row := range inv.Items() {
		if seen[row.Slot] {
			t.Fatalf("two rows still share slot %d after healing", row.Slot)
		}
		seen[row.Slot] = true
	}
	canonical, ok := inv.At(20)
	if !ok || canonical.RefObjID != 11459 {
		t.Fatalf("slot 20 = %+v, want the first row (the sword) to keep the slot", canonical)
	}
	surfaced, ok := inv.At(24)
	if !ok || surfaced.RefObjID != 3630 || surfaced.Quantity != 7 {
		t.Fatalf("slot 24 = %+v, want the shadowed potion x7 surfaced in the lowest hole", surfaced)
	}
	if slot, ok := inv.FirstFreeBagSlot(); !ok || slot != 29 {
		t.Fatalf("first free bag slot = %d, %v; want 29", slot, ok)
	}
	grant, fault := inv.GrantStack(sword(0), 1)
	if fault != nil || grant.DestSlot != 29 {
		t.Fatalf("grant = %+v, %v; want a fresh grant into slot 29", grant, fault)
	}
	if _, fault := inv.GrantStack(sword(0), 1); fault == nil || fault.Code != wire.ErrCodeStorageFull {
		t.Fatalf("grant into the healed full bag = %v, want ErrCodeStorageFull", fault)
	}
}

/*
================
TestNewHealsDuplicateEquipmentRowsIntoTheBag

A shadowed equipment row cannot stay on its socket (the client shows
one row there too); it surfaces in the first free bag slot instead.
================
*/
func TestNewHealsDuplicateEquipmentRowsIntoTheBag(t *testing.T) {
	worn := sword(6)
	worn.Plus = 5
	shadow := sword(6)
	shadow.Plus = 9
	inv := New([]Item{worn, shadow}, domain.DefaultInventorySize)

	if inv.Len() != 2 {
		t.Fatalf("row count = %d, want 2: healing lost a row", inv.Len())
	}
	atSocket, ok := inv.At(6)
	if !ok || atSocket.Plus != 5 {
		t.Fatalf("socket 6 = %+v, want the first row (+5) to keep the socket", atSocket)
	}
	surfaced, ok := inv.At(13)
	if !ok || surfaced.Plus != 9 {
		t.Fatalf("slot 13 = %+v, want the shadowed +9 sword surfaced in the bag", surfaced)
	}
}

/*
================
TestNewKeepsAShadowWithNowhereToGo

A genuinely full bag has no hole to surface a shadow into: the row is
kept as it lies (never deleted, so the write-back cannot burn a real
item), the full refusal that follows stays honest, and on a later arm
- once a slot has freed - the kept shadow surfaces into the hole.
================
*/
func TestNewKeepsAShadowWithNowhereToGo(t *testing.T) {
	rows := make([]Item, 0, 33)
	for slot := uint8(13); slot < 45; slot++ {
		rows = append(rows, sword(slot))
	}
	rows = append(rows, potion(20))

	inv := New(rows, domain.DefaultInventorySize)
	if inv.Len() != 33 {
		t.Fatalf("row count = %d, want 33: a shadow with nowhere to go is kept, not deleted", inv.Len())
	}
	if _, fault := inv.GrantStack(sword(0), 1); fault == nil || fault.Code != wire.ErrCodeStorageFull {
		t.Fatalf("grant into the full bag = %v, want ErrCodeStorageFull", fault)
	}

	// A slot frees (slot 44's row leaves) and the bag arms again: the
	// kept shadow surfaces into the hole.
	armed := make([]Item, 0, 32)
	for _, row := range inv.Items() {
		if row.Slot != 44 {
			armed = append(armed, row)
		}
	}
	inv = New(armed, domain.DefaultInventorySize)
	surfaced, ok := inv.At(44)
	if !ok || surfaced.RefObjID != 3630 {
		t.Fatalf("slot 44 = %+v, want the kept potion surfaced once a slot freed", surfaced)
	}
	seen := map[uint8]bool{}
	for _, row := range inv.Items() {
		if seen[row.Slot] {
			t.Fatalf("two rows still share slot %d after the second arm", row.Slot)
		}
		seen[row.Slot] = true
	}
}
