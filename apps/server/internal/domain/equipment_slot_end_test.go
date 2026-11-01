/*
===========================================================================

equipment_slot_end_test.go - the domain/inventory equipment boundary tie

===========================================================================
*/
package domain_test

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
)

/*
================
TestEquipmentSlotEndMatchesInventoryPackage

domain states the equipment/bag boundary because the inventory
package imports domain, not the other way round; the two constants
must never drift.
================
*/
func TestEquipmentSlotEndMatchesInventoryPackage(t *testing.T) {
	if domain.EquipmentSlotEnd != int(inventory.EquipmentSlotEnd) {
		t.Fatalf("domain.EquipmentSlotEnd = %d, inventory.EquipmentSlotEnd = %d",
			domain.EquipmentSlotEnd, inventory.EquipmentSlotEnd)
	}
}
